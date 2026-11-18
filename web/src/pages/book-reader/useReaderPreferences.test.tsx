// @vitest-environment jsdom
import { cleanup, renderHook, act } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { useReaderPreferences } from './useReaderPreferences';
import { PREFERENCE_KEYS } from '../../utils/browserPreferences';

beforeEach(() => localStorage.clear());
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it('reads legacy reader preferences and writes changes only to the new key', () => {
  localStorage.setItem('manga_reader_theme', JSON.stringify('comimi'));
  localStorage.setItem('manga_read_mode', JSON.stringify('paged'));
  const { result } = renderHook(useReaderPreferences);
  expect(result.current.readerTheme).toBe('comimi');
  expect(result.current.readMode).toBe('paged');
  act(() => result.current.setReadMode('webtoon'));
  expect(localStorage.getItem(PREFERENCE_KEYS.readMode)).toBe(JSON.stringify('webtoon'));
  expect(localStorage.getItem('manga_read_mode')).toBe(JSON.stringify('paged'));
});

it('keeps the new preference authoritative over the old one', () => {
  localStorage.setItem('manga_read_mode', JSON.stringify('paged'));
  localStorage.setItem(PREFERENCE_KEYS.readMode, JSON.stringify('webtoon'));
  expect(renderHook(useReaderPreferences).result.current.readMode).toBe('webtoon');
});

it('falls back safely for malformed JSON', () => {
  localStorage.setItem(PREFERENCE_KEYS.readerTheme, '{');
  expect(renderHook(useReaderPreferences).result.current.readerTheme).toBe('base');
});

it('uses legacy preferences despite quota failures and retains them for retry', () => {
  localStorage.setItem('manga_read_mode', JSON.stringify('paged'));
  const failure = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('quota'); });
  const { result, unmount } = renderHook(useReaderPreferences);
  expect(result.current.readMode).toBe('paged');
  act(() => result.current.setReadMode('webtoon'));
  expect(localStorage.getItem('manga_read_mode')).toBe(JSON.stringify('paged'));
  unmount();
  failure.mockRestore();
  expect(renderHook(useReaderPreferences).result.current.readMode).toBe('paged');
  expect(localStorage.getItem(PREFERENCE_KEYS.readMode)).toBe(JSON.stringify('paged'));
});

it('remains usable when storage reads and writes are blocked', () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked'); });
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked'); });
  const { result } = renderHook(useReaderPreferences);
  expect(result.current.readMode).toBe('webtoon');
  act(() => result.current.setReadMode('paged'));
  expect(result.current.readMode).toBe('paged');
});
