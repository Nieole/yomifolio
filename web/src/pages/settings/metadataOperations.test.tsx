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
it('shows retained cover comparison and warns that undo also restores text subject to current locks', async () => {
 const cover = { book_id: 8, path: 'ab/original.png', locked: false, automatic: true };
 mocks.get.mockResolvedValue({ data: { operations: [{ ...row, old_cover_material: 'metadata-covers/old.png', new_cover_material: 'metadata-covers/new.jpg', before: { ...state, cover }, after: { ...state, summary: 'Space journey', cover: { ...cover, path: 'metadata-covers/new.jpg' } }, fields: [{ field: 'cover', changed: true, reason: 'Upright complete artwork', sources: [] }, ...row.fields] }] } });
 setup();
 expect((await screen.findByRole('img', { name: 'Previous cover' })).getAttribute('src')).toBe('/api/thumbnails/metadata-covers/old.png');
 expect(screen.getByRole('img', { name: 'Selected cover' }).getAttribute('src')).toBe('/api/thumbnails/metadata-covers/new.jpg');
 expect(screen.getByText('Upright complete artwork')).toBeTruthy();
 expect(screen.getByText(/Cover and text are restored together/)).toBeTruthy();
 fireEvent.click(screen.getByRole('button', { name: 'Undo operation' }));
 expect(screen.getAllByText(/later manual cover/).length).toBeGreaterThan(0);
});
it('shows the authoritative cover undo in reverse direction and marks only successful restoration undone', async () => {
 const cover = { book_id: 8, path: 'metadata-covers/new.jpg', locked: false, automatic: true };
 const original = { ...row, old_cover_material: 'metadata-covers/old.png', new_cover_material: 'metadata-covers/new.jpg', before: { ...state, cover: { ...cover, path: 'ab/original.png' } }, after: { ...state, cover, summary: 'Space journey' }, fields: [{ field: 'cover', changed: true, reason: 'Compared artwork', sources: [] }, ...row.fields] };
 mocks.get.mockResolvedValue({ data: { operations: [original] } });
 mocks.post.mockResolvedValue({ data: { ...original, id: 'undo-cover', kind: 'undo', parent_id: original.id, before: original.after, after: { ...state, cover: { ...cover, path: original.old_cover_material } }, applied: true } });
 setup(); await screen.findByRole('img', { name: 'Previous cover' });
 fireEvent.click(screen.getByRole('button', { name: 'Undo operation' }));
 fireEvent.click(screen.getByRole('button', { name: 'Confirm undo' }));
 await screen.findByText('Undone');
 expect(screen.getAllByRole('img', { name: 'Previous cover' })[0].getAttribute('src')).toBe('/api/thumbnails/metadata-covers/new.jpg');
 expect(screen.getAllByRole('img', { name: 'Selected cover' })[0].getAttribute('src')).toBe('/api/thumbnails/metadata-covers/old.png');
 expect(screen.queryByRole('button', { name: 'Undo operation' })).toBeNull();
});
it('does not present a failed cover undo as a restored image or discard the original undo option', async () => {
 const original = { ...row, old_cover_material: 'metadata-covers/old.png', new_cover_material: 'metadata-covers/new.jpg', fields: [{ field: 'cover', changed: true, reason: 'Compared artwork', sources: [] }] };
 mocks.get.mockResolvedValue({ data: { operations: [original] } });
 mocks.post.mockResolvedValue({ data: { ...original, id: 'failed-cover', kind: 'undo', parent_id: original.id, outcome: 'failed', before: original.after, after: original.after, fields: [{ ...original.fields[0], changed: false }], applied: false } });
 setup(); await screen.findByRole('img', { name: 'Previous cover' });
 fireEvent.click(screen.getByRole('button', { name: 'Undo operation' }));
 fireEvent.click(screen.getByRole('button', { name: 'Confirm undo' }));
 await screen.findByRole('status');
 expect(screen.queryByText('Undone')).toBeNull();
 expect(screen.getAllByRole('img')).toHaveLength(2);
 expect(screen.getByRole('button', { name: 'Undo operation' })).toBeTruthy();
});

it('shows series-local author names and roles before and after correction and safe undo', async () => {
 const authorRow = { ...row, before: { ...state, authors: [{ name: 'Alias', role: 'Artist' }] }, after: { ...state, authors: [{ name: 'Canonical', role: 'Writer' }] }, fields: [{ ...row.fields[0], field: 'authors', reason: 'Published credit confirms pen name' }] };
 mocks.get.mockResolvedValue({ data: { operations: [authorRow] } });
 mocks.post.mockResolvedValue({ data: { ...authorRow, id: 'author-undo', kind: 'undo', outcome: 'conflict' } });
 setup();
 await screen.findByText('Alias · Artist');
 expect(screen.getByText('Canonical · Writer')).toBeTruthy();
 expect(screen.getByText('Authors')).toBeTruthy();
 expect(screen.getByText('Published credit confirms pen name')).toBeTruthy();
 fireEvent.click(screen.getByRole('button', { name: 'Undo operation' }));
 fireEvent.click(screen.getByRole('button', { name: 'Confirm undo' }));
 await screen.findByRole('status');
 expect(screen.queryByText('Undone')).toBeNull();
});

it('warns of permanent rollback loss and cleans without a per-batch confirmation workflow', async () => {
 mocks.get.mockResolvedValue({ data: { operations: [row] } });
 mocks.post.mockResolvedValue({ data: { id: 'cleanup1', operation_id: row.id, outcome: 'partial', undo_unavailable: true, materials: [{ key: 'metadata-covers/old.png', outcome: 'failed' }] } });
 setup(); await screen.findByText('Space journey');
 expect(screen.getByText(/permanently discards rollback snapshots/)).toBeTruthy();
 fireEvent.click(screen.getByRole('button', { name: 'Clean rollback material' }));
 await screen.findByText(/Some files could not be reclaimed/);
 expect(mocks.post).toHaveBeenCalledWith('/api/mcp/operations/op1/cleanup', expect.objectContaining({ request_key: expect.any(String) }));
 expect(screen.queryByRole('button', { name: 'Undo operation' })).toBeNull();
 expect(screen.queryByText('Space journey')).toBeNull();
 expect(screen.getByText(/Undo unavailable/)).toBeTruthy();
});
it('keeps cleanup retry identity after an uncertain network result and never claims lossless recovery', async () => {
 mocks.get.mockResolvedValue({ data: { operations: [row] } });
 mocks.post.mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce({ data: { id: 'cleanup2', operation_id: row.id, outcome: 'complete', undo_unavailable: true, materials: [] } });
 setup(); await screen.findByText('Space journey');
 fireEvent.click(screen.getByRole('button', { name: 'Clean rollback material' }));
 await screen.findByRole('alert');
 fireEvent.click(screen.getByRole('button', { name: 'Clean rollback material' }));
 await screen.findByText(/Rollback material cleaned/);
 expect(mocks.post.mock.calls[0]).toEqual(mocks.post.mock.calls[1]);
});
