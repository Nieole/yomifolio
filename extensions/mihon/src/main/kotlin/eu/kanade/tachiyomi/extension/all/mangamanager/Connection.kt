package eu.kanade.tachiyomi.extension.all.mangamanager

import okhttp3.Authenticator
import okhttp3.CookieJar
import okhttp3.Credentials
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.Interceptor
import okhttp3.OkHttpClient
import java.io.IOException
import java.security.MessageDigest

private fun safeUrl(raw: String): HttpUrl = try {
    raw.toHttpUrl()
} catch (_: IllegalArgumentException) {
    throw IOException("Invalid Manga Manager URL. Check the server root setting.")
}

internal const val API = "/api/mihon/v1"
internal const val BINDING = "__mm_identity"
internal const val STALE = "This item belongs to another server/account. Restore this slot's settings or add the item again from Browse."

internal class Connection(address: String, val username: String, private val password: String) {
    val base: HttpUrl
    init {
        val parsed = safeUrl(address.trim())
        require(parsed.username.isEmpty() && parsed.password.isEmpty() && parsed.query == null && parsed.fragment == null) {
            "Use a server URL without credentials, query or fragment."
        }
        require(username.isNotBlank() && ':' !in username) { "Set a valid Basic authentication username." }
        base = parsed.newBuilder().encodedPath(parsed.encodedPath.trimEnd('/') + "/").build()
    }
    val identity: String = MessageDigest.getInstance("SHA-256")
        .digest((base.toString() + "\u0000" + username).toByteArray(Charsets.UTF_8))
        .joinToString("") { "%02x".format(it) }
    private val apiPath = base.encodedPath.trimEnd('/') + API

    fun api(path: String): HttpUrl = bind((base.toString().trimEnd('/') + API + "/" + path).toHttpUrl())

    fun resource(raw: String): HttpUrl {
        // The backend emits root-relative protocol URLs even behind a path-stripping proxy.
        val url = when {
            raw.startsWith("$API/") -> (base.toString().trimEnd('/') + raw).toHttpUrl()
            else -> base.resolve(raw) ?: throw IOException("Invalid resource URL")
        }
        requireAllowed(url)
        require(url.queryParameter(BINDING) == null) { "Unexpected resource identity" }
        return bind(url)
    }

    private fun bind(url: HttpUrl): HttpUrl = url.newBuilder().setQueryParameter(BINDING, identity).build()

    fun validate(raw: String): HttpUrl {
        val url = safeUrl(raw)
        requireAllowed(url)
        if (url.queryParameterValues(BINDING) != listOf(identity)) throw IOException(STALE)
        return url
    }

    private fun requireAllowed(url: HttpUrl) {
        if (url.scheme != base.scheme || url.host != base.host || url.port != base.port ||
            url.username.isNotEmpty() || url.password.isNotEmpty() || url.fragment != null ||
            url.pathSegments.any { '/' in it || '\\' in it || it == "." || it == ".." } ||
            !url.encodedPath.startsWith("$apiPath/")) {
            throw IOException("Refusing a resource outside the configured Manga Manager API")
        }
    }

    fun authorization(): String = Credentials.basic(username, password, Charsets.UTF_8)
}

/** Validate at dispatch time too: cached covers/pages and queued downloads can outlive configuration. */
internal class Authentication(private val connection: () -> Connection) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): okhttp3.Response {
        val current = connection()
        current.validate(chain.request().url.toString())
        val response = try {
            chain.proceed(chain.request().newBuilder()
                .header("Authorization", current.authorization())
                .header("User-Agent", "MangaManager-Mihon/1")
                .removeHeader("Cookie")
                .build())
        } catch (_: IOException) {
            throw IOException("Manga Manager connection failed. Check network and server settings.")
        }
        if (!response.isSuccessful) {
            val code = response.code
            response.close()
            throw IOException("Manga Manager HTTP $code. Check protocol enablement, URL and account settings.")
        }
        return response
    }
}

internal fun authenticatedClient(connection: () -> Connection): OkHttpClient = OkHttpClient.Builder()
    .followRedirects(false)
    .followSslRedirects(false)
    .authenticator(Authenticator.NONE)
    .cookieJar(CookieJar.NO_COOKIES)
    .addInterceptor(Authentication(connection))
    .build()
