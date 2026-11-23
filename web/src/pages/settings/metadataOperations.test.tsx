/** @vitest-environment jsdom */
import { afterEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { SettingsMetadataOperationsPage } from './SettingsMetadataOperationsPage';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages } from '../../i18n/locales/en-US';
import { messages as zhMessages } from '../../i18n/locales/zh-CN';
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

it('distinguishes partial acceptance, supplement-and-close and complete rejection without undoing accepted fields', async () => {
 const adjudication = { proposal_id: 21, mode: 'partial', reason: 'Verified missing description', status: 'pending', accepted: true };
 mocks.get.mockResolvedValue({ data: { operations: [
  { ...row, id: 'partial', adjudication },
  { ...row, id: 'closed', adjudication: { ...adjudication, mode: 'reject', status: 'supplemented_closed', reason: 'Discard only remaining differences' }, outcome: 'no-change', fields: [] },
  { ...row, id: 'rejected', adjudication: { ...adjudication, mode: 'reject', status: 'rejected', accepted: false, reason: 'Wrong edition' }, outcome: 'no-change', fields: [] },
 ] } });
 setup();
 await screen.findByText('Partially applied; still pending');
 expect(screen.getByText('Supplemented; remaining differences closed')).toBeTruthy();
 expect(screen.getByText('All differences rejected')).toBeTruthy();
 expect(screen.getByText('Discard only remaining differences')).toBeTruthy();
 expect(screen.getAllByRole('button', { name: 'Undo operation' })).toHaveLength(1);
 expect(screen.getByText('Space journey')).toBeTruthy();
});
it.each([
 ['en-US', messages, 'Some content accepted; remaining differences rejected', 'All differences rejected', 'Conflict'],
 ['zh-CN', zhMessages, '已接受部分内容，剩余差异已拒绝', '全部差异已拒绝', '冲突'],
] as const)('shows rejected acceptance facts alongside the conflict outcome in %s', async (locale, translations, acceptedLabel, rejectedLabel, conflictLabel) => {
 const adjudication = { proposal_id: 21, mode: 'supplement_and_close', reason: 'Already manually rejected', status: 'rejected' };
 mocks.get.mockResolvedValue({ data: { operations: [
  { ...row, id: 'accepted-rejected', adjudication: { ...adjudication, accepted: true }, outcome: 'conflict', fields: [] },
  { ...row, id: 'pure-rejected', adjudication: { ...adjudication, accepted: false }, outcome: 'no-change', fields: [] },
  { ...row, id: 'pending', adjudication: { ...adjudication, status: 'pending', accepted: false }, outcome: 'no-change', fields: [] },
  { ...row, id: 'applied', adjudication: { ...adjudication, status: 'applied', accepted: true }, outcome: 'no-change', fields: [] },
 ] } });
 render(<LocaleProvider initialLocale={locale} initialMessages={translations} fallbackMessages={messages}><SettingsMetadataOperationsPage /></LocaleProvider>);
 const accepted = await screen.findByText(acceptedLabel);
 const conflict = within(accepted.closest('section')!);
 expect(conflict.getByRole('heading', { name: new RegExp(conflictLabel) })).toBeTruthy();
 expect(conflict.queryByText(rejectedLabel)).toBeNull();
 expect(screen.getAllByText(rejectedLabel)).toHaveLength(1);
 expect(screen.getByText(translations['audit.proposal.pending'])).toBeTruthy();
 expect(screen.getByText(translations['audit.proposal.applied'])).toBeTruthy();
 expect(screen.queryByText(translations['audit.proposal.supplemented_closed'])).toBeNull();
 expect(screen.queryByRole('button', { name: translations['audit.undo'] })).toBeNull();
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

it('recovers pending cleanup from durable cleaned_by after full unmount and resumes the original key', async () => {
 const isolated = { ...row, undo_unavailable: true, cleaned_by: 'cleanup-persisted', fields: [] };
 const pending = { id: 'cleanup-persisted', operation_id: row.id, outcome: 'pending', undo_unavailable: true, retry_request_key: 'original-server-key', materials: [] };
 mocks.get.mockImplementation((url: string) => Promise.resolve({ data: url === '/api/mcp/operations' ? { operations: [isolated] } : pending }));
 setup();
 await screen.findByText(messages['audit.cleanup.pending']);
 cleanup(); // component-local request maps cannot survive a reload
 setup();
 await screen.findByText(messages['audit.cleanup.pending']);
 expect(screen.queryByRole('button', { name: 'Undo operation' })).toBeNull();
 mocks.post.mockRejectedValueOnce(new Error('cancelled')).mockResolvedValueOnce({ data: { ...pending, outcome: 'complete', replayed: true } });
 fireEvent.click(screen.getByRole('button', { name: 'Resume cleanup' }));
 await screen.findByRole('alert');
 fireEvent.click(screen.getByRole('button', { name: 'Resume cleanup' }));
 await screen.findByText(messages['audit.cleanup.complete']);
 expect(mocks.post.mock.calls).toEqual([
  ['/api/mcp/operations/op1/cleanup', { request_key: 'original-server-key' }],
  ['/api/mcp/operations/op1/cleanup', { request_key: 'original-server-key' }],
 ]);
 expect(screen.queryByRole('button', { name: 'Resume cleanup' })).toBeNull();
});

it('groups durable lookups by cleanup identity without applying its retry key to another target', async () => {
 const isolated = { ...row, undo_unavailable: true, cleaned_by: 'shared-result', fields: [] };
 mocks.get.mockImplementation((url: string) => Promise.resolve({ data: url === '/api/mcp/operations' ? { operations: [isolated, { ...isolated, id: 'op2', series_id: 8 }] } : { id: 'shared-result', operation_id: 'op1', outcome: 'pending', retry_request_key: 'only-op1', materials: [] } }));
 setup();
 await screen.findAllByText(messages['audit.cleanup.pending']);
 expect(mocks.get.mock.calls.filter(([url]) => url === '/api/mcp/cleanups/shared-result')).toHaveLength(1);
 expect(screen.getAllByRole('button', { name: 'Resume cleanup' })).toHaveLength(1);
 expect(screen.queryByRole('button', { name: 'Clean rollback material' })).toBeNull();
});

it('inspects another actor pending receipt without allocating a substitute retry key', async () => {
 mocks.get.mockImplementation((url: string) => Promise.resolve({ data: url === '/api/mcp/operations' ? { operations: [{ ...row, undo_unavailable: true, cleaned_by: 'agent-cleanup', fields: [] }] } : { id: 'agent-cleanup', operation_id: row.id, outcome: 'pending', materials: [] } }));
 setup(); await screen.findByText(messages['audit.cleanup.pending']);
 expect(screen.queryByRole('button', { name: 'Resume cleanup' })).toBeNull();
 expect(screen.queryByRole('button', { name: 'Clean rollback material' })).toBeNull();
 expect(mocks.post).not.toHaveBeenCalled();
});

it('allows a failed durable result lookup to be retried without claiming completion', async () => {
 mocks.get.mockResolvedValueOnce({ data: { operations: [{ ...row, undo_unavailable: true, cleaned_by: 'cleanup3', fields: [] }] } })
  .mockRejectedValueOnce(new Error('network'))
  .mockResolvedValueOnce({ data: { id: 'cleanup3', operation_id: row.id, outcome: 'pending', retry_request_key: 'key3', materials: [] } });
 setup(); await screen.findByRole('alert');
 expect(screen.queryByText(messages['audit.cleanup.complete'])).toBeNull();
 fireEvent.click(screen.getByRole('button', { name: 'Reload cleanup result' }));
 await screen.findByRole('button', { name: 'Resume cleanup' });
});
