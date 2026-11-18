package eu.kanade.tachiyomi.extension.all.mangamanager

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class ProtocolTest {
    private val connection = Connection("https://example.org/proxy/manga/", "reader", "password-not-in-urls")

    @Test fun manifestAndSourceIdsRemainHostCompatible() {
        assertEquals(listOf(794243186540210001L, 794243186540210002L, 794243186540210003L, 794243186540210004L, 794243186540210005L), SOURCE_IDS)
        val manifest = java.io.File("../src/main/AndroidManifest.xml").readText()
        assertTrue(manifest.contains("android:name=\"tachiyomi.extension\""))
        assertTrue(manifest.contains("android:value=\".MangaManagerFactory\""))
    }

    @Test fun identityIncludesCanonicalServerPrefixAndAccountButNotPassword() {
        assertEquals(connection.identity, Connection("https://EXAMPLE.org:443/proxy/manga", "reader", "changed").identity)
        assertNotEquals(connection.identity, Connection("https://example.org/other", "reader", "x").identity)
        assertNotEquals(connection.identity, Connection("https://example.org/proxy/manga", "other", "x").identity)
        val url = connection.api("series/4").toString()
        assertFalse(url.contains("password-not-in-urls"))
        assertFalse(url.contains("reader"))
        assertEquals(url, connection.validate(url).toString())
    }

    @Test fun staleAccountServerAndLegacyUnboundItemsFailClosed() {
        val url = connection.api("series/4").toString()
        assertThrows(Exception::class.java) { Connection("https://elsewhere.org", "reader", "x").validate(url) }
        assertThrows(Exception::class.java) { Connection("https://example.org/proxy/manga", "other", "x").validate(url) }
        assertThrows(Exception::class.java) { connection.validate("https://example.org/proxy/manga$API/series/4") }
        assertThrows(Exception::class.java) { connection.validate("$url&$BINDING=${connection.identity}") }
    }

    @Test fun resourceUrlsRebaseBackendRootPathsWithoutEscapingProxyOrOrigin() {
        val root = connection.resource("$API/books/9/pages/2?format=webp&q=80")
        assertEquals("/proxy/manga$API/books/9/pages/2", root.encodedPath)
        assertEquals("80", root.queryParameter("q"))
        assertEquals("webp", root.queryParameter("format"))
        assertEquals(root, connection.validate(root.toString()))
        assertEquals(root, connection.resource("https://example.org/proxy/manga$API/books/9/pages/2?format=webp&q=80"))
        listOf("https://evil.org$API/covers/2", "//evil.org$API/covers/2", "/api/covers/2", "$API/../../admin", "http://example.org/proxy/manga$API/covers/2", "https://user:secret@example.org/proxy/manga$API/covers/2").forEach {
            assertThrows("Must reject $it", Exception::class.java) { connection.resource(it) }
        }
    }

    @Test fun seriesMappingUsesTitleFallbackStatusAndAuthenticatedCover() {
        val mapped = series(JSONObject("""{"id":4,"name":"Folder","title":"","summary":"Summary","status":"hiatus","cover_url":"/api/mihon/v1/covers/9"}"""), connection)
        assertEquals("Folder", mapped.title)
        assertEquals("Summary", mapped.description)
        assertEquals(6, mapped.status)
        assertTrue(mapped.url.contains("/series/4?"))
        connection.validate(mapped.cover!!)
        assertEquals(listOf(1, 2, 5, 6, 0), listOf("ongoing", "completed", "cancelled", "hiatus", "unknown").map(::status))
        assertNull(series(JSONObject("""{"id":1,"name":"n"}"""), connection).cover)
    }

    @Test fun detailCreditsUseRolesAndCatalogueDoesNotInventMetadata() {
        val mapped = series(JSONObject("""{"id":1,"name":"n","authors":[{"name":"Writer","role":"story"},{"name":"Artist","role":"art"},{"name":"Other","role":"editor"}],"tags":["Adventure","Mystery"]}"""), connection)
        assertEquals("Writer", mapped.author)
        assertEquals("Artist", mapped.artist)
        assertEquals("Adventure, Mystery", mapped.genre)
        val catalogue = series(JSONObject("""{"id":1,"name":"n"}"""), connection)
        assertNull(catalogue.author)
        assertNull(catalogue.artist)
        assertNull(catalogue.genre)
    }

    @Test fun rootAndEscapedProxyPathsStayInsideTheirBoundary() {
        val root = Connection("https://example.org/", "reader", "x")
        assertEquals("$API/covers/1", root.resource("$API/covers/1").encodedPath)
        val escaped = Connection("https://example.org/my%20manga", "reader", "x")
        assertEquals("/my%20manga$API/covers/1", escaped.resource("$API/covers/1").encodedPath)
        listOf("$API/%2f..%2fadmin", "$API/%5c..%5cadmin", "$API/%2e%2e/admin", "https://example.org/proxy/manga-sibling$API/covers/1").forEach {
            assertThrows(Exception::class.java) { connection.resource(it) }
        }
        val error = assertThrows(Exception::class.java) { Connection("bad-secret-address", "reader", "secret") }
        assertFalse(error.message!!.contains("bad-secret-address"))
    }

    @Test fun booksKeepServerReadingOrderAndPagesConvertOneBasedIndexes() {
        val mapped = books(JSONArray("""[{"id":9,"name":"second","number":"2.5","sort_number":99},{"id":2,"name":"first","title":"A title","sort_number":1}]"""), connection)
        assertEquals(listOf("second", "A title"), mapped.map { it.title })
        assertEquals(listOf(2.5f, 1f), mapped.map { it.number })
        mapped.forEach { connection.validate(it.url) }
        val pages = images(JSONArray("""[{"index":1,"image_url":"/api/mihon/v1/books/9/pages/1"},{"index":2,"image_url":"/api/mihon/v1/books/9/pages/2"}]"""), connection)
        assertEquals(listOf(0, 1), pages.map { it.index })
        assertTrue(pages[0].url.contains("/pages/1?"))
        assertThrows(IllegalArgumentException::class.java) { images(JSONArray("""[{"index":0,"image_url":"/api/mihon/v1/books/9/pages/1"}]"""), connection) }
    }

    @Test fun discoveryMapsStaticSmartAndOrderedListsToDistinctEndpoints() {
        val mapped = discovery(connection,
            JSONArray("""[{"id":2,"name":"Library"}]"""),
            JSONArray("""[{"kind":"collection","numeric_id":7,"name":"Manual"},{"kind":"smart","numeric_id":7,"name":"Rule"}]"""),
            JSONArray("""[{"id":7,"name":"Ordered"}]"""), JSONArray("""["A & B"]"""), JSONArray("""["Author"]"""))
        assertEquals(listOf("collections/7/series", "smart-collections/7/series", "reading-lists/7/series"), mapped.shelves.map { it.value })
        assertEquals("2", mapped.libraries.single().value)
        assertEquals("A & B", mapped.tags.single().value)
    }

    @Test fun searchPreservesBackendQuerySemanticsAndPagination() {
        val url = Search(query = "name + & 漫画", library = "3", tags = "A,B", authors = "Writer", status = "completed", sort = "updated_desc").url(connection, 2)
        assertEquals("name + & 漫画", url.queryParameter("q"))
        assertEquals("3", url.queryParameter("libraryId"))
        assertEquals("A,B", url.queryParameter("tag"))
        assertEquals("Writer", url.queryParameter("author"))
        assertEquals("completed", url.queryParameter("status"))
        assertEquals("updated_desc", url.queryParameter("sort"))
        assertEquals("2", url.queryParameter("page"))
        val list = Search(shelf = "reading-lists/7/series").url(connection, 3)
        assertNull(list.queryParameter("sort"))
        assertNull(list.queryParameter("q"))
        assertTrue(list.encodedPath.endsWith("/reading-lists/7/series"))
        // The membership endpoints do not implement keyword/filter queries: never silently discard them.
        assertThrows(IllegalArgumentException::class.java) { Search(query = "foo", shelf = "reading-lists/7/series").url(connection, 1) }
        assertThrows(IllegalArgumentException::class.java) { Search(sort = "books_desc", shelf = "collections/7/series").url(connection, 1) }
    }
}
