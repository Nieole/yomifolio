/** @vitest-environment jsdom */
/**
 * 守资料库菜单那条入口：给了资料库就只预取它，请求体里带的是这个资料库。
 */
import { afterEach, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { ToastProvider } from '../../components/ToastProvider';
import { messages } from '../../i18n/locales/en-US';
import { useEvidencePrefetch } from './useEvidencePrefetch';

const mocks = vi.hoisted(() => ({ post: vi.fn() }));
vi.mock('../../api/client', () => ({ apiClient: { defaults: { headers: { common: {} } }, post: mocks.post } }));

afterEach(() => { vi.resetAllMocks(); });

it('prefetches only the library it is given', async () => {
  mocks.post.mockResolvedValue({ data: { run_id: 4, series: 12 } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <LocaleProvider initialLocale="en-US" initialMessages={messages} fallbackMessages={messages}><ToastProvider>{children}</ToastProvider></LocaleProvider>
  );
  const { result } = renderHook(() => useEvidencePrefetch(), { wrapper });
  await act(async () => { await result.current(7); });
  expect(mocks.post).toHaveBeenCalledWith('/api/identification/prefetch', { library_id: 7 });
});
