/**
 * 待人工页：所有需要所有者亲自看的系列——agent 的识别类上报、待复核的系列和非识别类备注，
 * 可按上报分类筛选、分页。每张卡自己处理决定，这一层只管取列表与计数。
 */

import { useCallback, useEffect, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { apiClient, getApiErrorMessage } from '../../api/client';
import type { AwaitingPage } from '../../api/generated';
import { useLatestRequest } from '../../hooks/useLatestRequest';
import { useRequestLifetime } from '../../session/useRequestLifetime';
import { useI18n } from '../../i18n/LocaleProvider';
import { PendingSeriesCard } from './PendingSeriesCard';
import { categoryLabel } from './labels';

const PAGE_SIZE = 20;

interface IdentificationPendingProps {
  // 某个系列的决定落下之后调用，让外层刷新计数。
  onChange?: () => void;
}

export default function IdentificationPending({ onChange }: IdentificationPendingProps) {
  const { t } = useI18n();
  const latest = useLatestRequest();
  const [category, setCategory] = useState('');
  const [offset, setOffset] = useState(0);
  const [page, setPage] = useState<AwaitingPage | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  // 筛选条上的计数取整张列表，不随所选分类变；每次决定落下之后重取。
  const [counts, setCounts] = useState<AwaitingPage | null>(null);
  const beginRequest = useRequestLifetime();

  const load = useCallback(() => {
    setLoading(true);
    const params = new URLSearchParams({ offset: String(offset), limit: String(PAGE_SIZE) });
    if (category) params.set('category', category);
    void latest.run(async () => (await apiClient.get<AwaitingPage>(`/api/identification/pending?${params}`)).data, {
      onSuccess: (data) => {
        setPage(data);
        setError(null);
      },
      onError: (err) => setError(getApiErrorMessage(err, t('identification.pending.loadFailed'))),
      onSettled: () => setLoading(false),
    });
  }, [category, offset, latest, t]);

  useEffect(() => {
    load();
  }, [load]);

  const refreshCounts = useCallback(() => {
    const alive = beginRequest();
    apiClient
      .get<AwaitingPage>('/api/identification/pending?limit=0')
      .then(({ data }) => {
        if (alive()) setCounts(data);
      })
      .catch(() => {
        // 取不到计数时保留上一次的。
      });
  }, [beginRequest]);

  useEffect(() => {
    refreshCounts();
  }, [refreshCounts]);

  const changed = useCallback(() => {
    refreshCounts();
    onChange?.();
  }, [refreshCounts, onChange]);

  const choose = (next: string) => {
    setCategory(next);
    setOffset(0);
  };

  const chip = (active: boolean) =>
    `inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-xs font-medium transition-colors ${
      active ? 'border-komgaPrimary/60 bg-komgaPrimary/15 text-white' : 'border-white/10 bg-white/5 text-gray-300 hover:bg-white/10'
    }`;

  return (
    <div className="space-y-4">
      <p className="text-sm text-gray-400">{t('identification.pending.intro')}</p>

      {counts && counts.categories.length > 0 && (
        <div className="flex flex-wrap gap-2">
          <button type="button" className={chip(category === '')} onClick={() => choose('')}>
            {t('identification.pending.filter.all')}
            <span className="text-gray-400">{counts.total}</span>
          </button>
          {counts.categories.map((c) => (
            <button key={c.category} type="button" className={chip(category === c.category)} onClick={() => choose(c.category)}>
              {categoryLabel(t, c.category, c.class)}
              <span className="text-gray-400">{c.series}</span>
            </button>
          ))}
        </div>
      )}

      {error && <p className="rounded-xl border border-red-500/20 bg-red-500/10 px-3 py-2 text-sm text-red-200">{error}</p>}

      {loading && !page ? (
        <div className="flex min-h-[30vh] items-center justify-center">
          <Loader2 className="h-8 w-8 animate-spin text-komgaPrimary" />
        </div>
      ) : page && page.items.length === 0 ? (
        <p className="py-16 text-center text-sm text-gray-500">{t(category ? 'identification.pending.emptyFiltered' : 'identification.pending.empty')}</p>
      ) : (
        page && (
          <>
            <div className="space-y-4">
              {page.items.map((item) => (
                <PendingSeriesCard key={`${item.series_id}:${offset}:${category}`} item={item} onChange={changed} />
              ))}
            </div>
            <div className="flex flex-wrap items-center justify-between gap-3 text-xs text-gray-400">
              <span>{t('identification.pending.range', { from: offset + 1, to: offset + page.items.length, total: page.total })}</span>
              <div className="flex gap-2">
                <button
                  type="button"
                  disabled={offset === 0 || loading}
                  onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
                  className="rounded-lg border border-white/10 bg-white/5 px-3 py-1.5 text-gray-200 hover:bg-white/10 disabled:opacity-40"
                >
                  {t('identification.pending.previous')}
                </button>
                <button
                  type="button"
                  disabled={!page.next_offset || loading}
                  onClick={() => page.next_offset && setOffset(page.next_offset)}
                  className="rounded-lg border border-white/10 bg-white/5 px-3 py-1.5 text-gray-200 hover:bg-white/10 disabled:opacity-40"
                >
                  {t('identification.pending.next')}
                </button>
              </div>
            </div>
          </>
        )
      )}
    </div>
  );
}
