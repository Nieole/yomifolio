package eu.kanade.tachiyomi.extension.all.mangamanager

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class PasswordPreferencesTest {
    private fun migrate(values: MutableMap<String, String>, commitSucceeds: Boolean = true): Int {
        var writes = 0
        migrateLegacyPassword(values::containsKey, values::get) { password ->
            writes++
            if (commitSucceeds) {
                if (password != null) values[PASSWORD_PREFERENCE_KEY] = password
                values.remove(LEGACY_PASSWORD_PREFERENCE_KEY)
            }
            commitSucceeds
        }
        return writes
    }

    @Test fun `password uses the official private preference prefix`() {
        assertTrue(PASSWORD_PREFERENCE_KEY.startsWith("__PRIVATE_"))
        assertFalse(LEGACY_PASSWORD_PREFERENCE_KEY.startsWith("__PRIVATE_"))
    }

    @Test fun `legacy password moves and old key is removed together`() {
        val values = mutableMapOf(LEGACY_PASSWORD_PREFERENCE_KEY to "old-secret", "username" to "reader")
        assertEquals(1, migrate(values))
        assertEquals(mapOf(PASSWORD_PREFERENCE_KEY to "old-secret", "username" to "reader"), values)
        assertEquals(0, migrate(values))
    }

    @Test fun `existing private password wins even when intentionally empty`() {
        for (current in listOf("new-secret", "")) {
            val values = mutableMapOf(LEGACY_PASSWORD_PREFERENCE_KEY to "old-secret", PASSWORD_PREFERENCE_KEY to current)
            assertEquals(1, migrate(values))
            assertEquals(mapOf(PASSWORD_PREFERENCE_KEY to current), values)
        }
    }

    @Test fun `no legacy key means no preference write`() {
        for (values in listOf(mutableMapOf<String, String>(), mutableMapOf(PASSWORD_PREFERENCE_KEY to "secret"))) {
            val original = values.toMap()
            assertEquals(0, migrate(values))
            assertEquals(original, values)
        }
    }

    @Test fun `empty legacy password is preserved as a private value`() {
        val values = mutableMapOf(LEGACY_PASSWORD_PREFERENCE_KEY to "")
        assertEquals(1, migrate(values))
        assertEquals(mapOf(PASSWORD_PREFERENCE_KEY to ""), values)
    }

    @Test fun `failed disk commit still fails if preferences already changed in memory`() {
        val values = mutableMapOf(LEGACY_PASSWORD_PREFERENCE_KEY to "old-secret")
        assertThrows(IllegalStateException::class.java) {
            migrateLegacyPassword(values::containsKey, values::get) { password ->
                if (password != null) values[PASSWORD_PREFERENCE_KEY] = password
                values.remove(LEGACY_PASSWORD_PREFERENCE_KEY)
                false
            }
        }
    }

    @Test fun `failed commit fails source initialization without exposing secret in error`() {
        val values = mutableMapOf(LEGACY_PASSWORD_PREFERENCE_KEY to "old-secret")
        val failure = assertThrows(IllegalStateException::class.java) { migrate(values, commitSucceeds = false) }
        assertFalse(failure.message.orEmpty().contains("old-secret"))
        assertEquals(mapOf(LEGACY_PASSWORD_PREFERENCE_KEY to "old-secret"), values)
    }
}
