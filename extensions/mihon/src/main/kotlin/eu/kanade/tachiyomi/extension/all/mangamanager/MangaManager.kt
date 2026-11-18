package eu.kanade.tachiyomi.extension.all.mangamanager

import android.content.Context
import android.content.SharedPreferences
import android.text.InputType
import androidx.preference.EditTextPreference
import androidx.preference.PreferenceScreen
import eu.kanade.tachiyomi.source.ConfigurableSource
import eu.kanade.tachiyomi.source.Source
import eu.kanade.tachiyomi.source.SourceFactory
import eu.kanade.tachiyomi.source.model.Filter
import eu.kanade.tachiyomi.source.model.FilterList
import eu.kanade.tachiyomi.source.model.MangasPage
import eu.kanade.tachiyomi.source.model.Page
import eu.kanade.tachiyomi.source.model.SChapter
import eu.kanade.tachiyomi.source.model.SManga
import eu.kanade.tachiyomi.source.online.HttpSource
import okhttp3.Headers
import okhttp3.Request
import okhttp3.Response
import org.json.JSONArray
import org.json.JSONObject
import uy.kohesive.injekt.Injekt
import uy.kohesive.injekt.api.get
import java.io.IOException

class MangaManagerFactory : SourceFactory {
    override fun createSources(): List<Source> = SOURCE_IDS.mapIndexed { index, id -> MangaManager(index + 1, id) }
}


class MangaManager(private val slot: Int, override val id: Long) : HttpSource(), ConfigurableSource {
    override val name = "Manga Manager ($slot)"
    override val lang = "all"
    override val supportsLatest = true
    private val preferences: SharedPreferences =
        Injekt.get<Context>().getSharedPreferences("source_$id", Context.MODE_PRIVATE).also { store ->
            migrateLegacyPassword(store::contains, { store.getString(it, null) }) { password ->
                val editor = store.edit().remove(LEGACY_PASSWORD_PREFERENCE_KEY)
                if (password != null) editor.putString(PASSWORD_PREFERENCE_KEY, password)
                editor.commit()
            }
        }
    private fun connection() = Connection(
        preferences.getString("address", "")!!,
        preferences.getString("username", "")!!,
        preferences.getString(PASSWORD_PREFERENCE_KEY, "")!!,
    )
    override val baseUrl: String get() = connection().base.toString().trimEnd('/')
    override val client by lazy { authenticatedClient(::connection) }
    override fun headersBuilder(): Headers.Builder = Headers.Builder()

    @Volatile private var catalog: Discovery? = null

    private fun request(url: String) = Request.Builder().url(connection().validate(url)).get().build()
    override fun popularMangaRequest(page: Int): Request = request(Search().url(connection(), page).toString())
    override fun latestUpdatesRequest(page: Int): Request = request(connection().api("recently-added").newBuilder()
        .addQueryParameter("page", page.toString()).addQueryParameter("limit", "30").build().toString())

    private class Select(name: String, val choices: List<Choice>, val identity: String) : Filter.Select<String>(name, choices.map { it.label }.toTypedArray()) {
        val selected get() = choices[state].value
    }
    private class Text(name: String) : Filter.Text(name)

    override fun getFilterList(): FilterList {
        val identity = runCatching { connection().identity }.getOrDefault("")
        val known = catalog?.takeIf { it.identity == identity }
        fun select(name: String, options: List<Choice>) = Select(name, listOf(Choice("Any", "")) + options, identity)
        return FilterList(
            Filter.Header("Browse or Latest loads discovery; Reset filters to refresh choices."),
            Filter.Header("Shelves keep server order; clear search and other filters when selecting one."),
            select("Library", known?.libraries.orEmpty()),
            select("Collection / smart collection / reading list", known?.shelves.orEmpty()),
            select("Tag", known?.tags.orEmpty()),
            Text("Additional tags (comma-separated, OR)"),
            select("Author", known?.authors.orEmpty()),
            Text("Additional authors (comma-separated, OR)"),
            select("Status", listOf("ongoing", "completed", "cancelled", "hiatus").map { Choice(it, it) }),
            select("Sort", listOf(Choice("Name", "name_asc"), Choice("Updated", "updated_desc"), Choice("Book count", "books_desc"))),
        )
    }

    override fun searchMangaRequest(page: Int, query: String, filters: FilterList): Request {
        val current = connection()
        val selected = filters.filterIsInstance<Select>()
        require(selected.all { it.identity == current.identity }) { "Settings changed. Reset filters first." }
        fun value(name: String) = selected.firstOrNull { it.name == name }?.selected.orEmpty()
        fun extra(name: String) = filters.filterIsInstance<Text>().firstOrNull { it.name == name }?.state.orEmpty().trim()
        fun csv(first: String, rest: String) = listOf(first, rest).filter { it.isNotBlank() }.joinToString(",")
        val search = Search(query.trim(), value("Library"), value("Collection / smart collection / reading list"),
            csv(value("Tag"), extra("Additional tags (comma-separated, OR)")),
            csv(value("Author"), extra("Additional authors (comma-separated, OR)")), value("Status"), value("Sort"))
        return request(search.url(current, page).toString())
    }

    private fun body(response: Response): String {
        connection().validate(response.request.url.toString())
        if (!response.isSuccessful) throw IOException("Manga Manager HTTP ${response.code}. Check protocol enablement, URL and account settings.")
        return response.body?.string() ?: throw IOException("Empty Manga Manager response")
    }
    private fun responseConnection(response: Response): Connection = connection().also { it.validate(response.request.url.toString()) }

    @Synchronized private fun loadDiscovery(current: Connection) {
        fun array(path: String): JSONArray = client.newCall(request(current.api(path).toString())).execute().use { JSONArray(body(it)) }
        catalog = discovery(current, array("libraries"), array("collections"), array("reading-lists"), array("tags"), array("authors"))
    }

    private fun manga(value: Series): SManga = SManga.create().apply {
        url = value.url; title = value.title; description = value.description
        status = value.status; thumbnail_url = value.cover
        author = value.author; artist = value.artist; genre = value.genre
    }
    private fun page(response: Response, refresh: Boolean): MangasPage {
        val current = responseConnection(response)
        val json = JSONObject(body(response))
        val result = MangasPage(json.getJSONArray("items").objects().map { manga(series(it, current)) }, json.getBoolean("has_next"))
        if (refresh || catalog?.identity != current.identity) loadDiscovery(current)
        return result
    }
    override fun popularMangaParse(response: Response): MangasPage = page(response, response.request.url.queryParameter("page") == "1")
    override fun latestUpdatesParse(response: Response): MangasPage = popularMangaParse(response)
    override fun searchMangaParse(response: Response): MangasPage = page(response, false)

    override fun mangaDetailsRequest(manga: SManga): Request = request(manga.url)
    override fun mangaDetailsParse(response: Response): SManga = manga(series(JSONObject(body(response)), responseConnection(response)))
    override fun chapterListRequest(manga: SManga): Request {
        val url = connection().validate(manga.url).newBuilder().addPathSegment("books").build()
        return request(url.toString())
    }
    override fun chapterListParse(response: Response): List<SChapter> = books(JSONArray(body(response)), responseConnection(response))
        .map { book -> SChapter.create().apply {
            url = book.url; name = book.title; chapter_number = book.number
            // The API exposes updated_at, not a publication date. Do not mislabel it as upload time.
            date_upload = 0L
        } }.reversed() // HttpSource chapter lists are newest-first; API book order is reading order.

    override fun pageListRequest(chapter: SChapter): Request = request(chapter.url)
    override fun pageListParse(response: Response): List<Page> = images(JSONArray(body(response)), responseConnection(response))
        .map { Page(it.index, imageUrl = it.url) }
    override fun imageRequest(page: Page): Request = request(page.imageUrl ?: throw IOException("Missing page image"))
    override fun imageUrlParse(response: Response): String = throw UnsupportedOperationException("Page manifests contain direct image URLs")

    override fun setupPreferenceScreen(screen: PreferenceScreen) {
        fun field(keyName: String, label: String, secret: Boolean = false) {
            screen.addPreference(EditTextPreference(screen.context).apply {
                key = "manga_manager_${id}_$keyName"
                title = label
                setPersistent(false)
                text = preferences.getString(keyName, "")
                summary = if (secret) "Stored privately in Mihon; never included in content URLs" else when (keyName) {
                    "address" -> "Server root, including proxy prefix (https://example.org/manga). HTTPS recommended. Redirects are refused."
                    else -> "Changing server or username invalidates previously added items in this slot."
                }
                setOnBindEditTextListener { edit ->
                    edit.inputType = if (secret) InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_PASSWORD
                    else InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
                }
                setOnPreferenceChangeListener { _, value ->
                    preferences.edit().putString(keyName, value as String).apply()
                    catalog = null
                    true
                }
            })
        }
        field("address", "Server URL")
        field("username", "Username")
        field(PASSWORD_PREFERENCE_KEY, "Password", true)
    }
}
