/**
 * @vitest-environment jsdom
 *
 * 守系列详情识别区的两件事：识别结论按来源如实列出（条目链接、确认者、确认时间），
 * 设为与撤回已审定之后画的是服务端返回的识别，失败时保留原状而不是按意图改写。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';

import { apiClient } from '../../api/client';
import type { SeriesIdentification } from '../../api/generated';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages as zhCN } from '../../i18n/locales/zh-CN';
import { ToastProvider } from '../../components/ToastProvider';
import { SeriesIdentificationPanel } from './SeriesIdentificationPanel';

const IDENTIFIED: SeriesIdentification = {
  series_id: 7,
  library_id: 1,
  status: 'identified',
  conclusions: [
    {
      source: 'bangumi',
      entry_id: '42',
      entry_url: 'https://bgm.tv/subject/42',
      title: '中文名',
      original_title: '原名',
      aliases: [],
      confirmer: { kind: 'owner', user_id: 0, user_name: '', credential_id: '', credential_name: '' },
      confirmed_at: '2026-01-02T03:04:05Z',
      reason: '',
      proposal_id: 12,
    },
    {
      source: 'anilist',
      entry_id: '9',
      entry_url: 'https://anilist.co/manga/9',
      title: 'English title',
      original_title: '',
      aliases: [],
      confirmer: { kind: 'agent', user_id: 1, user_name: 'admin', credential_id: 'abc', credential_name: '整理 agent' },
      confirmed_at: '2026-02-03T04:05:06Z',
      reason: '标题与作者一致',
      proposal_id: 0,
    },
  ],
  excluded_candidates: [
    {
      source: 'mangadex',
      entry_id: 'uuid-1',
      entry_url: 'https://mangadex.org/title/uuid-1',
      title: '错的作品',
      user_id: 0,
      excluded_at: '2026-01-01T00:00:00Z',
      proposal_id: 3,
    },
  ],
};

const REVIEWED: SeriesIdentification = {
  ...IDENTIFIED,
  status: 'owner_reviewed',
  owner_review: { user_id: 1, user_name: 'admin', reviewed_at: '2026-03-01T00:00:00Z' },
};

function renderPanel(props: Partial<Parameters<typeof SeriesIdentificationPanel>[0]> = {}) {
  const ui = (extra: Partial<Parameters<typeof SeriesIdentificationPanel>[0]>) => (
    <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
      <ToastProvider>
        <SeriesIdentificationPanel seriesId={7} {...props} {...extra} />
      </ToastProvider>
    </LocaleProvider>
  );
  const view = render(ui({}));
  return { rerender: (extra: Partial<Parameters<typeof SeriesIdentificationPanel>[0]>) => view.rerender(ui(extra)) };
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('SeriesIdentificationPanel', () => {
  it('lists each source conclusion with its entry link, confirmer and the excluded candidates', async () => {
    const get = vi.spyOn(apiClient, 'get').mockResolvedValue({ data: IDENTIFIED } as never);
    renderPanel();

    expect(await screen.findByTestId('identification-status')).toHaveProperty('textContent', '已识别');
    expect(get).toHaveBeenCalledWith('/api/identification/series/7');
    expect(screen.getByRole('link', { name: /中文名/ }).getAttribute('href')).toBe('https://bgm.tv/subject/42');
    expect(screen.getByText('原名')).toBeTruthy();
    expect(screen.getByText(/人工确认/)).toBeTruthy();
    expect(screen.getByText('来自已应用的提案 #12')).toBeTruthy();
    expect(screen.getByRole('link', { name: /English title/ }).getAttribute('href')).toBe('https://anilist.co/manga/9');
    expect(screen.getByText(/agent 确认（凭据「整理 agent」，归属 admin）/)).toBeTruthy();
    expect(screen.getByText('标题与作者一致')).toBeTruthy();
    expect(screen.getByRole('link', { name: '错的作品' }).getAttribute('href')).toBe('https://mangadex.org/title/uuid-1');
  });

  it('sets and withdraws owner-reviewed from the server answer', async () => {
    vi.spyOn(apiClient, 'get').mockResolvedValue({ data: IDENTIFIED } as never);
    const put = vi.spyOn(apiClient, 'put').mockResolvedValue({ data: REVIEWED } as never);
    const del = vi.spyOn(apiClient, 'delete').mockResolvedValue({ data: IDENTIFIED } as never);
    renderPanel();

    fireEvent.click(await screen.findByRole('button', { name: '设为已审定' }));
    expect(await screen.findByRole('button', { name: '撤回已审定' })).toBeTruthy();
    expect(put).toHaveBeenCalledWith('/api/identification/series/7/owner-review');
    expect(screen.getByTestId('identification-status').textContent).toBe('已审定');
    expect(screen.getByText(/admin 于 .+ 设为已审定/)).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '撤回已审定' }));
    expect(await screen.findByRole('button', { name: '设为已审定' })).toBeTruthy();
    expect(del).toHaveBeenCalledWith('/api/identification/series/7/owner-review');
    expect(screen.getByTestId('identification-status').textContent).toBe('已识别');
  });

  it('keeps the shown state when the decision is refused', async () => {
    vi.spyOn(apiClient, 'get').mockResolvedValue({ data: IDENTIFIED } as never);
    vi.spyOn(apiClient, 'put').mockRejectedValue(new Error('需要管理员权限'));
    renderPanel();

    fireEvent.click(await screen.findByRole('button', { name: '设为已审定' }));
    expect(await screen.findByText('需要管理员权限')).toBeTruthy();
    expect(screen.getByTestId('identification-status').textContent).toBe('已识别');
    expect(within(document.body).getByRole('button', { name: '设为已审定' })).toBeTruthy();
  });

  it('replaces a source identification through the manual search of that source', async () => {
    vi.spyOn(apiClient, 'get').mockResolvedValue({ data: IDENTIFIED } as never);
    const onReplace = vi.fn();
    renderPanel({ onReplace, replaceableSources: ['bangumi'] });

    const [bangumi, anilist] = await screen.findAllByRole('button', { name: '撤换' });
    fireEvent.click(bangumi);
    expect(onReplace).toHaveBeenCalledWith('bangumi');
    // A source that is not enabled has no manual search to replace from.
    expect(anilist).toHaveProperty('disabled', true);
    fireEvent.click(anilist);
    expect(onReplace).toHaveBeenCalledTimes(1);
  });

  it('rereads the identification after an owner pick elsewhere on the page', async () => {
    const picked: SeriesIdentification = {
      ...IDENTIFIED,
      conclusions: [{ ...IDENTIFIED.conclusions[0], entry_id: '43', title: '撤换后的作品', proposal_id: 0, confirmer: { kind: 'owner', user_id: 1, user_name: 'admin', credential_id: '', credential_name: '' } }],
    };
    const get = vi.spyOn(apiClient, 'get').mockResolvedValueOnce({ data: IDENTIFIED } as never).mockResolvedValueOnce({ data: picked } as never);
    const view = renderPanel({ revision: 0 });
    expect(await screen.findByRole('link', { name: /中文名/ })).toBeTruthy();

    view.rerender({ revision: 1 });
    expect(await screen.findByRole('link', { name: /撤换后的作品/ })).toBeTruthy();
    expect(get).toHaveBeenCalledTimes(2);
    expect(screen.getByText('在手动搜索中挑中')).toBeTruthy();
  });

  it('withdraws a source identification after a confirmation and draws the server answer', async () => {
    vi.spyOn(apiClient, 'get').mockResolvedValue({ data: IDENTIFIED } as never);
    const withdrawn: SeriesIdentification = {
      ...IDENTIFIED,
      conclusions: [IDENTIFIED.conclusions[1]],
      excluded_candidates: [
        ...IDENTIFIED.excluded_candidates,
        { source: 'bangumi', entry_id: '42', entry_url: 'https://bgm.tv/subject/42', title: '中文名', user_id: 1, excluded_at: '2026-03-01T00:00:00Z', proposal_id: 0 },
      ],
    };
    const del = vi.spyOn(apiClient, 'delete').mockResolvedValue({ data: { identification: withdrawn, withdrawn: IDENTIFIED.conclusions[0], retraction_id: 'r' } } as never);
    const onChanged = vi.fn();
    renderPanel({ onChanged });

    const [bangumi] = await screen.findAllByRole('button', { name: '撤回识别' });
    fireEvent.click(bangumi);
    expect(del).not.toHaveBeenCalled();
    expect(screen.getByText(/「中文名」会记为排除候选/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '确认撤回' }));

    expect(await screen.findByText('已撤回识别，原条目已记为排除候选')).toBeTruthy();
    expect(del).toHaveBeenCalledWith('/api/identification/series/7/conclusions/bangumi');
    expect(screen.getAllByRole('button', { name: '撤回识别' })).toHaveLength(1);
    expect(onChanged).toHaveBeenCalled();
  });

  it('keeps the identification when the withdrawal is refused', async () => {
    vi.spyOn(apiClient, 'get').mockResolvedValue({ data: IDENTIFIED } as never);
    vi.spyOn(apiClient, 'delete').mockRejectedValue(new Error('需要管理员权限'));
    const onChanged = vi.fn();
    renderPanel({ onChanged });

    fireEvent.click((await screen.findAllByRole('button', { name: '撤回识别' }))[0]);
    fireEvent.click(screen.getByRole('button', { name: '确认撤回' }));

    expect(await screen.findByText('需要管理员权限')).toBeTruthy();
    expect(screen.getAllByRole('button', { name: '撤回识别' })).toHaveLength(2);
    expect(onChanged).not.toHaveBeenCalled();
  });
});
