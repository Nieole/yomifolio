// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { apiClient } from '../../api/client';
import { useGlobalSearch } from './useGlobalSearch';
import { ApplicationSessionProvider } from '../../session/ApplicationSessionProvider';
import type { ReactNode } from 'react';

const original = apiClient.defaults.adapter;
afterEach(() => { cleanup(); apiClient.defaults.adapter = original; vi.useRealTimers(); vi.unstubAllGlobals(); });

function requests() {
  const pending: { url: string; finish: (title: string) => void; fail: () => void }[] = [];
  apiClient.defaults.adapter = config => new Promise((resolve, reject) => {
    // Simulate a transport that cannot cancel: also bypass axios's post-adapter cancellation check.
    config.signal = undefined;
    pending.push({ url: config.url!, finish: title => resolve({
      data: { hits: title ? [{ id: 's_1', fields: { title, type: 'series' } }] : [] },
      status: 200, statusText: 'OK', headers: {}, config,
    }), fail: () => reject(new Error('offline')) });
  });
  return pending;
}
async function tick(ms = 300) { await act(async () => { await vi.advanceTimersByTimeAsync(ms); }); }

it('removes old selectable hits immediately and ignores uncancellable late replies', async () => {
  vi.useFakeTimers();
  const pending = requests();
  const { result } = renderHook(() => useGlobalSearch());
  act(() => { result.current.setIsSearchModalOpen(true); result.current.setSearchQuery('first'); });
  await tick();
  await act(async () => pending[0].finish('First'));
  expect(result.current.searchResults[0]?.fields?.title).toBe('First');
  act(() => result.current.setSearchQuery('second'));
  expect(result.current.searchResults).toEqual([]);
  await tick();
  act(() => result.current.setSearchQuery('third'));
  await tick();
  await act(async () => pending[2].finish('Third'));
  await act(async () => pending[1].finish('Obsolete'));
  expect(result.current.searchResults[0]?.fields?.title).toBe('Third');
});

it('invalidates scope, close/reopen and reset even when old requests finish', async () => {
  vi.useFakeTimers();
  const pending = requests();
  const { result } = renderHook(() => useGlobalSearch());
  act(() => { result.current.setIsSearchModalOpen(true); result.current.setSearchQuery('same'); });
  await tick();
  act(() => result.current.setSearchTarget('series'));
  await tick();
  expect(pending[1].url).toBe('/api/search?q=same&target=series');
  await act(async () => pending[0].finish('Old scope'));
  expect(result.current.searchResults).toEqual([]);
  act(() => result.current.setIsSearchModalOpen(false));
  await act(async () => pending[1].finish('Closed'));
  expect(result.current.searchResults).toEqual([]);
  act(() => result.current.setIsSearchModalOpen(true));
  expect(result.current.searchResults).toEqual([]);
  await tick();
  await act(async () => pending[2].finish('Reopened'));
  expect(result.current.searchResults[0]?.fields?.title).toBe('Reopened');
  act(() => result.current.setSearchQuery('reset me'));
  await tick();
  act(() => result.current.resetSearch());
  await act(async () => pending[3].finish('Reset'));
  expect(result.current.searchResults).toEqual([]);
  expect(result.current.searchQuery).toBe('');
});

it('drops requests and results when the effective application identity/connection scope is replaced', async () => {
  vi.useFakeTimers();
  vi.stubGlobal('EventSource', class { close() {} });
  const pending = requests();
  let identity = 'reader-1';
  const { result, rerender } = renderHook(() => useGlobalSearch(), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <ApplicationSessionProvider identity={identity} isAdmin={false}>{children}</ApplicationSessionProvider>
    ),
  });
  act(() => { result.current.setIsSearchModalOpen(true); result.current.setSearchQuery('private'); });
  await tick();
  identity = 'reader-2';
  rerender();
  await act(async () => pending[0].finish('Private old hit'));
  expect(result.current.searchResults).toEqual([]);
  expect(result.current.isSearchModalOpen).toBe(false);
});
