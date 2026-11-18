// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import Layout from '../Layout';
import { AuthProvider } from '../../auth/AuthProvider';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { ToastProvider } from '../ToastProvider';
import { ApplicationSessionProvider } from '../../session/ApplicationSessionProvider';
import { apiClient } from '../../api/client';
import type { SearchHit } from './types';

const original = apiClient.defaults.adapter;
afterEach(() => { cleanup(); apiClient.defaults.adapter = original; vi.unstubAllGlobals(); });
function Path() { return <output data-testid="path">{useLocation().pathname}</output>; }
function mount() {
  vi.stubGlobal('EventSource', class { close() {} });
  const pending: { signal: AbortSignal | undefined; finish: (hits: SearchHit[]) => void; fail: () => void }[] = [];
  apiClient.defaults.adapter = config => {
    if (config.url?.startsWith('/api/search?')) return new Promise((resolve, reject) => {
      const signal = config.signal as AbortSignal | undefined;
      config.signal = undefined; // The real axios client, with a deliberately uncancellable HTTP adapter.
      pending.push({ signal, finish: hits => resolve({ data: { hits }, status: 200, statusText: 'OK', headers: {}, config }), fail: () => reject(new Error('offline')) });
    });
    const data = config.url === '/api/auth/status'
      ? { authenticated: true, setup_required: false, user: { id: 1, username: 'reader', role: 'regular' } } : [];
    return Promise.resolve({ data, status: 200, statusText: 'OK', headers: {}, config });
  };
  render(<MemoryRouter><LocaleProvider><ToastProvider><AuthProvider>
    <ApplicationSessionProvider identity="reader-1" isAdmin={false}>
      <Path /><Routes><Route element={<Layout />}><Route path="*" element={<div>Page</div>} /></Route></Routes>
    </ApplicationSessionProvider>
  </AuthProvider></ToastProvider></LocaleProvider></MemoryRouter>);
  return pending;
}
const hit = (id: string, title: string): SearchHit => ({ id, fields: { title, type: id.startsWith('s_') ? 'series' : 'book' } });
async function open() {
  fireEvent.keyDown(window, { key: 'k', ctrlKey: true });
  return await screen.findByPlaceholderText('输入关键字搜索...');
}
const change = (input: HTMLElement, value: string) => fireEvent.change(input, { target: { value } });
const key = (input: HTMLElement, value: string) => fireEvent.keyDown(input, { key: value });

it('real layout/modal announces wait, failure and immediate retry; only successful zero is empty', async () => {
  const pending = mount();
  const input = await open();
  expect(screen.getByText('支持全局模糊检索、键盘上下方向键导航')).toBeTruthy();
  change(input, 'first');
  expect(within(screen.getByRole('dialog')).getByRole('status').textContent).toContain('等待搜索');
  expect(screen.queryByText('未找到符合条件的漫画')).toBeNull();
  key(input, 'ArrowUp'); key(input, 'ArrowDown'); key(input, 'Enter');
  expect(screen.getByTestId('path').textContent).toBe('/');
  await waitFor(() => expect(pending).toHaveLength(1));
  expect(within(screen.getByRole('dialog')).getByRole('status').textContent).toContain('正在搜索');
  await act(async () => pending[0].fail());
  expect(screen.getByRole('alert').textContent).toContain('搜索失败');
  expect(screen.queryByText('未找到符合条件的漫画')).toBeNull();
  await act(async () => fireEvent.click(screen.getByRole('button', { name: '重试' })));
  expect(pending).toHaveLength(2); // no 300ms waitFor here
  await act(async () => pending[1].finish([]));
  expect(within(screen.getByRole('dialog')).getByRole('status').textContent).toContain('未找到符合条件的漫画');
  key(input, 'Enter');
  expect(screen.getByTestId('path').textContent).toBe('/');
});

it('real layout only navigates current hits, clamps keys and keeps series/book destinations', async () => {
  const pending = mount();
  let input = await open();
  change(input, 'first');
  await waitFor(() => expect(pending).toHaveLength(1));
  await act(async () => pending[0].finish([hit('s_1', 'Old'), hit('s_2', 'Old second')]));
  key(input, 'ArrowDown');
  change(input, 'second');
  expect(screen.queryByText('Old')).toBeNull();
  key(input, 'Enter');
  expect(screen.getByTestId('path').textContent).toBe('/');
  await waitFor(() => expect(pending).toHaveLength(2));
  fireEvent.click(screen.getByRole('button', { name: '仅系列' }));
  expect(pending[1].signal?.aborted).toBe(true);
  await waitFor(() => expect(pending).toHaveLength(3));
  await act(async () => pending[1].finish([hit('s_9', 'Stale target')]));
  expect(screen.queryByText('Stale target')).toBeNull();
  await act(async () => pending[2].finish([hit('s_3', 'Current series')]));
  key(input, 'ArrowDown'); key(input, 'ArrowDown'); key(input, 'Enter');
  expect(screen.getByTestId('path').textContent).toBe('/series/3');
  input = await open();
  expect((input as HTMLInputElement).value).toBe('');
  change(input, 'book');
  await waitFor(() => expect(pending).toHaveLength(4));
  await act(async () => pending[3].finish([hit('b_4', 'Current book')]));
  fireEvent.click(screen.getByText('Current book'));
  expect(screen.getByTestId('path').textContent).toBe('/reader/4');
});

it('modal close/reopen, rapid typing and reset cannot be backfilled by late responses or errors', async () => {
  const pending = mount();
  let input = await open();
  change(input, 'before close');
  await waitFor(() => expect(pending).toHaveLength(1));
  key(input, 'Escape');
  expect(pending[0].signal?.aborted).toBe(true);
  input = await open();
  expect((input as HTMLInputElement).value).toBe('before close');
  await waitFor(() => expect(pending).toHaveLength(2));
  await act(async () => pending[0].finish([hit('s_8', 'Closed result')]));
  expect(screen.queryByText('Closed result')).toBeNull();
  change(input, 'rapid');
  change(input, 'rapid final');
  await waitFor(() => expect(pending).toHaveLength(3));
  await act(async () => pending[1].fail());
  expect(screen.queryByRole('alert')).toBeNull();
  await act(async () => pending[2].finish([hit('s_5', 'Final')]));
  expect(screen.getByText('Final')).toBeTruthy();
  change(input, 'reset');
  await waitFor(() => expect(pending).toHaveLength(4));
  fireEvent.click(screen.getByRole('button', { name: '清空搜索' }));
  await act(async () => pending[3].finish([hit('s_6', 'After reset')]));
  expect(screen.queryByText('After reset')).toBeNull();
  key(input, 'ArrowDown'); key(input, 'Enter');
  expect(screen.getByTestId('path').textContent).toBe('/');
  expect(screen.getByText('支持全局模糊检索、键盘上下方向键导航')).toBeTruthy();
});
