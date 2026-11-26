import { useCallback } from 'react';
import { apiClient } from '../../api/client';
import type { PrefetchLaunch } from '../../api/generated';
import { useI18n } from '../../i18n/LocaleProvider';
import { useToast } from '../../components/ToastProvider';
import { useRequestLifetime } from '../../session/useRequestLifetime';

/**
 * 启动证据包预取并用一条提示报告结果。给了资料库只预取它，不给就覆盖全部资料库；
 * 没有待预取的系列时服务端不建运行，提示也照实说。
 */
export function useEvidencePrefetch() {
  const { t } = useI18n();
  const { showToast } = useToast();
  const beginRequest = useRequestLifetime();
  return useCallback(async (libraryId?: number) => {
    const alive = beginRequest();
    try {
      const res = await apiClient.post<PrefetchLaunch>('/api/identification/prefetch', libraryId === undefined ? {} : { library_id: libraryId });
      if (!alive()) return;
      showToast(res.data.series > 0 ? t('identification.prefetch.started', { series: res.data.series }) : t('identification.prefetch.nothingPending'), 'success');
    } catch (error) {
      if (!alive()) return;
      console.error(error);
      showToast(t('identification.prefetch.failed'), 'error');
    }
  }, [beginRequest, showToast, t]);
}
