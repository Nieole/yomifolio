/**
 * 系列详情侧栏的识别区：展示识别状态、各来源的识别结论与排除候选，让管理员设为或撤回已审定，
 * 并从某个来源的手动搜索撤换它的识别。每次操作都以服务端返回的识别为准重绘，不按请求意图乐观改写。
 */

import { useEffect, useRef, useState } from 'react';
import { BadgeCheck, ExternalLink, Loader2, Replace, ShieldCheck, Undo2 } from 'lucide-react';
import { apiClient, getApiErrorMessage } from '../../api/client';
import type { Conclusion, SeriesIdentification } from '../../api/generated';
import { useI18n } from '../../i18n/LocaleProvider';
import { useToast } from '../../components/ToastProvider';

const SOURCE_LABELS: Record<string, string> = {
  bangumi: 'Bangumi',
  anilist: 'AniList',
  mangadex: 'MangaDex',
  myanimelist: 'MyAnimeList',
  comicvine: 'Comic Vine',
};

function sourceLabel(source: string) {
  return SOURCE_LABELS[source] ?? source;
}

interface SeriesIdentificationPanelProps {
  seriesId: number;
  /** 撤换某个来源的识别：打开该来源的手动搜索，挑中的条目取代现有识别。不给就不提供撤换。 */
  onReplace?: (source: string) => void;
  /** 当前能手动搜索的来源；不在其中的来源没有可撤换的入口。 */
  replaceableSources?: string[];
  /** 页面别处记下人工识别后递增，识别区随之重读。 */
  revision?: number;
}

export function SeriesIdentificationPanel({ seriesId, onReplace, replaceableSources = [], revision = 0 }: SeriesIdentificationPanelProps) {
  const { t, formatDateTime } = useI18n();
  const { showToast } = useToast();
  const [identification, setIdentification] = useState<SeriesIdentification | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  // 切换系列后，上一个系列迟到的响应不得画到新系列上。
  const current = useRef(seriesId);
  current.current = seriesId;

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setIdentification(null);
    apiClient
      .get<SeriesIdentification>(`/api/identification/series/${seriesId}`)
      .then((r) => {
        if (!cancelled) setIdentification(r.data);
      })
      .catch((err) => {
        if (!cancelled) showToast(getApiErrorMessage(err, t('series.identification.loadFailed')), 'error');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [seriesId, revision, t, showToast]);

  const decide = async (reviewed: boolean) => {
    const target = seriesId;
    const path = `/api/identification/series/${target}/owner-review`;
    setSaving(true);
    try {
      const r = reviewed
        ? await apiClient.put<SeriesIdentification>(path)
        : await apiClient.delete<SeriesIdentification>(path);
      if (current.current !== target) return;
      setIdentification(r.data);
      showToast(t(reviewed ? 'series.identification.ownerReviewSet' : 'series.identification.ownerReviewWithdrawn'), 'success');
    } catch (err) {
      showToast(getApiErrorMessage(err, t('series.identification.decisionFailed')), 'error');
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex justify-center py-10">
        <Loader2 className="h-5 w-5 animate-spin text-komgaPrimary" />
      </div>
    );
  }
  if (!identification) {
    return <div className="py-10 text-center text-sm text-gray-500">{t('series.identification.unavailable')}</div>;
  }

  const confirmer = (c: Conclusion) => {
    if (c.confirmer.kind === 'agent') {
      return t('series.identification.confirmedByAgent', {
        credential: c.confirmer.credential_name || c.confirmer.credential_id,
        user: c.confirmer.user_name || String(c.confirmer.user_id),
      });
    }
    return c.confirmer.user_name
      ? t('series.identification.confirmedByOwnerNamed', { user: c.confirmer.user_name })
      : t('series.identification.confirmedByOwner');
  };
  const review = identification.owner_review;

  return (
    <div className="space-y-6">
      <section className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2 text-sm text-gray-300">
            <span>{t('series.identification.statusLabel')}</span>
            <span
              data-testid="identification-status"
              className="rounded-full border border-komgaPrimary/30 bg-komgaPrimary/10 px-2.5 py-1 text-xs font-semibold text-komgaPrimary"
            >
              {t(`series.identification.status.${identification.status}`)}
            </span>
          </div>
          {review ? (
            <button
              type="button"
              onClick={() => decide(false)}
              disabled={saving}
              className="inline-flex items-center gap-2 rounded-xl border border-white/10 bg-white/5 px-3 py-2 text-sm font-medium text-gray-200 transition-colors hover:bg-white/10 disabled:opacity-50"
            >
              <Undo2 className="h-4 w-4" />
              {t('series.identification.withdrawOwnerReview')}
            </button>
          ) : (
            <button
              type="button"
              onClick={() => decide(true)}
              disabled={saving}
              className="inline-flex items-center gap-2 rounded-xl bg-komgaPrimary px-3 py-2 text-sm font-semibold text-white transition-colors hover:bg-komgaPrimaryHover disabled:opacity-50"
            >
              <ShieldCheck className="h-4 w-4" />
              {t('series.identification.setOwnerReview')}
            </button>
          )}
        </div>
        <p className="text-xs text-gray-500">
          {review
            ? t('series.identification.ownerReviewedBy', {
                user: review.user_name || String(review.user_id),
                time: formatDateTime(review.reviewed_at),
              })
            : t('series.identification.ownerReviewHint')}
        </p>
      </section>

      <section className="space-y-3">
        <h4 className="text-sm font-semibold text-white">{t('series.identification.conclusions')}</h4>
        {identification.conclusions.length === 0 ? (
          <p className="text-sm text-gray-500">{t('series.identification.noConclusions')}</p>
        ) : (
          identification.conclusions.map((c) => (
            <div key={c.source} className="rounded-xl border border-white/10 bg-black/25 p-3">
              <div className="flex flex-wrap items-center gap-2">
                <span className="inline-flex items-center gap-1 rounded-lg border border-cyan-400/20 bg-cyan-400/10 px-2 py-0.5 text-xs font-semibold text-cyan-200">
                  <BadgeCheck className="h-3.5 w-3.5" />
                  {sourceLabel(c.source)}
                </span>
                {c.entry_url ? (
                  <a
                    href={c.entry_url}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex min-w-0 items-center gap-1 truncate text-sm text-cyan-300 hover:text-cyan-200"
                  >
                    <span className="truncate">{c.title || c.entry_id}</span>
                    <ExternalLink className="h-3.5 w-3.5 shrink-0" />
                  </a>
                ) : (
                  <span className="truncate text-sm text-gray-200">{c.title || c.entry_id}</span>
                )}
              </div>
              {c.original_title && <p className="mt-1 text-xs text-gray-400">{c.original_title}</p>}
              <p className="mt-2 text-xs text-gray-400">
                {confirmer(c)} · {formatDateTime(c.confirmed_at)}
              </p>
              {c.reason ? (
                <p className="mt-1 text-xs text-gray-300">{c.reason}</p>
              ) : c.proposal_id > 0 ? (
                <p className="mt-1 text-xs text-gray-500">
                  {t('series.identification.fromAppliedProposal', { id: c.proposal_id })}
                </p>
              ) : (
                c.confirmer.kind === 'owner' && <p className="mt-1 text-xs text-gray-500">{t('series.identification.ownerPicked')}</p>
              )}
              {onReplace && (
                <div className="mt-3 flex flex-wrap items-center gap-2">
                  <button
                    type="button"
                    onClick={() => onReplace(c.source)}
                    disabled={!replaceableSources.includes(c.source)}
                    title={replaceableSources.includes(c.source) ? t('series.identification.replaceHint') : t('series.identification.replaceUnavailable')}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/5 px-2.5 py-1 text-xs font-medium text-gray-200 transition-colors hover:bg-white/10 disabled:cursor-not-allowed disabled:opacity-40"
                  >
                    <Replace className="h-3.5 w-3.5" />
                    {t('series.identification.replace')}
                  </button>
                </div>
              )}
            </div>
          ))
        )}
      </section>

      {identification.excluded_candidates.length > 0 && (
        <section className="space-y-2">
          <h4 className="text-sm font-semibold text-white">{t('series.identification.excluded')}</h4>
          {identification.excluded_candidates.map((x) => (
            <div key={`${x.source}:${x.entry_id}`} className="flex flex-wrap items-center gap-2 text-xs text-gray-400">
              <span className="rounded border border-white/10 px-1.5 py-0.5 text-gray-300">{sourceLabel(x.source)}</span>
              {x.entry_url ? (
                <a href={x.entry_url} target="_blank" rel="noreferrer" className="text-gray-300 hover:text-white">
                  {x.title || x.entry_id}
                </a>
              ) : (
                <span className="text-gray-300">{x.title || x.entry_id}</span>
              )}
              <span>{formatDateTime(x.excluded_at)}</span>
            </div>
          ))}
        </section>
      )}
    </div>
  );
}
