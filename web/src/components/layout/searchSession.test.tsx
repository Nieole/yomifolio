// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { apiClient } from '../../api/client';
import { useGlobalSearch } from './useGlobalSearch';

const original = apiClient.defaults.adapter;
afterEach(() => { cleanup(); apiClient.defaults.adapter = original; vi.useRealTimers(); });
it('distinguishes waiting/loading/error/empty and retries the same query without debounce', async () => {
  vi.useFakeTimers();
  const replies: { fail: () => void; empty: () => void }[] = [];
  apiClient.defaults.adapter = config => new Promise((resolve, reject) => {
    replies.push({ fail: () => reject(new Error('offline')), empty: () => resolve({ data: { hits: [] }, status: 200, statusText: 'OK', headers: {}, config }) });
  });
  const { result } = renderHook(() => useGlobalSearch());
  expect(result.current.searchStatus).toBe('idle');
  act(() => { result.current.setIsSearchModalOpen(true); result.current.setSearchQuery('comic'); });
  expect(result.current.searchStatus).toBe('debouncing');
  await act(async () => { await vi.advanceTimersByTimeAsync(300); });
  expect(result.current.searchStatus).toBe('loading');
  await act(async () => replies[0].fail());
  expect(result.current.searchStatus).toBe('error');
  await act(async () => result.current.retrySearch());
  expect(replies).toHaveLength(2);
  expect(result.current.searchStatus).toBe('loading');
  await act(async () => replies[1].empty());
  expect(result.current.searchStatus).toBe('success');
  expect(result.current.searchResults).toEqual([]);
});
