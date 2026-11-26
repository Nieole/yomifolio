/**
 * 手动搜索弹窗里「挑中即识别」的共用逻辑：把挑中的结果交给服务端（哪些来源能记为识别见 sources.ts）、
 * 按服务端的权威结果组出提示，以及判断一条搜索结果是不是某条识别结论或排除候选的条目。
 * 详情页与资料库网格各有一份弹窗状态，这里是两边唯一的口径。
 */

import { apiClient } from '../../api/client';
import type { Conclusion, ExcludedCandidate, OwnerIdentification, SeriesIdentification } from '../../api/generated';

/** 读系列的识别，供弹窗提示撤换与标出排除候选；读不到时弹窗照常可用，由服务端把关。 */
export async function loadIdentification(seriesId: number | string): Promise<SeriesIdentification | null> {
  try {
    const res = await apiClient.get<SeriesIdentification>(`/api/identification/series/${seriesId}`);
    return res.data;
  } catch {
    return null;
  }
}

/** 把挑中的搜索结果记为所有者对该来源条目的识别。 */
export async function identifyAsOwner(seriesId: number | string, source: string, result: unknown): Promise<OwnerIdentification> {
  const res = await apiClient.put<OwnerIdentification>(`/api/identification/series/${seriesId}/conclusions/${source}`, { result });
  return res.data;
}

/** 按服务端结果选提示：撤换、仅记下识别（已审定的系列不补充）、解除了排除，或记下并补充。 */
export function ownerPickMessage(out: OwnerIdentification): { key: string; params?: Record<string, unknown> } {
  if (out.replaced) return { key: 'identification.pick.replaced', params: { title: out.replaced.title || out.replaced.entry_id } };
  if (!out.operation_id) return { key: 'identification.pick.recordedOnly' };
  if (out.lifted_exclusion) return { key: 'identification.pick.liftedExclusion' };
  return { key: 'identification.pick.identified' };
}

interface PickedEntry {
  SourceID: number;
  SourceURL?: string;
}

/** 搜索结果指向的是不是这条识别结论或排除候选的条目：条目编号或条目链接相同即是。 */
export function isSameEntry(result: PickedEntry, entry: Pick<Conclusion | ExcludedCandidate, 'entry_id' | 'entry_url'>): boolean {
  if (result.SourceID > 0 && String(result.SourceID) === entry.entry_id) return true;
  return Boolean(result.SourceURL && entry.entry_url && result.SourceURL.trim() === entry.entry_url);
}
