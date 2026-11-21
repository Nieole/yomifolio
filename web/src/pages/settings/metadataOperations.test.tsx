/** @vitest-environment jsdom */
import { afterEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { SettingsMetadataOperationsPage } from './SettingsMetadataOperationsPage';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages } from '../../i18n/locales/en-US';
const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), admin: true }));
vi.mock('../../api/client', () => ({ apiClient: { defaults: { headers: { common: {} } }, get: mocks.get, post: mocks.post }, getApiErrorMessage: () => 'Operation unavailable' }));
vi.mock('../../auth/AuthProvider', () => ({ useAuth: () => ({ isAdmin: mocks.admin, user: { id: 1 } }) }));
const state = { title: null, summary: null, publisher: null, status: null, rating: null, tags: [], links: [] };
const row = { version: 1, id: 'op1', request_key: 'request', actor: { user_id: 1, credential_id: 'agent1' }, series_id: 7, library_id: 1, kind: 'enrich', created_at: '2026-11-16T00:00:00Z', outcome: 'changed', reason: '', before: state, after: { ...state, summary: 'Space journey' }, fields: [{ field: 'summary', changed: true, reason: 'Same edition', sources: [{ source: 'bangumi', metadata: { SourceURL: 'https://bgm.tv/subject/1' } }] }], applied: false, replayed: false };
function setup() { render(<LocaleProvider initialLocale="en-US" initialMessages={messages} fallbackMessages={messages}><SettingsMetadataOperationsPage /></LocaleProvider>); }
afterEach(() => { cleanup(); vi.resetAllMocks(); mocks.admin = true; });
it('shows complete field evidence and adopts the authoritative undo result', async () => {
 mocks.get.mockResolvedValue({ data: { operations: [row] } });
 mocks.post.mockResolvedValue({ data: { ...row, id: 'undo1', kind: 'undo', parent_id: 'op1', applied: true } });
 setup(); await screen.findByText('Space journey');
 expect(screen.getByText('Same edition')).toBeTruthy(); expect(screen.getByRole('link', { name: 'bangumi' }).getAttribute('href')).toBe('https://bgm.tv/subject/1');
 fireEvent.click(screen.getByRole('button', { name: 'Undo operation' }));
 fireEvent.click(screen.getByRole('button', { name: 'Confirm undo' }));
 await screen.findByText('Undone'); expect(mocks.post).toHaveBeenCalledWith('/api/mcp/operations/op1/undo', expect.objectContaining({ request_key: expect.any(String) }));
});
it('reports conflicts without pretending restoration succeeded', async () => {
 mocks.get.mockResolvedValue({ data: { operations: [row] } }); mocks.post.mockResolvedValue({ data: { ...row, id: 'conflict', outcome: 'conflict', applied: false } });
 setup(); await screen.findByText('Space journey'); fireEvent.click(screen.getByRole('button', { name: 'Undo operation' })); fireEvent.click(screen.getByRole('button', { name: 'Confirm undo' }));
 await screen.findByRole('status'); expect(screen.queryByText('Undone')).toBeNull();
});
it('does not query or render the management page for regular accounts', () => { mocks.admin = false; setup(); expect(mocks.get).not.toHaveBeenCalled(); expect(screen.queryByText('Metadata operations')).toBeNull(); });
it('appends a server cursor page without duplicating an existing receipt', async () => {
 mocks.get.mockResolvedValueOnce({ data: { operations: [row], next_before: 12 } }).mockResolvedValueOnce({ data: { operations: [row, { ...row, id: 'op2', series_id: 8 }] } });
 setup(); await screen.findByText('Space journey');
 fireEvent.click(screen.getByRole('button', { name: messages['audit.more'] }));
 await screen.findByText(/#8/);
 expect(mocks.get).toHaveBeenLastCalledWith('/api/mcp/operations?before=12');
 expect(screen.getAllByText('Space journey')).toHaveLength(2);
});
it('retains the same request identity when a network failure makes undo uncertain', async () => {
 mocks.get.mockResolvedValue({ data: { operations: [row] } });
 mocks.post.mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce({ data: { ...row, id: 'undo-retry', kind: 'undo', parent_id: 'op1', applied: false, replayed: true } });
 setup(); await screen.findByText('Space journey');
 fireEvent.click(screen.getByRole('button', { name: 'Undo operation' }));
 fireEvent.click(screen.getByRole('button', { name: 'Confirm undo' }));
 await screen.findByRole('alert');
 fireEvent.click(screen.getByRole('button', { name: 'Confirm undo' }));
 await screen.findByText('Undone');
 expect(mocks.post.mock.calls[0]).toEqual(mocks.post.mock.calls[1]);
});
