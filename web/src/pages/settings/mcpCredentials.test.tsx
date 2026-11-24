/** @vitest-environment jsdom */
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { SettingsMCPPage } from './SettingsMCPPage';
import { SettingsProvider } from '../../features/settings-editor';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages } from '../../i18n/locales/en-US';
const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), patch: vi.fn(), remove: vi.fn() }));
vi.mock('../../api/client', () => ({ apiClient: { defaults: { headers: { common: {} } }, get: mocks.get, post: mocks.post, patch: mocks.patch, delete: mocks.remove }, getApiErrorMessage: () => 'Restrictions changed; reload and retry' }));
vi.mock('../../auth/AuthProvider', () => ({ useAuth: () => ({ isAdmin: true, user: { id: 1 } }) }));
// 页面现在会读服务器配置（mcp 分区）并渲染站内说明，凭据用例只关心凭据接口，故把两者一并替换。
vi.mock('../../features/mcp-docs', () => ({ docHtml: () => '<h1>Client setup guide</h1>' }));
const row = { id: 'credential', user_id: 1, name: 'Catalogue', library_ids: [7], capabilities: ['read', 'cleanup'], revoked: false };
const config = { server: {}, database: {}, library: {}, cache: {}, logging: {}, scanner: {}, tasks: {}, llm: {}, protocols: {}, koreader: {}, mcp: { public_url: 'https://library.example.com', allow_lan_http: false } };
beforeEach(() => { vi.resetAllMocks(); mocks.get.mockImplementation((path: string) => Promise.resolve(path === '/api/system/config'
  ? { data: { config, validation: { valid: true, issues: [] }, capabilities: {} }, headers: { etag: '"one"' } }
  : { data: path === '/api/users' ? [{ id: 1, username: 'owner' }] : path === '/api/libraries' ? [{ id: 7, name: 'Comics' }] : [row], headers: {} })); });
afterEach(() => { cleanup(); window.localStorage.clear(); });
function setup() { render(<LocaleProvider initialLocale="en-US" initialMessages={messages} fallbackMessages={messages}><SettingsProvider><SettingsMCPPage /></SettingsProvider></LocaleProvider>); }
it('issues independent scoped credentials and shows the secret only until dismissed', async () => {
 mocks.post.mockResolvedValue({ data: { ...row, id: 'new', name: 'Research', token: 'one-time-secret' } }); setup();
 await screen.findByText('Catalogue');
 fireEvent.change(screen.getByLabelText('Credential name'), { target: { value: 'Research' } });
 fireEvent.click(screen.getByLabelText('Comics'));
 fireEvent.click(screen.getByRole('button', { name: 'Issue credential' }));
 await screen.findByText('one-time-secret');
 expect(mocks.post).toHaveBeenCalledWith('/api/mcp/credentials', { user_id: 1, name: 'Research', library_ids: [7], capabilities: ['read'] });
 fireEvent.click(screen.getByRole('button', { name: 'Dismiss secret' }));
 expect(screen.queryByText('one-time-secret')).toBeNull();
});
it('narrows from the authoritative result and revokes without a list reload', async () => {
 mocks.patch.mockResolvedValue({ data: { ...row, capabilities: ['read'] } });
 mocks.remove.mockResolvedValue({ data: { revoked: true } }); setup(); await screen.findByText('Catalogue');
 fireEvent.click(screen.getByRole('button', { name: 'Narrow restrictions' }));
 fireEvent.click(screen.getByLabelText('Cleanup'));
 fireEvent.click(screen.getByRole('button', { name: 'Save restrictions' }));
 await waitFor(() => expect(mocks.patch).toHaveBeenCalledWith('/api/mcp/credentials/credential', expect.objectContaining({ capabilities: ['read'] })));
 await waitFor(() => expect(screen.queryByRole('button', { name: 'Save restrictions' })).toBeNull());
 fireEvent.click(screen.getByRole('button', { name: 'Revoke' }));
 fireEvent.click(screen.getByRole('button', { name: 'Confirm revocation' }));
 await screen.findByText('Revoked'); expect(mocks.get.mock.calls.filter(([path]) => path === '/api/mcp/credentials')).toHaveLength(1);
});
it('keeps existing restrictions on conflict', async () => {
 mocks.patch.mockRejectedValue(new Error('conflict')); setup(); await screen.findByText('Catalogue');
 fireEvent.click(screen.getByRole('button', { name: 'Narrow restrictions' }));
 fireEvent.click(screen.getByLabelText('Cleanup')); fireEvent.click(screen.getByRole('button', { name: 'Save restrictions' }));
 await screen.findByRole('alert'); expect(screen.getByRole('button', { name: 'Save restrictions' })).toBeTruthy();
});
