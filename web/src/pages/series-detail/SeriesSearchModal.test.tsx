/**
 * @vitest-environment jsdom
 *
 * 守手动搜索弹窗在目录来源下的识别口径：同来源已有识别时说明挑别的条目是撤换，排除候选挑不进去，
 * 预览按识别策略只把空字段与目录名标题标成「将补充」；LLM 来源仍是提案口径。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';

import type { SeriesIdentification } from '../../api/generated';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages as zhCN } from '../../i18n/locales/zh-CN';
import { SeriesSearchModal } from './SeriesSearchModal';
import type { SearchResult, Series } from './types';

const SERIES: Series = {
  id: 5, name: 'Title 1', library_id: 1, path: '/lib/Title 1', book_count: 1,
  title: { String: 'Title 1', Valid: true }, summary: { String: 'Owner summary', Valid: true }, locked_fields: { String: 'title', Valid: true },
};

function result(id: number, title: string): SearchResult {
  return {
    Title: title, OriginalTitle: '', Summary: `${title} summary`, Publisher: 'Shogakukan', CoverURL: '', Rating: 0, Tags: ['剧情'],
    SourceID: id, SourceURL: `https://bgm.tv/subject/${id}`, ReleaseDate: '', VolumeCount: 0,
  };
}

const IDENTIFICATION: SeriesIdentification = {
  series_id: 5, library_id: 1, status: 'identified',
  conclusions: [{
    source: 'bangumi', entry_id: '1', entry_url: 'https://bgm.tv/subject/1', title: '错的作品', original_title: '', aliases: [],
    confirmer: { kind: 'agent', user_id: 1, user_name: 'admin', credential_id: 'c', credential_name: 'agent' },
    confirmed_at: '2026-03-01T09:00:00Z', reason: 'titles match', proposal_id: 0,
  }],
  excluded_candidates: [{ source: 'bangumi', entry_id: '9', entry_url: 'https://bgm.tv/subject/9', title: '否决过的作品', user_id: 1, excluded_at: '2026-03-01T09:00:00Z', proposal_id: 0 }],
};

function renderModal(provider: string, selected: SearchResult, onApply = vi.fn()) {
  render(
    <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
      <SeriesSearchModal
        open
        provider={provider}
        providerLabel={provider === 'llm' ? 'AI/LLM' : 'Bangumi'}
        identification={IDENTIFICATION}
        modalSearchQuery="Monster"
        isScraping={false}
        searchResults={[result(7, '怪物'), result(1, '错的作品'), result(9, '否决过的作品')]}
        currentOffset={0}
        searchTotal={3}
        currentSeries={SERIES}
        currentTags={[]}
        lockedFields={new Set(['title'])}
        selectedResult={selected}
        onClose={vi.fn()}
        onSearchQueryChange={vi.fn()}
        onReSearch={vi.fn()}
        onSelectMetadata={vi.fn()}
        onApplyMetadata={onApply}
      />
    </LocaleProvider>,
  );
  return onApply;
}

afterEach(() => {
  cleanup();
});

describe('SeriesSearchModal 识别口径', () => {
  it('tells a pick of another entry replaces the identification and previews only the fill', () => {
    const picked = result(7, '怪物');
    const onApply = renderModal('bangumi', picked);

    expect(screen.getByTestId('search-replacing').textContent).toContain('「错的作品」');
    expect(screen.getByText('当前识别')).toBeTruthy();
    // The directory-name title is a placeholder even under its lock; the owner's summary stays.
    expect(screen.getByTestId('preview-title').getAttribute('data-outcome')).toBe('fill');
    expect(screen.getByTestId('preview-summary').getAttribute('data-outcome')).toBe('keep');
    expect(screen.getByTestId('preview-publisher').getAttribute('data-outcome')).toBe('fill');
    expect(screen.getByTestId('preview-tags').getAttribute('data-outcome')).toBe('policy');

    fireEvent.click(screen.getByRole('button', { name: '撤换为此条目' }));
    expect(onApply).toHaveBeenCalledWith(picked);
  });

  it('marks an excluded entry and keeps it from being picked', () => {
    const onApply = renderModal('bangumi', result(9, '否决过的作品'));

    expect(screen.getAllByText('已排除')).toHaveLength(1);
    expect(screen.getByText('这个条目是该系列的排除候选，不能识别为该系列')).toBeTruthy();
    const apply = screen.getByRole('button', { name: '撤换为此条目' });
    expect(apply).toHaveProperty('disabled', true);
    fireEvent.click(apply);
    expect(onApply).not.toHaveBeenCalled();
  });

  it('keeps the proposal wording for a model answer', () => {
    renderModal('llm', result(7, '怪物'));

    expect(screen.queryByTestId('search-replacing')).toBeNull();
    expect(screen.getByRole('button', { name: '加入审核队列' })).toBeTruthy();
    expect(screen.getByTestId('preview-title').getAttribute('data-outcome')).toBe('locked');
  });
});
