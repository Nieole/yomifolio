/** @vitest-environment jsdom */
/**
 * 守维护页的全库证据包预取入口：确认后启动的是预取而不是旧的首条结果刮削，提示照实报告
 * 要预取的系列数，没有待预取的系列时也不假装启动了运行。
 */
import { afterEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { SettingsMaintenancePage } from './SettingsMaintenancePage';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { ToastProvider } from '../../components/ToastProvider';
import { messages } from '../../i18n/locales/en-US';

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock('../../api/client', () => ({ apiClient: { defaults: { headers: { common: {} } }, get: mocks.get, post: mocks.post } }));

function setup() {
  mocks.get.mockImplementation(async (url: string) => ({ data: url === '/api/system/storage-io' ? { scheduler: [], libraries: [] } : { path: '', file_size: 0, file_count: 0 } }));
  render(
    <LocaleProvider initialLocale="en-US" initialMessages={messages} fallbackMessages={messages}>
      <ToastProvider><SettingsMaintenancePage /></ToastProvider>
    </LocaleProvider>,
  );
}

async function startFromMaintenance() {
  fireEvent.click(screen.getByRole('button', { name: new RegExp(messages['settings.maintenance.prefetch']) }));
  expect(screen.getByText(messages['settings.maintenance.prefetchConfirm'])).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: messages['modal.confirm'] }));
}

afterEach(() => { cleanup(); vi.resetAllMocks(); });

it('starts the evidence prefetch over every library and reports how many series it covers', async () => {
  mocks.post.mockResolvedValue({ data: { run_id: 9, series: 3 } });
  setup();
  await startFromMaintenance();
  expect(await screen.findByText('Preparing evidence for 3 series in the background')).toBeTruthy();
  expect(mocks.post).toHaveBeenCalledTimes(1);
  expect(mocks.post).toHaveBeenCalledWith('/api/identification/prefetch', {});
});

it('says so when no series needs evidence instead of claiming a run started', async () => {
  mocks.post.mockResolvedValue({ data: { series: 0 } });
  setup();
  await startFromMaintenance();
  expect(await screen.findByText(messages['identification.prefetch.nothingPending'])).toBeTruthy();
});

it('reports a failed launch', async () => {
  mocks.post.mockRejectedValue(new Error('offline'));
  vi.spyOn(console, 'error').mockImplementation(() => {});
  setup();
  await startFromMaintenance();
  expect(await screen.findByText(messages['identification.prefetch.failed'])).toBeTruthy();
});
