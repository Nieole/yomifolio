/**
 * @vitest-environment jsdom
 *
 * 守资料库筛选条的识别状态：只给管理员显示，选中后成为一个可单独清除的已选条件。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';

import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages as zhCN } from '../../i18n/locales/zh-CN';
import { LibraryFilterBar } from './LibraryFilterBar';
import { EMPTY_ADVANCED_FILTERS } from './hooks/libraryFilterParams';

function renderBar(props: { activeIdentification: string | null; showIdentification: boolean; onIdentificationChange?: (v: string | null) => void }) {
  render(
    <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
      <LibraryFilterBar
        allStatuses={[]}
        allTags={[]}
        allAuthors={[]}
        activeStatus={null}
        activeTag={null}
        activeAuthor={null}
        activeLetter={null}
        advanced={EMPTY_ADVANCED_FILTERS}
        smartFilterChips={[]}
        hasAnyFilter={props.activeIdentification !== null}
        onStatusChange={vi.fn()}
        onTagChange={vi.fn()}
        onAuthorChange={vi.fn()}
        onLetterChange={vi.fn()}
        onAdvancedChange={vi.fn()}
        onResetFilters={vi.fn()}
        showIdentification={props.showIdentification}
        activeIdentification={props.activeIdentification}
        onIdentificationChange={props.onIdentificationChange ?? vi.fn()}
      />
    </LocaleProvider>,
  );
}

afterEach(() => {
  cleanup();
});

describe('LibraryFilterBar 识别状态', () => {
  it('lets an administrator filter by identification status', () => {
    const onChange = vi.fn();
    renderBar({ activeIdentification: null, showIdentification: true, onIdentificationChange: onChange });

    fireEvent.click(screen.getByRole('button', { name: '展开筛选' }));
    expect(screen.getByText('识别状态')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '查无此作' }));
    expect(onChange).toHaveBeenCalledWith('not_found');
  });

  it('shows the chosen status as a chip that clears it', () => {
    const onChange = vi.fn();
    renderBar({ activeIdentification: 'awaiting_owner', showIdentification: true, onIdentificationChange: onChange });

    fireEvent.click(screen.getByText('识别状态：待人工').querySelector('button') as HTMLElement);
    expect(onChange).toHaveBeenCalledWith(null);
  });

  it('offers no identification filter to a regular account', () => {
    renderBar({ activeIdentification: null, showIdentification: false });

    fireEvent.click(screen.getByRole('button', { name: '展开筛选' }));
    expect(screen.queryByText('识别状态')).toBeNull();
    expect(screen.queryByRole('button', { name: '查无此作' })).toBeNull();
  });
});
