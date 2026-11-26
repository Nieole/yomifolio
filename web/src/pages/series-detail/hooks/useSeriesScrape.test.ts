/**
 * @vitest-environment jsdom
 *
 * 守详情页手动搜索的「挑中即识别」：目录来源的条目挑中后交给识别接口、按服务端结果提示并让识别区重读，
 * 不进审核队列；破了的话挑中的条目只变成一条待审提案，系列仍是未处理。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';

import { apiClient } from '../../../api/client';
import type { OwnerIdentification, SeriesIdentification } from '../../../api/generated';
import type { SearchResult, Series } from '../types';
import { useSeriesScrape } from './useSeriesScrape';

const SERIES: Series = { id: 5, name: 'Title 1', library_id: 1, path: '/lib/Title 1', book_count: 1, locked_fields: { String: '', Valid: false } };

const RESULT: SearchResult = {
  Title: '怪物', OriginalTitle: '', Summary: '', Publisher: '', CoverURL: '', Rating: 0, Tags: [], SourceID: 7,
  SourceURL: 'https://bgm.tv/subject/7', ReleaseDate: '', VolumeCount: 0,
};

const IDENTIFIED: SeriesIdentification = {
  series_id: 5, library_id: 1, status: 'identified', excluded_candidates: [],
  conclusions: [{
    source: 'bangumi', entry_id: '1', entry_url: 'https://bgm.tv/subject/1', title: '错的作品', original_title: '', aliases: [],
    confirmer: { kind: 'agent', user_id: 1, user_name: 'admin', credential_id: 'c', credential_name: 'agent' },
    confirmed_at: '2026-03-01T09:00:00Z', reason: 'titles match', proposal_id: 0,
  }],
};

function setup() {
  const reload = vi.fn().mockResolvedValue(undefined);
  const showToast = vi.fn();
  const t = (key: string, params?: Record<string, unknown>) => (params ? `${key} ${JSON.stringify(params)}` : key);
  const view = renderHook(() => useSeriesScrape({ seriesId: '5', series: SERIES, reload, showToast, t }));
  return { ...view, reload, showToast };
}

function mockGet(identification: SeriesIdentification | null) {
  return vi.spyOn(apiClient, 'get').mockImplementation(((url: string) => {
    if (url === '/api/identification/series/5') return identification ? Promise.resolve({ data: identification }) : Promise.reject(new Error('forbidden'));
    return Promise.resolve({ data: { results: [RESULT], total: 1 } });
  }) as never);
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('useSeriesScrape', () => {
  it('records the pick as the owner identification and has the identification reread', async () => {
    mockGet(IDENTIFIED);
    const answer: OwnerIdentification = { identification: { ...IDENTIFIED, conclusions: [] }, replaced: IDENTIFIED.conclusions[0], retraction_id: 'r', operation_id: 'o' };
    const put = vi.spyOn(apiClient, 'put').mockResolvedValue({ data: answer } as never);
    const post = vi.spyOn(apiClient, 'post');
    const { result, reload, showToast } = setup();

    await act(async () => {
      await result.current.handleScrape('bangumi');
    });
    expect(result.current.searchIdentification?.conclusions[0].entry_id).toBe('1');
    const before = result.current.identificationRevision;
    await act(async () => {
      await result.current.handleApplyMetadata(RESULT);
    });

    expect(put).toHaveBeenCalledWith('/api/identification/series/5/conclusions/bangumi', { result: RESULT });
    expect(post).not.toHaveBeenCalled();
    expect(showToast).toHaveBeenCalledWith('identification.pick.replaced {"title":"错的作品"}', 'success');
    expect(reload).toHaveBeenCalled();
    expect(result.current.identificationRevision).toBe(before + 1);
  });

  it('reports an excluded entry and leaves the identification as it was', async () => {
    mockGet(null);
    vi.spyOn(apiClient, 'put').mockRejectedValue(Object.assign(new Error('conflict'), { isAxiosError: true, response: { status: 409 } }));
    const { result, reload, showToast } = setup();

    await act(async () => {
      await result.current.handleScrape('anilist');
    });
    expect(result.current.searchIdentification).toBeNull();
    const before = result.current.identificationRevision;
    await act(async () => {
      await result.current.handleApplyMetadata(RESULT);
    });

    expect(showToast).toHaveBeenCalledWith('identification.pick.excluded', 'error');
    expect(reload).not.toHaveBeenCalled();
    expect(result.current.identificationRevision).toBe(before);
  });
});
