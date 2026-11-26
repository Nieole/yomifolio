/**
 * 待人工页的一张系列卡：等人处理的上报与备注、证据包，以及所有者在这里能做的决定。
 * 每次决定都以服务端返回的识别为准重绘；被别处抢先处理（409）时重读该系列，照实画它现在的样子。
 */

import { useState } from 'react';
import { Link } from 'react-router-dom';
import { Ban, BookOpen, CheckCircle2, ExternalLink, Loader2, Search, ShieldCheck, X } from 'lucide-react';
import { apiClient, getApiErrorMessage, isAxiosError } from '../../api/client';
import type { Escalation, EvidenceCandidate, OwnerIdentified, PendingSeries, ResearchResult, SeriesIdentification } from '../../api/generated';
import { useI18n } from '../../i18n/LocaleProvider';
import { useToast } from '../../components/ToastProvider';
import { ConfirmDialog } from '../../components/ui/ConfirmDialog';
import { useRequestLifetime } from '../../session/useRequestLifetime';
import { sourceLabel } from '../../features/identification/sources';
import { categoryLabel, waitingEscalations } from './labels';

const CONFLICT_CODES = new Set(['not_pending', 'candidate_unavailable', 'not_a_note']);

function conflictCode(error: unknown) {
  if (!isAxiosError(error) || error.response?.status !== 409) return null;
  const code = (error.response.data as { code?: string } | undefined)?.code;
  return code && CONFLICT_CODES.has(code) ? code : 'not_pending';
}

interface PendingSeriesCardProps {
  item: PendingSeries;
  // 系列不再等人、或者被别处改过之后调用，让外层重取计数。
  onChange?: () => void;
}

export function PendingSeriesCard({ item, onChange }: PendingSeriesCardProps) {
  const { t, formatDateTime } = useI18n();
  const { showToast } = useToast();
  const beginRequest = useRequestLifetime();
  const [identification, setIdentification] = useState<SeriesIdentification>(item.identification);
  const [busy, setBusy] = useState<string | null>(null);
  // 每个来源至多选一个候选：键是来源，值是候选 ID。
  const [selected, setSelected] = useState<Record<string, string>>({});
  const [query, setQuery] = useState('');
  const [confirmingNotFound, setConfirmingNotFound] = useState(false);

  const seriesPath = `/api/identification/series/${item.series_id}`;
  const waiting = waitingEscalations(identification);
  const deciding = identification.status === 'awaiting_owner';
  const rechecking = identification.status === 'needs_recheck';
  // 复核上报记下的 ComicInfo 系列名：所有者认可识别之后，这些名字不再触发复核。
  const clues = [...new Set(waiting.filter((e) => e.class === 'recheck' && e.clue).map((e) => e.clue as string))];
  const evidence = identification.evidence;
  const candidates = evidence?.candidates ?? [];
  const selectedIds = candidates.filter((c) => selected[c.source] === c.id).map((c) => c.id);
  const searched = (evidence?.sources ?? []).filter((s) => s.status === 'available').map((s) => sourceLabel(s.source));

  const reread = async (alive: () => boolean) => {
    try {
      const r = await apiClient.get<SeriesIdentification>(seriesPath);
      if (alive()) setIdentification(r.data);
    } catch {
      // 重读失败就留着原样；提示已经说明了冲突。
    }
  };

  const decide = async (key: string, request: () => Promise<SeriesIdentification>, success: string) => {
    const alive = beginRequest();
    setBusy(key);
    try {
      const next = await request();
      if (!alive()) return;
      setIdentification(next);
      setSelected({});
      showToast(success, 'success');
      onChange?.();
    } catch (error) {
      if (!alive()) return;
      const code = conflictCode(error);
      if (code) {
        showToast(t(`identification.pending.conflict.${code}`), 'error');
        await reread(alive);
        onChange?.();
      } else {
        showToast(getApiErrorMessage(error, t('identification.pending.actionFailed')), 'error');
      }
    } finally {
      if (alive()) setBusy(null);
    }
  };

  const pick = (ids: string[]) =>
    decide('pick', async () => (await apiClient.post<OwnerIdentified>(`${seriesPath}/identify`, { candidate_ids: ids })).data.identification, t('identification.pending.picked'));
  const exclude = (id: string) =>
    decide(`exclude:${id}`, async () => (await apiClient.post<SeriesIdentification>(`${seriesPath}/exclusions`, { candidate_id: id })).data, t('identification.pending.excluded'));
  const concludeNotFound = () =>
    decide('not-found', async () => (await apiClient.post<SeriesIdentification>(`${seriesPath}/not-found`)).data, t('identification.pending.notFoundDone')).finally(() =>
      setConfirmingNotFound(false),
    );
  const setOwnerReviewed = () =>
    decide('owner-review', async () => (await apiClient.put<SeriesIdentification>(`${seriesPath}/owner-review`)).data, t('identification.pending.ownerReviewed'));
  const keepIdentification = () =>
    decide('keep', async () => (await apiClient.post<SeriesIdentification>(`${seriesPath}/keep-identification`)).data, t('identification.pending.kept'));
  const closeNote = (note: Escalation) =>
    decide(`note:${note.id}`, async () => (await apiClient.post<SeriesIdentification>(`${seriesPath}/notes/${note.id}/close`)).data, t('identification.pending.noteClosed'));

  const research = async () => {
    const term = query.trim();
    if (!term) return;
    const alive = beginRequest();
    setBusy('research');
    try {
      const { data } = await apiClient.post<ResearchResult>(`${seriesPath}/research`, { query: term });
      if (!alive()) return;
      switch (data.status) {
        case 'completed':
        case 'already_searched':
          await reread(alive);
          if (!alive()) return;
          setQuery('');
          showToast(t(data.status === 'completed' ? 'identification.pending.researchDone' : 'identification.pending.researchAlready'), 'success');
          break;
        case 'running':
          showToast(t('identification.pending.researchRunning'), 'success');
          break;
        case 'cancelled':
          showToast(t('identification.pending.researchCancelled'), 'error');
          break;
        default:
          showToast(t('identification.pending.researchFailed'), 'error');
      }
    } catch (error) {
      if (alive()) showToast(getApiErrorMessage(error, t('identification.pending.actionFailed')), 'error');
    } finally {
      if (alive()) setBusy(null);
    }
  };

  const toggle = (c: EvidenceCandidate) =>
    setSelected((prev) => {
      const next = { ...prev };
      if (next[c.source] === c.id) delete next[c.source];
      else next[c.source] = c.id;
      return next;
    });

  const raisedBy = (e: Escalation) => {
    const time = formatDateTime(e.raised_at);
    if (e.raised_by.kind === 'agent') {
      return t('identification.pending.raisedByAgent', { credential: e.raised_by.credential_name || e.raised_by.credential_id, time });
    }
    return t(e.raised_by.kind === 'system' ? 'identification.pending.raisedBySystem' : 'identification.pending.raisedByOwner', { time });
  };

  const status = t(`series.identification.status.${identification.status}`);
  const disabled = busy !== null;

  return (
    <article data-testid={`pending-series-${item.series_id}`} className="rounded-2xl border border-white/10 bg-gray-950/60 p-4 sm:p-5">
      <header className="flex gap-4">
        <div className="h-24 w-16 shrink-0 overflow-hidden rounded-lg border border-white/10 bg-black/40">
          {item.cover_book_id > 0 ? (
            <img src={`/api/covers/${item.cover_book_id}`} alt="" className="h-full w-full object-cover" loading="lazy" />
          ) : (
            <BookOpen className="m-5 h-6 w-6 text-gray-700" />
          )}
        </div>
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <Link to={`/series/${item.series_id}`} className="truncate text-base font-semibold text-white hover:text-komgaPrimary">
              {item.title}
            </Link>
            <span className="rounded-full border border-komgaPrimary/30 bg-komgaPrimary/10 px-2 py-0.5 text-xs font-semibold text-komgaPrimary">{status}</span>
          </div>
          {item.directory_name !== item.title && <p className="truncate text-xs text-gray-400">{t('identification.pending.directory', { name: item.directory_name })}</p>}
          <p className="text-xs text-gray-500">
            {item.library_name} · {t('common.books', { count: item.book_count })}
          </p>
        </div>
      </header>

      {waiting.length === 0 ? (
        <p className="mt-4 flex items-center gap-2 rounded-xl border border-emerald-500/20 bg-emerald-500/10 px-3 py-2 text-sm text-emerald-200">
          <CheckCircle2 className="h-4 w-4 shrink-0" />
          {t('identification.pending.settled', { status })}
        </p>
      ) : (
        <>
          <ul className="mt-4 space-y-2">
            {waiting.map((e) => (
              <li key={e.id} className="rounded-xl border border-white/10 bg-black/25 p-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <span className="rounded-lg border border-amber-400/20 bg-amber-400/10 px-2 py-0.5 text-xs font-semibold text-amber-200">
                    {categoryLabel(t, e.category, e.class)}
                  </span>
                  <span className="text-xs text-gray-500">{raisedBy(e)}</span>
                </div>
                {e.category === 'new_book_contradiction' ? (
                  <>
                    <p className="mt-2 text-sm text-gray-200">
                      {e.clue ? t('identification.pending.contradictionNamed', { name: e.clue }) : t('identification.pending.contradictionExplained')}
                    </p>
                    <p className="mt-1 break-words text-xs text-gray-500">{t('identification.pending.originalReason', { reason: e.reason })}</p>
                  </>
                ) : (
                  <p className="mt-2 break-words text-sm text-gray-200">{e.reason}</p>
                )}
                {e.class === 'note' && (
                  <div className="mt-2 flex flex-wrap items-center justify-between gap-2">
                    <p className="text-xs text-gray-500">{t('identification.pending.closeNoteHint')}</p>
                    <button
                      type="button"
                      onClick={() => closeNote(e)}
                      disabled={disabled}
                      className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/5 px-2.5 py-1.5 text-xs font-medium text-gray-200 hover:bg-white/10 disabled:opacity-50"
                    >
                      <X className="h-3.5 w-3.5" />
                      {t('identification.pending.closeNote')}
                    </button>
                  </div>
                )}
              </li>
            ))}
          </ul>

          {identification.status === 'needs_recheck' && (
            <section className="mt-4 space-y-2">
              <h4 className="text-sm font-semibold text-white">{t('identification.pending.currentConclusions')}</h4>
              {identification.conclusions.length === 0 ? (
                <p className="text-sm text-gray-500">{t('series.identification.noConclusions')}</p>
              ) : (
                identification.conclusions.map((c) => (
                  <div key={c.source} data-testid={`pending-conclusion-${c.source}`} className="flex flex-wrap items-center gap-2 rounded-xl border border-white/10 bg-black/25 px-3 py-2 text-sm">
                    <span className="rounded-lg border border-cyan-400/20 bg-cyan-400/10 px-2 py-0.5 text-xs font-semibold text-cyan-200">{sourceLabel(c.source)}</span>
                    {c.entry_url ? (
                      <a href={c.entry_url} target="_blank" rel="noreferrer" className="inline-flex min-w-0 items-center gap-1 text-cyan-300 hover:text-cyan-200">
                        <span className="truncate">{c.title || c.entry_id}</span>
                        <ExternalLink className="h-3.5 w-3.5 shrink-0" />
                      </a>
                    ) : (
                      <span className="text-gray-200">{c.title || c.entry_id}</span>
                    )}
                    {c.original_title && <span className="text-xs text-gray-400">{c.original_title}</span>}
                    <span className="text-xs text-gray-500">
                      {c.confirmer.kind === 'agent'
                        ? t('series.identification.confirmedByAgent', { credential: c.confirmer.credential_name || c.confirmer.credential_id, user: c.confirmer.user_name || String(c.confirmer.user_id) })
                        : t('series.identification.confirmedByOwner')}
                      {' · '}
                      {formatDateTime(c.confirmed_at)}
                    </span>
                  </div>
                ))
              )}
            </section>
          )}

          {rechecking && (
            <section className="mt-4 space-y-2 rounded-xl border border-komgaPrimary/20 bg-komgaPrimary/5 p-3">
              <p className="text-xs text-gray-300">
                {clues.length > 0
                  ? t('identification.pending.keepHint', { names: clues.map((c) => `「${c}」`).join('、') })
                  : t('identification.pending.keepHintNoName')}
              </p>
              <div className="flex flex-wrap items-center gap-3">
                <button
                  type="button"
                  onClick={keepIdentification}
                  disabled={disabled}
                  className="inline-flex items-center gap-1.5 rounded-xl bg-komgaPrimary px-3 py-2 text-sm font-semibold text-white hover:bg-komgaPrimaryHover disabled:opacity-50"
                >
                  {busy === 'keep' ? <Loader2 className="h-4 w-4 animate-spin" /> : <CheckCircle2 className="h-4 w-4" />}
                  {t('identification.pending.keep')}
                </button>
                <Link to={`/series/${item.series_id}`} className="text-sm text-cyan-300 hover:text-cyan-200">
                  {t('identification.pending.replaceOnSeries')}
                </Link>
              </div>
            </section>
          )}

          {(deciding || rechecking) && (
            <section className="mt-4 space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h4 className="text-sm font-semibold text-white">{t('identification.pending.evidence')}</h4>
                {evidence && <span className="text-xs text-gray-500">{t('identification.pending.fetchedAt', { time: formatDateTime(evidence.fetched_at) })}</span>}
              </div>
              {evidence ? (
                <>
                  <div className="rounded-xl border border-white/10 bg-black/20 p-3 text-xs text-gray-400">
                    <p className="mb-1 font-semibold text-gray-300">{t('identification.pending.localClues')}</p>
                    <p className="break-words text-gray-200">{evidence.local.directory_name}</p>
                    <p>{t('common.books', { count: evidence.local.book_count })}</p>
                    {evidence.local.file_names.length > 0 && <p className="break-words">{t('identification.pending.fileNames', { names: evidence.local.file_names.join('、') })}</p>}
                    {evidence.local.comic_info?.series && <p>{t('identification.pending.comicInfo', { series: evidence.local.comic_info.series })}</p>}
                    {evidence.local.comic_info?.authors?.length ? <p>{t('identification.pending.comicInfoAuthors', { authors: evidence.local.comic_info.authors.join('、') })}</p> : null}
                    {evidence.local.comic_info?.year ? <p>{t('identification.pending.comicInfoYear', { year: evidence.local.comic_info.year })}</p> : null}
                    <div className="mt-2 flex flex-wrap gap-1.5">
                      {evidence.sources
                        .filter((s) => s.status !== 'available')
                        .map((s) => (
                          <span key={s.source} className="rounded border border-red-400/20 bg-red-400/10 px-1.5 py-0.5 text-red-200">
                            {t('identification.pending.sourceUnavailable', { source: sourceLabel(s.source) })}
                          </span>
                        ))}
                    </div>
                  </div>
                  {candidates.length === 0 ? (
                    <p className="text-sm text-gray-500">{t('identification.pending.noCandidates')}</p>
                  ) : (
                    <ul className="space-y-2">
                      {candidates.map((c) => (
                        <CandidateRow
                          key={c.id}
                          candidate={c}
                          checked={deciding && selected[c.source] === c.id}
                          disabled={disabled}
                          onToggle={() => toggle(c)}
                          onPick={deciding ? () => pick([c.id]) : undefined}
                          onExclude={() => exclude(c.id)}
                        />
                      ))}
                    </ul>
                  )}
                </>
              ) : (
                <p className="text-sm text-gray-500">{t('identification.pending.noEvidence')}</p>
              )}

              <div className="flex flex-wrap items-center gap-2">
                {deciding && selectedIds.length > 0 && (
                  <button
                    type="button"
                    onClick={() => pick(selectedIds)}
                    disabled={disabled}
                    className="inline-flex items-center gap-1.5 rounded-xl bg-komgaPrimary px-3 py-2 text-sm font-semibold text-white hover:bg-komgaPrimaryHover disabled:opacity-50"
                  >
                    {busy === 'pick' ? <Loader2 className="h-4 w-4 animate-spin" /> : <CheckCircle2 className="h-4 w-4" />}
                    {t('identification.pending.pickSelected', { n: selectedIds.length })}
                  </button>
                )}
                {deciding && (
                  <button
                    type="button"
                    onClick={() => setConfirmingNotFound(true)}
                    disabled={disabled}
                    className="inline-flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/5 px-3 py-2 text-sm font-medium text-gray-200 hover:bg-white/10 disabled:opacity-50"
                  >
                    <Ban className="h-4 w-4" />
                    {t('identification.pending.notFound')}
                  </button>
                )}
                <button
                  type="button"
                  onClick={setOwnerReviewed}
                  disabled={disabled}
                  className="inline-flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/5 px-3 py-2 text-sm font-medium text-gray-200 hover:bg-white/10 disabled:opacity-50"
                >
                  <ShieldCheck className="h-4 w-4" />
                  {t('identification.pending.ownerReview')}
                </button>
              </div>

              <form
                className="flex flex-col gap-2 sm:flex-row"
                onSubmit={(event) => {
                  event.preventDefault();
                  void research();
                }}
              >
                <input
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  maxLength={100}
                  placeholder={t('identification.pending.researchPlaceholder')}
                  className="min-w-0 flex-1 rounded-xl border border-white/10 bg-black/30 px-3 py-2 text-sm text-white placeholder:text-gray-600 focus:border-komgaPrimary/60 focus:outline-none"
                />
                <button
                  type="submit"
                  disabled={disabled || query.trim() === ''}
                  className="inline-flex items-center justify-center gap-1.5 rounded-xl border border-white/10 bg-white/5 px-3 py-2 text-sm font-medium text-gray-200 hover:bg-white/10 disabled:opacity-50"
                >
                  {busy === 'research' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Search className="h-4 w-4" />}
                  {t('identification.pending.research')}
                </button>
              </form>
            </section>
          )}
        </>
      )}

      <ConfirmDialog
        open={confirmingNotFound}
        title={t('identification.pending.notFoundTitle')}
        description={t('identification.pending.notFoundDescription', { sources: searched.length > 0 ? searched.join('、') : t('identification.pending.noSources') })}
        tone="warning"
        loading={busy === 'not-found'}
        onClose={() => setConfirmingNotFound(false)}
        onConfirm={() => void concludeNotFound()}
      />
    </article>
  );
}

interface CandidateRowProps {
  candidate: EvidenceCandidate;
  checked: boolean;
  disabled: boolean;
  onToggle: () => void;
  // 不给时这一行只能否决：待复核的系列不在这里挑候选。
  onPick?: () => void;
  onExclude: () => void;
}

function CandidateRow({ candidate: c, checked, disabled, onToggle, onPick, onExclude }: CandidateRowProps) {
  const { t } = useI18n();
  const signal = (ok: boolean, yes: string, no: string) => (
    <span className={`rounded px-1.5 py-0.5 ${ok ? 'bg-emerald-500/10 text-emerald-200' : 'bg-red-500/10 text-red-200'}`}>{t(ok ? yes : no)}</span>
  );
  return (
    <li data-testid={`pending-candidate-${c.id}`} className={`rounded-xl border p-3 ${checked ? 'border-komgaPrimary/50 bg-komgaPrimary/5' : 'border-white/10 bg-black/25'}`}>
      <div className="flex gap-3">
        {onPick && (
          <input
            type="checkbox"
            checked={checked}
            onChange={onToggle}
            disabled={disabled}
            aria-label={t('identification.pending.select')}
            className="mt-1 h-4 w-4 shrink-0 accent-komgaPrimary"
          />
        )}
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="rounded-lg border border-cyan-400/20 bg-cyan-400/10 px-2 py-0.5 text-xs font-semibold text-cyan-200">{sourceLabel(c.source)}</span>
            <a href={c.entry_url} target="_blank" rel="noreferrer" className="inline-flex min-w-0 items-center gap-1 text-sm font-medium text-cyan-300 hover:text-cyan-200">
              <span className="truncate">{c.title}</span>
              <ExternalLink className="h-3.5 w-3.5 shrink-0" />
            </a>
          </div>
          {c.original_title && <p className="text-xs text-gray-400">{c.original_title}</p>}
          {(Boolean(c.authors?.length) || Boolean(c.year)) && (
            <p className="text-xs text-gray-400">{[c.authors?.join('、'), c.year ? String(c.year) : ''].filter(Boolean).join(' · ')}</p>
          )}
          {c.summary && <p className="text-xs text-gray-500">{c.summary}</p>}
          <div className="flex flex-wrap gap-1.5 text-[11px]">
            {signal(c.signals.title_matches, 'identification.pending.signal.titleMatches', 'identification.pending.signal.titleDiffers')}
            {c.signals.author_matches !== undefined &&
              signal(c.signals.author_matches, 'identification.pending.signal.authorMatches', 'identification.pending.signal.authorDiffers')}
            <span className="rounded bg-white/5 px-1.5 py-0.5 text-gray-300">
              {t('identification.pending.signal.volumes', { volumes: c.signals.source_volumes, books: c.signals.local_books })}
            </span>
          </div>
        </div>
        <div className="flex shrink-0 flex-col gap-1.5">
          {onPick && (
            <button
              type="button"
              onClick={onPick}
              disabled={disabled}
              className="rounded-lg bg-komgaPrimary px-2.5 py-1.5 text-xs font-semibold text-white hover:bg-komgaPrimaryHover disabled:opacity-50"
            >
              {t('identification.pending.pick')}
            </button>
          )}
          <button
            type="button"
            onClick={onExclude}
            disabled={disabled}
            className="rounded-lg border border-red-500/30 bg-red-500/10 px-2.5 py-1.5 text-xs font-medium text-red-200 hover:bg-red-500/20 disabled:opacity-50"
          >
            {t('identification.pending.exclude')}
          </button>
        </div>
      </div>
    </li>
  );
}
