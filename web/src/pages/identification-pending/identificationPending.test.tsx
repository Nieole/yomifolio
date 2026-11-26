/**
 * @vitest-environment jsdom
 *
 * 守待人工页：列出上报、证据包与 agent 的理由并可按分类筛选；挑候选、否决、判查无此作、重搜、
 * 设为已审定与关闭备注都发到对应的管理员接口，界面画的是服务端返回的识别；被别处抢先处理（409）时
 * 重读该系列并照实呈现，而不是按请求意图改写。
 */

import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosRequestConfig } from 'axios';

import { apiClient } from '../../api/client';
import type { EvidencePacket, AwaitingPage, AwaitingSeries, SeriesIdentification } from '../../api/generated';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages as zhCN } from '../../i18n/locales/zh-CN';
import { ToastProvider } from '../../components/ToastProvider';
import IdentificationPending from './index';

const agent = { kind: 'agent', user_id: 1, user_name: 'admin', credential_id: 'c1', credential_name: '整理 agent' };

const evidence: EvidencePacket = {
  fetched_at: '2026-03-01T09:00:00Z',
  local: { directory_name: '[汉化组] 某作 Vol.01', book_count: 3, file_names: ['01.cbz', '02.cbz'], comic_info: { series: '某作', authors: ['作者甲'], year: 2001 } },
  queries: ['某作'],
  sources: [
    { source: 'bangumi', status: 'available' },
    { source: 'anilist', status: 'available' },
    { source: 'mangadex', status: 'unavailable' },
  ],
  candidates: [
    { id: 'bangumi:1', source: 'bangumi', entry_url: 'https://bgm.tv/subject/1', title: '某作', original_title: '某作 原名', authors: ['作者甲'], year: 2001, summary: '原作简介', signals: { title_matches: true, author_matches: true, source_volumes: 3, local_books: 3 } },
    { id: 'bangumi:2', source: 'bangumi', entry_url: 'https://bgm.tv/subject/2', title: '某作 新装版', signals: { title_matches: false, source_volumes: 5, local_books: 3 } },
    { id: 'anilist:1', source: 'anilist', entry_url: 'https://anilist.co/manga/1', title: 'Some Work', signals: { title_matches: false, author_matches: false, source_volumes: 0, local_books: 3 } },
  ],
};

const escalated: SeriesIdentification = {
  series_id: 7,
  library_id: 1,
  status: 'awaiting_owner',
  conclusions: [],
  excluded_candidates: [],
  escalations: [{ id: 31, category: 'ambiguous_candidates', class: 'identification', reason: '原作和新装版都对得上', raised_by: agent, raised_at: '2026-03-01T09:01:00Z' }],
  evidence,
};

const noted: SeriesIdentification = {
  series_id: 8,
  library_id: 1,
  status: 'unprocessed',
  conclusions: [],
  excluded_candidates: [],
  escalations: [{ id: 32, category: 'tag_problem', class: 'note', reason: '标签里有店名', raised_by: agent, raised_at: '2026-03-01T09:02:00Z' }],
};

const contradicted: SeriesIdentification = {
  series_id: 9,
  library_id: 1,
  status: 'needs_recheck',
  conclusions: [{
    source: 'bangumi',
    entry_id: '1234',
    entry_url: 'https://bgm.tv/subject/1234',
    title: 'MONSTER',
    original_title: '',
    aliases: [],
    confirmer: agent,
    confirmed_at: '2026-02-01T00:00:00Z',
    reason: 'title and author match',
    proposal_id: 0,
  }],
  excluded_candidates: [],
  escalations: [{
    id: 33,
    category: 'new_book_contradiction',
    class: 'recheck',
    reason: 'new book "Other 01.cbz" has ComicInfo series "Other", which matches no identified title, original title or alias',
    clue: 'Other',
    raised_by: { kind: 'system', user_id: 0, user_name: '', credential_id: '', credential_name: '' },
    raised_at: '2026-03-01T09:03:00Z',
  }],
};

function item(identification: SeriesIdentification, title: string): AwaitingSeries {
  return { series_id: identification.series_id, library_name: '漫画', directory_name: `${title} 目录`, title, book_count: 3, cover_book_id: 0, identification };
}

const fullPage: AwaitingPage = {
  items: [item(escalated, '某作'), item(noted, '另一部'), item(contradicted, '第三部')],
  total: 3,
  categories: [
    { category: 'ambiguous_candidates', class: 'identification', series: 1 },
    { category: 'new_book_contradiction', class: 'recheck', series: 1 },
    { category: 'tag_problem', class: 'note', series: 1 },
  ],
};

interface Call {
  method: string;
  url: string;
  body?: unknown;
}

type Responder = (call: Call) => { status?: number; data: unknown };

const originalAdapter = apiClient.defaults.adapter;
let calls: Call[] = [];

function serve(respond: Responder) {
  calls = [];
  apiClient.defaults.adapter = async (config: AxiosRequestConfig) => {
    const call: Call = { method: config.method ?? 'get', url: config.url ?? '', body: config.data ? JSON.parse(String(config.data)) : undefined };
    calls.push(call);
    const { status = 200, data } = respond(call);
    const response = { data, status, statusText: String(status), headers: {}, config };
    if (status >= 400) {
      throw Object.assign(new Error(`status ${status}`), { isAxiosError: true, response, config });
    }
    return response;
  };
}

function listOr(call: Call, other: () => { status?: number; data: unknown }) {
  if (call.method === 'get' && call.url.startsWith('/api/identification/pending')) return { data: fullPage };
  return other();
}

function renderPage() {
  render(
    <MemoryRouter>
      <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
        <ToastProvider>
          <IdentificationPending />
        </ToastProvider>
      </LocaleProvider>
    </MemoryRouter>,
  );
}

// 最近一次不是取列表或计数的请求：决定之后外层会重取计数。
function lastDecision() {
  return calls.filter((c) => !c.url.startsWith('/api/identification/pending')).at(-1);
}

function card(seriesId: number) {
  return screen.getByTestId(`pending-series-${seriesId}`);
}

function candidate(id: string) {
  return screen.getByTestId(`pending-candidate-${id}`);
}

afterEach(() => {
  cleanup();
  apiClient.defaults.adapter = originalAdapter;
});

describe('IdentificationPending', () => {
  it('lists escalations, evidence and the agents\' reasons, and filters by category', async () => {
    serve((call) => listOr(call, () => ({ data: {} })));
    renderPage();

    await screen.findByText('原作和新装版都对得上');
    expect(calls[0].url).toBe('/api/identification/pending?offset=0&limit=20');
    const first = card(7);
    expect(within(first).getByText('多个候选难分')).toBeTruthy();
    expect(within(first).getByText(/整理 agent/)).toBeTruthy();
    expect(within(first).getByText('[汉化组] 某作 Vol.01')).toBeTruthy();
    expect(within(first).getByText('MangaDex 不可用')).toBeTruthy();
    expect(within(candidate('bangumi:1')).getByRole('link', { name: /某作/ }).getAttribute('href')).toBe('https://bgm.tv/subject/1');
    expect(within(candidate('bangumi:1')).getByText('标题一致')).toBeTruthy();
    expect(within(candidate('bangumi:1')).getByText('作者一致')).toBeTruthy();
    expect(within(candidate('anilist:1')).getByText('作者不一致')).toBeTruthy();
    expect(within(candidate('bangumi:2')).queryByText(/作者/)).toBeNull();

    // A note offers closing, and says closing keeps the same category from being reported again.
    expect(within(card(8)).getByText('标签问题')).toBeTruthy();
    expect(within(card(8)).getByText(zhCN['identification.pending.closeNoteHint'])).toBeTruthy();
    expect(within(card(8)).queryByRole('button', { name: '确认是这部' })).toBeNull();
    // A system recheck is explained in the page's language, with the raw fact kept beside it.
    expect(within(card(9)).getByText('新加入的书带的 ComicInfo 系列名「Other」，与这个系列所有来源的识别标题、原名和别名都对不上。')).toBeTruthy();
    expect(within(card(9)).getByText(/Other 01\.cbz/)).toBeTruthy();
    // The identification a recheck questions is shown beside it.
    expect(within(screen.getByTestId('pending-conclusion-bangumi')).getByRole('link', { name: /MONSTER/ }).getAttribute('href')).toBe('https://bgm.tv/subject/1234');

    fireEvent.click(screen.getByRole('button', { name: /标签问题/ }));
    await waitFor(() => expect(calls.at(-1)?.url).toBe('/api/identification/pending?offset=0&limit=20&category=tag_problem'));
  });

  it('picks one candidate, or the selected ones, and shows the series settled', async () => {
    const identified: SeriesIdentification = { ...escalated, status: 'identified', escalations: undefined };
    serve((call) => listOr(call, () => ({ data: { identification: identified, operation_id: 'op1' } })));
    renderPage();

    fireEvent.click(within(await screen.findByTestId('pending-candidate-bangumi:1')).getByRole('button', { name: '确认是这部' }));
    expect(await within(card(7)).findByText('已处理，当前识别状态：已识别')).toBeTruthy();
    expect(lastDecision()).toEqual({ method: 'post', url: '/api/identification/series/7/identify', body: { candidate_ids: ['bangumi:1'] } });
    expect(screen.getByText(zhCN['identification.pending.picked'])).toBeTruthy();
    // The filter's counts are read again once a decision lands.
    await waitFor(() => expect(calls.filter((c) => c.url === '/api/identification/pending?limit=0')).toHaveLength(2));
  });

  it('confirms the selected candidates of several sources at once, one per source', async () => {
    const identified: SeriesIdentification = { ...escalated, status: 'identified', escalations: undefined };
    serve((call) => listOr(call, () => ({ data: { identification: identified, operation_id: 'op1' } })));
    renderPage();

    fireEvent.click(within(await screen.findByTestId('pending-candidate-bangumi:2')).getByRole('checkbox'));
    fireEvent.click(within(candidate('bangumi:1')).getByRole('checkbox'));
    fireEvent.click(within(candidate('anilist:1')).getByRole('checkbox'));
    fireEvent.click(within(card(7)).getByRole('button', { name: '确认所选（2）' }));
    await within(card(7)).findByText('已处理，当前识别状态：已识别');
    expect(lastDecision()).toEqual({ method: 'post', url: '/api/identification/series/7/identify', body: { candidate_ids: ['bangumi:1', 'anilist:1'] } });
  });

  it('excludes a candidate and draws the evidence the server answers with', async () => {
    const after: SeriesIdentification = {
      ...escalated,
      excluded_candidates: [{ source: 'bangumi', entry_id: '2', entry_url: 'https://bgm.tv/subject/2', title: '某作 新装版', user_id: 1, excluded_at: '2026-03-01T10:00:00Z', proposal_id: 0 }],
      evidence: { ...evidence, candidates: evidence.candidates.filter((c) => c.id !== 'bangumi:2') },
    };
    serve((call) => listOr(call, () => ({ data: after })));
    renderPage();

    fireEvent.click(within(await screen.findByTestId('pending-candidate-bangumi:2')).getByRole('button', { name: '否决' }));
    await waitFor(() => expect(screen.queryByTestId('pending-candidate-bangumi:2')).toBeNull());
    expect(lastDecision()).toEqual({ method: 'post', url: '/api/identification/series/7/exclusions', body: { candidate_id: 'bangumi:2' } });
    expect(within(card(7)).getByRole('button', { name: '判为查无此作' })).toBeTruthy();
  });

  it('concludes not found after naming the sources it records as searched', async () => {
    const after: SeriesIdentification = { ...escalated, status: 'not_found', escalations: undefined };
    serve((call) => listOr(call, () => ({ data: after })));
    renderPage();

    fireEvent.click(within(await screen.findByTestId('pending-series-7')).getByRole('button', { name: '判为查无此作' }));
    expect(await screen.findByText('记为查无此作，搜过的来源：Bangumi、AniList。以后新启用的来源会让这个系列重新可处理。')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '确认' }));
    expect(await within(card(7)).findByText('已处理，当前识别状态：查无此作')).toBeTruthy();
    expect(lastDecision()).toMatchObject({ method: 'post', url: '/api/identification/series/7/not-found' });
  });

  it('closes a note and keeps the series\' status', async () => {
    serve((call) => listOr(call, () => ({ data: { ...noted, escalations: undefined } })));
    renderPage();

    fireEvent.click(within(await screen.findByTestId('pending-series-8')).getByRole('button', { name: '关闭备注' }));
    expect(await within(card(8)).findByText('已处理，当前识别状态：未处理')).toBeTruthy();
    expect(lastDecision()).toMatchObject({ method: 'post', url: '/api/identification/series/8/notes/32/close' });
  });

  it('searches again with a custom term and shows what joined the evidence', async () => {
    const researched: SeriesIdentification = {
      ...escalated,
      evidence: {
        ...evidence,
        candidates: [...evidence.candidates, { id: 'bangumi:5', source: 'bangumi', entry_url: 'https://bgm.tv/subject/5', title: '某作 外传', signals: { title_matches: false, source_volumes: 1, local_books: 3 } }],
      },
    };
    serve((call) => listOr(call, () => {
      if (call.method === 'post') return { data: { series_id: 7, query: '某作 作者甲', status: 'completed', run_id: 4, added: ['bangumi:5'] } };
      return { data: researched };
    }));
    renderPage();

    const series = await screen.findByTestId('pending-series-7');
    fireEvent.change(within(series).getByPlaceholderText(zhCN['identification.pending.researchPlaceholder']), { target: { value: '某作 作者甲' } });
    fireEvent.click(within(series).getByRole('button', { name: '重搜' }));
    expect(await screen.findByTestId('pending-candidate-bangumi:5')).toBeTruthy();
    expect(calls.find((c) => c.method === 'post')).toEqual({ method: 'post', url: '/api/identification/series/7/research', body: { query: '某作 作者甲' } });
    expect(lastDecision()).toEqual({ method: 'get', url: '/api/identification/series/7', body: undefined });
  });

  it('sets the series owner-reviewed from the server answer', async () => {
    serve((call) => listOr(call, () => ({ data: { ...escalated, status: 'owner_reviewed', owner_review: { user_id: 1, user_name: 'admin', reviewed_at: '2026-03-01T10:00:00Z' } } })));
    renderPage();

    fireEvent.click(within(await screen.findByTestId('pending-series-7')).getByRole('button', { name: '设为已审定' }));
    expect(await within(card(7)).findByText('已处理，当前识别状态：已审定')).toBeTruthy();
    expect(lastDecision()).toEqual({ method: 'put', url: '/api/identification/series/7/owner-review', body: undefined });
  });

  it('reads the series again when another decision got there first, and shows what it holds', async () => {
    const elsewhere: SeriesIdentification = { ...escalated, status: 'identified', escalations: [] };
    serve((call) => listOr(call, () => {
      if (call.method === 'post') return { status: 409, data: { code: 'not_pending', error: 'The series no longer waits for this decision' } };
      return { data: elsewhere };
    }));
    renderPage();

    fireEvent.click(within(await screen.findByTestId('pending-candidate-bangumi:1')).getByRole('button', { name: '确认是这部' }));
    expect(await screen.findByText(zhCN['identification.pending.conflict.not_pending'])).toBeTruthy();
    expect(within(card(7)).getByText('已处理，当前识别状态：已识别')).toBeTruthy();
    expect(lastDecision()).toEqual({ method: 'get', url: '/api/identification/series/7', body: undefined });
  });

  it('keeps the series as shown when a decision fails without a conflict', async () => {
    serve((call) => listOr(call, () => ({ status: 500, data: { error: 'Identification unavailable' } })));
    renderPage();

    fireEvent.click(within(await screen.findByTestId('pending-candidate-bangumi:1')).getByRole('button', { name: '确认是这部' }));
    expect(await screen.findByText('Identification unavailable')).toBeTruthy();
    expect(within(card(7)).getByRole('button', { name: '判为查无此作' })).toBeTruthy();
    expect(within(card(7)).queryByText(/已处理/)).toBeNull();
  });

  it('keeps a questioned identification, saying the name will not question it again', async () => {
    serve((call) => listOr(call, () => ({ data: { ...contradicted, status: 'identified', escalations: undefined } })));
    renderPage();

    const series = await screen.findByTestId('pending-series-9');
    // A recheck is answered by keeping the identification or replacing it on the series page, not by
    // picking or concluding here.
    expect(within(series).queryByRole('button', { name: '确认是这部' })).toBeNull();
    expect(within(series).queryByRole('button', { name: '判为查无此作' })).toBeNull();
    expect(within(series).getByRole('button', { name: '设为已审定' })).toBeTruthy();
    expect(within(series).getByRole('button', { name: '重搜' })).toBeTruthy();
    expect(within(series).getByText(/以后这个系列的新书带「Other」不再报/)).toBeTruthy();
    expect(within(series).getByRole('link', { name: '去系列详情页撤换识别' }).getAttribute('href')).toBe('/series/9');

    fireEvent.click(within(series).getByRole('button', { name: '识别无误' }));
    expect(await within(card(9)).findByText('已处理，当前识别状态：已识别')).toBeTruthy();
    expect(lastDecision()).toEqual({ method: 'post', url: '/api/identification/series/9/keep-identification', body: undefined });
    expect(screen.getByText(zhCN['identification.pending.kept'])).toBeTruthy();
  });
});
