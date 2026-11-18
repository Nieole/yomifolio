package eu.kanade.tachiyomi.extension.all.mangamanager

import okhttp3.HttpUrl
import org.json.JSONArray
import org.json.JSONObject

// Published identities: never derive these from editable settings or names, and never reorder them.
internal val SOURCE_IDS = listOf(794243186540210001L, 794243186540210002L, 794243186540210003L, 794243186540210004L, 794243186540210005L)


internal data class Series(val url: String, val title: String, val description: String, val status: Int, val cover: String?, val author: String?, val artist: String?, val genre: String?)
internal data class Book(val url: String, val title: String, val number: Float)
internal data class Image(val index: Int, val url: String)
internal data class Choice(val label: String, val value: String)
internal data class Discovery(val identity: String, val libraries: List<Choice>, val shelves: List<Choice>, val tags: List<Choice>, val authors: List<Choice>)

internal fun JSONArray.objects(): List<JSONObject> = (0 until length()).map { getJSONObject(it) }
internal fun status(raw: String): Int = when (raw.lowercase()) {
    "ongoing" -> 1
    "completed" -> 2
    "cancelled" -> 5
    "hiatus" -> 6
    else -> 0
}
internal fun series(json: JSONObject, connection: Connection): Series = Series(
    connection.api("series/${json.getLong("id")}").toString(),
    json.optString("title").ifBlank { json.getString("name") },
    json.optString("summary"),
    status(json.optString("status")),
    json.optString("cover_url").takeIf { it.isNotBlank() }?.let { connection.resource(it).toString() },
    credits(json, "story"),
    credits(json, "art"),
    json.optJSONArray("tags")?.let { tags ->
        (0 until tags.length()).map { tags.getString(it) }.filter { it.isNotBlank() }.distinct().joinToString(", ")
    },
)
private fun credits(json: JSONObject, role: String): String? = json.optJSONArray("authors")?.objects()
    ?.filter { it.optString("role") == role }
    ?.map { it.getString("name") }?.filter { it.isNotBlank() }?.distinct()?.joinToString(", ")

internal fun books(json: JSONArray, connection: Connection): List<Book> = json.objects().map { book ->
    Book(
        connection.api("books/${book.getLong("id")}/pages").toString(),
        book.optString("title").ifBlank { book.getString("name") },
        book.optString("number").toFloatOrNull() ?: book.optDouble("sort_number", 0.0).toFloat(),
    )
}
internal fun images(json: JSONArray, connection: Connection): List<Image> = json.objects().map { page ->
    val index = page.getInt("index")
    require(index > 0) { "Invalid one-based page index" }
    Image(index - 1, connection.resource(page.getString("image_url")).toString())
}.also { pages ->
    require(pages.map { it.index } == pages.indices.toList()) { "Non-contiguous page manifest" }
}

internal data class Search(
    val query: String = "", val library: String = "", val shelf: String = "",
    val tags: String = "", val authors: String = "", val status: String = "", val sort: String = "",
) {
    fun url(connection: Connection, page: Int): HttpUrl {
        require(page > 0)
        val scoped = shelf.isNotEmpty()
        require(!scoped || listOf(query, library, tags, authors, status, sort).all { it.isBlank() }) {
            "Collections and reading lists use server-defined membership/order. Clear search and other filters first."
        }
        val path = if (scoped) shelf else "series"
        require(path == "series" || Regex("(collections|smart-collections|reading-lists)/[0-9]+/series").matches(path))
        return connection.api(path).newBuilder()
            .addQueryParameter("page", page.toString()).addQueryParameter("limit", "30")
            .apply {
                if (!scoped) {
                    mapOf("q" to query, "libraryId" to library, "tag" to tags, "author" to authors, "status" to status, "sort" to sort)
                        .filterValues { it.isNotBlank() }.forEach { (key, value) -> addQueryParameter(key, value) }
                }
            }.build()
    }
}

internal fun discovery(connection: Connection, libraries: JSONArray, collections: JSONArray, lists: JSONArray, tags: JSONArray, authors: JSONArray): Discovery {
    fun strings(array: JSONArray) = (0 until array.length()).map { Choice(array.getString(it), array.getString(it)) }
    return Discovery(
        connection.identity,
        libraries.objects().map { Choice(it.getString("name"), it.getLong("id").toString()) },
        collections.objects().map {
            val kind = it.getString("kind")
            require(kind == "smart" || kind == "collection") { "Unknown collection kind: $kind" }
            val path = if (kind == "smart") "smart-collections" else "collections"
            Choice("${if (kind == "smart") "Smart" else "Collection"}: ${it.getString("name")}", "$path/${it.getLong("numeric_id")}/series")
        } + lists.objects().map { Choice("Reading list: ${it.getString("name")}", "reading-lists/${it.getLong("id")}/series") },
        strings(tags), strings(authors),
    )
}
