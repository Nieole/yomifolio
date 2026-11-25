import { useEffect, useRef, useState } from 'react';
import { apiClient, getApiErrorMessage } from '../../api/client';
import type { AdjudicationResult, Operation, Page, State } from '../../api/generated';
import { useAuth } from '../../auth/AuthProvider';
import { useI18n } from '../../i18n/LocaleProvider';
import { ConfirmDialog } from '../../components/ui/ConfirmDialog';
import { useOperationCleanups } from './useOperationCleanups';
import { SettingsPageIntro, sectionClassName } from './shared';

const buttonClass = 'rounded-lg border border-gray-700 px-4 py-2 text-sm text-white hover:bg-gray-800 disabled:opacity-50';
function safeLink(value: string) {
  try { const url = new URL(value); return ['https:', 'http:'].includes(url.protocol) ? url.href : undefined; } catch { return undefined; }
}
function coverImage(key?: string) {
  return key?.startsWith('metadata-covers/') && /^metadata-covers\/[a-zA-Z0-9.-]+$/.test(key) ? `/api/thumbnails/${key}` : undefined;
}
function fieldValue(state: State, field: string) {
  if (field === 'authors') return state.authors?.map((author) => `${author.name} · ${author.role}`).join('\n') || '∅';
  const value = state[field === 'source_links' ? 'links' : field as keyof State];
  if (value === null || value === undefined) return '∅';
  return typeof value === 'string' ? value : JSON.stringify(value);
}

function proposalStatusKey({ status, accepted }: AdjudicationResult) {
  if (accepted && status === 'pending') return 'partial';
  if (accepted && status === 'rejected') return 'accepted_rejected';
  return status;
}

export function SettingsMetadataOperationsPage() {
  const { isAdmin, user } = useAuth();
  return isAdmin && user ? <OperationHistory key={user.id} /> : null;
}

function OperationHistory() {
  const { t } = useI18n();
  const alive = useRef(false);
  const request = useRef(0);
  const [rows, setRows] = useState<Operation[]>([]);
  const [next, setNext] = useState<number>();
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const cleanups = useOperationCleanups(rows, t('audit.failed'));
  const [undo, setUndo] = useState<{ operation: Operation; key: string } | null>(null);
  useEffect(() => {
    alive.current = true;
    const generation = ++request.current;
    apiClient.get<Page>('/api/mcp/operations').then(({ data }) => {
      if (alive.current && request.current === generation) { setRows(data.operations); setNext(data.next_before); }
    }).catch((err) => { if (alive.current && request.current === generation) setError(getApiErrorMessage(err, t('audit.failed'))); })
      .finally(() => { if (alive.current && request.current === generation) setLoading(false); });
    return () => { alive.current = false; };
  }, [t]);
  async function more() {
    if (!next || loading) return;
    const generation = ++request.current;
    setLoading(true); setError('');
    try {
      const { data } = await apiClient.get<Page>(`/api/mcp/operations?before=${next}`);
      if (alive.current && generation === request.current) { setRows((old) => [...old, ...data.operations.filter((row) => !old.some((item) => item.id === row.id))]); setNext(data.next_before); }
    } catch (err) { if (alive.current && request.current === generation) setError(getApiErrorMessage(err, t('audit.failed'))); }
    finally { if (alive.current && request.current === generation) setLoading(false); }
  }
  async function restore() {
    if (!undo || busy) return;
    setBusy(true); setError(''); setNotice('');
    try {
      const { data } = await apiClient.post<Operation>(`/api/mcp/operations/${undo.operation.id}/undo`, { request_key: undo.key });
      if (!alive.current) return;
      setNotice(t(`audit.${data.outcome}`));
      setRows((old) => [data, ...old.filter((row) => row.id !== data.id).map((row) => row.id === undo.operation.id && data.outcome === 'changed' ? { ...row, undone_by: data.id } : row)]);
      setUndo(null);
    } catch (err) { if (alive.current) setError(getApiErrorMessage(err, t('audit.failed'))); }
    finally { if (alive.current) setBusy(false); }
  }
  async function clean(row: Operation) {
    if (busy) return;
    setBusy(true); setError(''); setNotice('');
    try {
      const data = await cleanups.clean(row);
      if (!alive.current) return;
      setNotice('');
      setRows((old) => old.map((item) => item.id === row.id ? { ...item, undo_unavailable: data.undo_unavailable, cleaned_by: data.id, old_cover_material: undefined, new_cover_material: undefined, fields: [] } : item));
    } catch (err) { if (alive.current) setError(getApiErrorMessage(err, t('audit.failed'))); }
    finally { if (alive.current) setBusy(false); }
  }
  return <div className="space-y-6">
    <SettingsPageIntro title={t('audit.title')} description={t('audit.description')} />
    <p className="text-sm text-amber-200">{t('audit.cleanup.warning')}</p>
    {error && <p role="alert" className="text-red-300">{error}</p>}
    {cleanups.error && <div role="alert"><p>{cleanups.error}</p><button className={buttonClass} onClick={cleanups.reload}>{t('audit.cleanup.inspect')}</button></div>}
    {notice && <p role="status" className="text-amber-200">{notice}</p>}
    {!loading && rows.length === 0 && <p>{t('audit.empty')}</p>}
    {rows.map((row) => <section key={row.id} className={`${sectionClassName} space-y-4 min-w-0`}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 className="text-lg font-semibold">{t('audit.series')} #{row.series_id} · {t(`audit.${row.outcome}`)}</h3>
        <time className="text-sm text-gray-400">{new Date(row.created_at).toLocaleString()}</time>
      </div>
      <p className="break-all text-sm text-gray-400">{row.id} · {t('audit.actor')} #{row.actor.user_id} · {row.actor.credential_id || t('audit.web')}</p>
      {row.parent_id && <p className="break-all text-sm">{t('audit.reversal')}: {row.parent_id}</p>}
      {row.relation && <p>{t('audit.relation', { source: row.relation.source_series_id, target: row.relation.target_series_id, type: t(`series.relations.type.${row.relation.relation_type}`) })}</p>}
      {row.reason && <p>{row.reason}</p>}
      {row.adjudication && <div className="space-y-1 border-l-2 border-blue-400 pl-3">
        <p className="font-medium">{t(`audit.proposal.${proposalStatusKey(row.adjudication)}`)}</p>
        <p className="text-sm text-gray-400">{t('audit.proposal')} #{row.adjudication.proposal_id}</p>
        <p className="text-sm">{row.adjudication.reason}</p>
      </div>}
      {!row.undo_unavailable && row.fields.map((field) => <div key={field.field} className="space-y-2 border-t border-gray-700 pt-3">
        <h4 className="font-medium">{t(`audit.field.${field.field}`)}</h4>
        {field.field === 'cover' && field.changed && <div className="flex gap-4">
          {coverImage(row.old_cover_material) && <img className="h-40 max-w-[45%] object-contain" src={coverImage(row.kind === 'undo' ? row.new_cover_material : row.old_cover_material)} alt={t('audit.cover.before')} />}
          {coverImage(row.new_cover_material) && <img className="h-40 max-w-[45%] object-contain" src={coverImage(row.kind === 'undo' ? row.old_cover_material : row.new_cover_material)} alt={t('audit.cover.after')} />}
        </div>}
        <div className="grid gap-3 text-sm md:grid-cols-2">
          <div><span className="text-gray-400">{t('audit.before')}</span><p className="whitespace-pre-wrap break-words">{fieldValue(row.before, field.field)}</p></div>
          <div><span className="text-gray-400">{t('audit.after')}</span><p className="whitespace-pre-wrap break-words">{fieldValue(row.after, field.field)}</p></div>
        </div>
        <p className="text-sm text-gray-400">{field.reason}</p>
        {field.formula && <p className="break-words text-sm">{field.formula}</p>}
        <ul className="space-y-1 text-sm">{field.sources.map((source, index) => <li key={`${source.source}-${index}`}>
          {safeLink(source.metadata.SourceURL) ? <a className="text-blue-300 underline" href={safeLink(source.metadata.SourceURL)} target="_blank" rel="noreferrer">{source.source}</a> : source.source}
          {source.metadata.SiteRating && <span> · {source.metadata.SiteRating.value} / {source.metadata.SiteRating.scale}</span>}
        </li>)}</ul>
      </div>)}
      {row.fields.some((field) => field.field === 'cover' && field.changed) && <p className="text-sm text-amber-200">{t('audit.cover.risk')}</p>}
      {row.undo_unavailable ? <p className="text-amber-200">{t('audit.cleanup.unavailable')}</p> : row.undone_by ? <p className="text-emerald-300">{t('audit.undone')}</p> : row.kind === 'enrich' && row.outcome === 'changed' && <button className={buttonClass} disabled={busy} onClick={() => setUndo({ operation: row, key: crypto.randomUUID() })}>{t('audit.undo')}</button>}
      {row.cleaned_by && cleanups.results[row.cleaned_by] && <p role="status" className="text-amber-200">{t(cleanups.results[row.cleaned_by].outcome === 'pending' ? 'audit.cleanup.pending' : cleanups.results[row.cleaned_by].outcome === 'partial' ? 'audit.cleanup.partial' : 'audit.cleanup.complete')}</p>}
      {row.cleaned_by && cleanups.results[row.cleaned_by]?.outcome === 'pending' && cleanups.results[row.cleaned_by]?.retry_request_key && cleanups.results[row.cleaned_by]?.operation_id === row.id && <button className={buttonClass} disabled={busy} onClick={() => void clean(row)}>{t('audit.cleanup.resume')}</button>}
      {!row.undo_unavailable && !row.cleaned_by && <button className={buttonClass} disabled={busy} onClick={() => void clean(row)}>{t('audit.cleanup.action')}</button>}
    </section>)}
    {loading && <p>{t('audit.loading')}</p>}
    {next && <button className={buttonClass} disabled={loading} onClick={() => void more()}>{t('audit.more')}</button>}
    <ConfirmDialog open={!!undo} title={t('audit.undo')} description={t(undo?.operation.fields.some((field) => field.field === 'cover' && field.changed) ? 'audit.cover.risk' : 'audit.confirmDescription')} confirmLabel={t('audit.confirm')} loading={busy} onClose={() => setUndo(null)} onConfirm={() => void restore()} />
  </div>;
}
