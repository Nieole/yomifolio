package eu.kanade.tachiyomi.extension.all.mangamanager

import okhttp3.Credentials
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test
import java.io.IOException
import java.util.concurrent.TimeUnit

class AuthenticationTest {
    @Test fun everyResourceTypeUsesBasicAuthAndNeverWritesProgress() {
        MockWebServer().use { server ->
            server.start()
            val connection = Connection(server.url("/prefix").toString(), "user", "secret")
            val client = authenticatedClient { connection }
            listOf("series", "series/1", "series/1/books", "books/2/pages", "books/2/pages/1", "covers/2", "thumbnails/a.webp", "libraries", "collections", "reading-lists", "tags", "authors").forEach { path ->
                server.enqueue(MockResponse().setBody("{}"))
                client.newCall(Request.Builder().url(connection.api(path)).header("Cookie", "host-session=wrong-user").build()).execute().use { assertEquals(200, it.code) }
                val received = server.takeRequest()
                assertEquals("GET", received.method)
                assertEquals(Credentials.basic("user", "secret"), received.getHeader("Authorization"))
                assertNull(received.getHeader("Cookie"))
                assertTrue(received.path!!.startsWith("/prefix$API/"))
            }
            assertEquals(12, server.requestCount)
        }
    }

    @Test fun refusesCrossOriginBeforeSendingAnyCredentials() {
        MockWebServer().use { configured ->
            MockWebServer().use { external ->
                configured.start(); external.start()
                val connection = Connection(configured.url("/").toString(), "user", "secret")
                val client = authenticatedClient { connection }
                val malicious = external.url("$API/covers/1").newBuilder().addQueryParameter(BINDING, connection.identity).build()
                assertThrows(IOException::class.java) { client.newCall(Request.Builder().url(malicious).build()).execute() }
                assertEquals(0, configured.requestCount)
                assertEquals(0, external.requestCount)
            }
        }
    }

    @Test fun refusesAllRedirectsIncludingSameOriginAndDoesNotRetryAuthentication() {
        MockWebServer().use { server ->
            MockWebServer().use { external ->
                server.start(); external.start()
                val connection = Connection(server.url("/").toString(), "user", "secret")
                val client = authenticatedClient { connection }
                listOf(external.url("/stolen"), server.url("/outside-api")).forEach { destination ->
                    server.enqueue(MockResponse().setResponseCode(302).setHeader("Location", destination))
                    assertThrows(IOException::class.java) { client.newCall(Request.Builder().url(connection.api("series")).build()).execute() }
                    assertEquals(Credentials.basic("user", "secret"), server.takeRequest().getHeader("Authorization"))
                    assertNull(server.takeRequest(100, TimeUnit.MILLISECONDS))
                    assertEquals(0, external.requestCount)
                }
                server.enqueue(MockResponse().setResponseCode(401).setHeader("WWW-Authenticate", "Basic realm=server"))
                assertThrows(IOException::class.java) { client.newCall(Request.Builder().url(connection.api("series")).build()).execute() }
                server.takeRequest()
                assertEquals(3, server.requestCount)
            }
        }
    }

    @Test fun httpErrorsDoNotExposeQueryOrAccount() {
        MockWebServer().use { server ->
            server.start()
            val connection = Connection(server.url("/").toString(), "private-user", "secret")
            val client = authenticatedClient { connection }
            for (code in listOf(401, 403, 404, 429, 503)) {
                server.enqueue(MockResponse().setResponseCode(code))
                val url = Search(query = "private-query").url(connection, 1)
                val error = assertThrows(IOException::class.java) { client.newCall(Request.Builder().url(url).build()).execute() }
                assertTrue(error.message!!.contains("HTTP $code"))
                assertFalse(error.message!!.contains("private-"))
                assertNull(error.cause)
            }
        }
    }

    @Test fun unicodeAccountsUseUtf8BasicCredentials() {
        val connection = Connection("https://example.org", "读者", "密码")
        assertEquals(Credentials.basic("读者", "密码", Charsets.UTF_8), connection.authorization())
        assertFalse(connection.api("series").toString().contains("读者"))
    }

    @Test fun queuedImagesAreRejectedAfterAccountChangeButPasswordRotationWorks() {
        MockWebServer().use { server ->
            server.start()
            var current = Connection(server.url("/").toString(), "user", "old")
            val queued = Request.Builder().url(current.api("books/1/pages/1")).build()
            val client = authenticatedClient { current }
            current = Connection(server.url("/").toString(), "other", "new")
            assertThrows(IOException::class.java) { client.newCall(queued).execute() }
            assertEquals(0, server.requestCount)
            current = Connection(server.url("/").toString(), "user", "new")
            server.enqueue(MockResponse().setBody("image"))
            client.newCall(queued).execute().close()
            assertEquals(Credentials.basic("user", "new"), server.takeRequest().getHeader("Authorization"))
            assertEquals(1, server.requestCount)
        }
    }
}
