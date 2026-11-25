/** @vitest-environment jsdom */
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { SettingsMCPPage } from './SettingsMCPPage';
import { SettingsProvider } from '../../features/settings-editor';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages } from '../../i18n/locales/en-US';

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), patch: vi.fn(), remove: vi.fn() }));
vi.mock('../../api/client', () => ({
  apiClient: { defaults: { headers: { common: {} } }, get: mocks.get, post: mocks.post, patch: mocks.patch, delete: mocks.remove },
  getApiErrorMessage: () => 'Restrictions changed; reload and retry',
  isAxiosError: () => false,
}));
vi.mock('../../auth/AuthProvider', () => ({ useAuth: () => ({ isAdmin: true, user: { id: 1 } }) }));
vi.mock('../../features/mcp-docs', () => ({ docHtml: (locale: string) => (locale === 'zh-CN' ? '<h1>客户端配置说明</h1>' : '<h1>Client setup guide</h1><p>Wire the client.</p>') }));

const config = {
  server: { host: '0.0.0.0', port: 8080, allowed_origins: [] },
  database: { path: '/data/manga.db' },
  library: { paths: [], storage_profile: 'balanced', io_policy: {}, storage_policies: [] },
  cache: { dir: '/data/cache', page_disk_cache_enabled: false },
  logging: { level: 'info' },
  scanner: { workers: 4, scan_profile: 'fast_scan', thumbnail_format: 'webp', waifu2x_path: '', realcugan_path: '', archive_pool_size: 8, max_ai_concurrency: 1 },
  tasks: {},
  llm: { provider: 'ollama', api_mode: 'responses', base_url: '', request_path: '', endpoint: '', model: '', api_key: '', timeout: 120 },
  protocols: { opds: { enabled: false }, mihon: { enabled: false } },
  koreader: { enabled: false, base_path: '', allow_registration: false, match_mode: 'binary_hash', path_ignore_extension: false },
  mcp: { public_url: 'https://library.example.com', allow_lan_http: false },
};

function configResponse(overrides: Record<string, unknown> = {}) {
  return { data: { config: { ...config, ...overrides }, validation: { valid: true, issues: [] }, capabilities: {} }, headers: { etag: '"one"' } };
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.get.mockImplementation(async (path: string) => {
    if (path === '/api/system/config') return configResponse();
    if (path === '/api/users') return { data: [{ id: 1, username: 'owner' }] };
    if (path === '/api/libraries') return { data: [{ id: 7, name: 'Comics' }] };
    return { data: [], headers: {} };
  });
  mocks.post.mockResolvedValue({ data: { config, validation: { valid: true, issues: [] } }, headers: { etag: '"two"' } });
});

afterEach(() => { cleanup(); window.localStorage.clear(); });

function setup() {
  render(
    <LocaleProvider initialLocale="en-US" initialMessages={messages} fallbackMessages={messages}>
      <SettingsProvider>
        <SettingsMCPPage />
      </SettingsProvider>
    </LocaleProvider>,
  );
}

// codeBlock 在注入的内联示例里按内容找节点：示例是纯文本段落，按 tagName + 内容定位最稳。
function codeBlock(needle: string) {
  return screen.getByText((_, element) => element?.tagName === 'CODE' && (element.textContent || '').includes(needle));
}

it('shows the derived endpoint and the two client configurations', async () => {
  setup();
  expect(await screen.findByDisplayValue('https://library.example.com')).toBeTruthy();
  expect(screen.getAllByText(/https:\/\/library\.example\.com\/mcp/).length).toBeGreaterThan(0);
  expect(codeBlock('"type": "http"')).toBeTruthy();
  expect(codeBlock('mcp-proxy')).toBeTruthy();
});

it('saves only the mcp section with the published version as precondition', async () => {
  setup();
  const input = await screen.findByDisplayValue('https://library.example.com');
  fireEvent.change(input, { target: { value: 'https://books.example.net/' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save server settings' }));
  await waitFor(() => expect(mocks.post).toHaveBeenCalled());
  const [path, body, options] = mocks.post.mock.calls[0] as [string, Record<string, unknown>, { headers: Record<string, string> }];
  expect(path).toBe('/api/system/config');
  expect(body.mcp).toEqual({ public_url: 'https://books.example.net', allow_lan_http: false });
  // 其他分区来自服务端基线，不能被这张卡片顺手写回。
  expect(body.llm).toEqual(config.llm);
  expect(options.headers['If-Match']).toBe('"one"');
});

it('accepts a typed URL without swallowing the scheme slashes', async () => {
  mocks.get.mockImplementation(async (path: string) => path === '/api/system/config'
    ? configResponse({ mcp: { public_url: '', allow_lan_http: false } })
    : { data: [] });
  setup();
  const input = (await screen.findByPlaceholderText('https://library.example.com')) as HTMLInputElement;
  const target = 'https://books.example.net';
  // 每个字符都接在“目前渲染出来的值”后面，模拟真实键入：
  // 被输入框吞掉字符后，后续按键不会自己把它补回来。
  let rendered = input.value;
  for (const char of target) {
    fireEvent.change(input, { target: { value: rendered + char } });
    rendered = input.value;
  }
  expect(rendered).toBe(target);
});

it('keeps the plaintext LAN switch next to its risk and saves the flag', async () => {
  setup();
  const toggle = await screen.findByLabelText('Allow plaintext LAN HTTP');
  expect(screen.getByText(/credential is sent unencrypted/i)).toBeTruthy();
  fireEvent.click(toggle);
  fireEvent.click(screen.getByRole('button', { name: 'Save server settings' }));
  await waitFor(() => expect(mocks.post).toHaveBeenCalled());
  expect((mocks.post.mock.calls[0][1] as { mcp: { allow_lan_http: boolean } }).mcp.allow_lan_http).toBe(true);
});

it('surfaces server validation for the public URL', async () => {
  mocks.get.mockImplementation(async (path: string) => path === '/api/system/config'
    ? { data: { config, validation: { valid: false, issues: [{ field: 'mcp.public_url', message: 'must be an absolute https URL' }] }, capabilities: {} }, headers: { etag: '"one"' } }
    : { data: [] });
  setup();
  expect(await screen.findByText('must be an absolute https URL')).toBeTruthy();
});

it('explains that /mcp answers 404 while no public URL is configured', async () => {
  mocks.get.mockImplementation(async (path: string) => path === '/api/system/config'
    ? configResponse({ mcp: { public_url: '', allow_lan_http: false } })
    : { data: [] });
  setup();
  expect(await screen.findByText(/returns 404/i)).toBeTruthy();
});

it('opens the in-app guide rendered from the tracked markdown', async () => {
  setup();
  fireEvent.click(await screen.findByRole('button', { name: 'Read the full guide' }));
  expect(await screen.findByText('Client setup guide')).toBeTruthy();
});

it('collapses the setup guide and remembers it', async () => {
  setup();
  await screen.findByDisplayValue('https://library.example.com');
  expect(screen.queryAllByText(/mcp-proxy/).length).toBeGreaterThan(0);
  fireEvent.click(screen.getByRole('button', { name: 'Collapse setup guide' }));
  expect(screen.queryAllByText(/mcp-proxy/)).toHaveLength(0);
  expect(window.localStorage.getItem('mcp-guide-collapsed')).toBe('1');
});
