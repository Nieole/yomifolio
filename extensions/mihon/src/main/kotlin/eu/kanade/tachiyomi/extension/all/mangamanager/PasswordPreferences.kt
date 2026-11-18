package eu.kanade.tachiyomi.extension.all.mangamanager

internal const val PASSWORD_PREFERENCE_KEY = "__PRIVATE_manga_manager_password"
internal const val LEGACY_PASSWORD_PREFERENCE_KEY = "password"

internal fun migrateLegacyPassword(
    contains: (String) -> Boolean,
    read: (String) -> String?,
    commit: (String?) -> Boolean,
) {
    if (!contains(LEGACY_PASSWORD_PREFERENCE_KEY)) return
    val password = if (contains(PASSWORD_PREFERENCE_KEY)) null else read(LEGACY_PASSWORD_PREFERENCE_KEY).orEmpty()
    check(commit(password)) { "Unable to migrate password settings safely. Restart Mihon and try again." }
}
