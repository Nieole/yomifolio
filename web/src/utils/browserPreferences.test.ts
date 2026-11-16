// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PREFERENCE_KEYS, readPreference, writePreference } from './browserPreferences';

beforeEach(() => localStorage.clear());
afterEach(() => vi.restoreAllMocks());

const managerPreferences = ['locale', 'theme', 'sidebarCollapsed', 'librariesExpanded', 'paginationMode',
  'libraryViewMode', 'recentLibraryPaths', 'recentExternalPaths', 'externalIgnoreExtension'];

describe('ordinary browser preference compatibility', () => {
  it.each(Object.entries(PREFERENCE_KEYS))('copies the legacy %s key without removing it', (name, key) => {
    const legacyKey = key.replace('yomifolio_', managerPreferences.includes(name) ? 'manga_manager_' : 'manga_');
    localStorage.setItem(legacyKey, 'saved');
    expect(readPreference(key)).toBe('saved');
    expect(localStorage.getItem(key)).toBe('saved');
    expect(localStorage.getItem(legacyKey)).toBe('saved');
  });

  it('prefers an existing new value, even an empty string', () => {
    localStorage.setItem('manga_reader_theme', 'old');
    localStorage.setItem(PREFERENCE_KEYS.readerTheme, '');
    expect(readPreference(PREFERENCE_KEYS.readerTheme)).toBe('');
  });

  it('returns the legacy value on copy failure and retries on the next read', () => {
    localStorage.setItem('manga_manager_theme', 'dark');
    const failure = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('quota'); });
    expect(readPreference(PREFERENCE_KEYS.theme)).toBe('dark');
    expect(localStorage.getItem('manga_manager_theme')).toBe('dark');
    expect(localStorage.getItem(PREFERENCE_KEYS.theme)).toBeNull();
    failure.mockRestore();
    expect(readPreference(PREFERENCE_KEYS.theme)).toBe('dark');
    expect(localStorage.getItem(PREFERENCE_KEYS.theme)).toBe('dark');
  });

  it('does not crash on storage read or write errors', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked'); });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked'); });
    expect(readPreference(PREFERENCE_KEYS.locale)).toBeNull();
    expect(() => writePreference(PREFERENCE_KEYS.locale, 'en-US')).not.toThrow();
  });

  it('does not migrate unknown keys or offline state', () => {
    localStorage.setItem('manga_offline_books', '[1]');
    expect(readPreference('yomifolio_offline_books')).toBeNull();
    expect(localStorage.getItem('manga_offline_books')).toBe('[1]');
  });
});
