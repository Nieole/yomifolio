// Ordinary device preferences only; offline books, queues, owners and caches are not migrated here.
export const PREFERENCE_KEYS = {
  locale: 'yomifolio_locale',
  theme: 'yomifolio_theme',
  sidebarCollapsed: 'yomifolio_sidebar_collapsed',
  librariesExpanded: 'yomifolio_libraries_expanded',
  paginationMode: 'yomifolio_pagination_mode',
  libraryViewMode: 'yomifolio_library_view_mode',
  recentLibraryPaths: 'yomifolio_recent_library_paths',
  recentExternalPaths: 'yomifolio_recent_external_paths',
  externalIgnoreExtension: 'yomifolio_external_ignore_extension',
  readerTheme: 'yomifolio_reader_theme',
  readMode: 'yomifolio_read_mode',
  readDirection: 'yomifolio_read_direction',
  doublePage: 'yomifolio_double_page',
  scaleMode: 'yomifolio_scale_mode',
  imageFilter: 'yomifolio_image_filter',
  autoCrop: 'yomifolio_auto_crop',
  preloadCount: 'yomifolio_preload_count',
  readerImageFormat: 'yomifolio_reader_image_format',
  readerImageQuality: 'yomifolio_reader_image_quality',
  eyeProtection: 'yomifolio_eye_protection',
  w2xScale: 'yomifolio_waifu2x_scale',
  w2xNoise: 'yomifolio_waifu2x_noise',
  w2xFormat: 'yomifolio_waifu2x_format',
} as const;

const legacyKeys: Record<string, string> = Object.fromEntries(
  Object.entries(PREFERENCE_KEYS).map(([name, key]) => [key, key.replace('yomifolio_',
    ['locale', 'theme', 'sidebarCollapsed', 'librariesExpanded', 'paginationMode', 'libraryViewMode',
      'recentLibraryPaths', 'recentExternalPaths', 'externalIgnoreExtension'].includes(name)
      ? 'manga_manager_' : 'manga_')]),
);

export function writePreference(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // Storage may be unavailable or full. Keep in-memory preferences usable.
  }
}

export function readPreference(key: string): string | null {
  try {
    const storage = window.localStorage;
    const current = storage.getItem(key);
    if (current !== null) return current;
    const legacyKey = legacyKeys[key];
    const legacy = legacyKey ? storage.getItem(legacyKey) : null;
    if (legacy !== null) writePreference(key, legacy);
    // Retain the old value, including when copying failed, so the next read can retry.
    return legacy;
  } catch {
    return null;
  }
}
