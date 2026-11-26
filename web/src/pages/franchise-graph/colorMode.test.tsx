// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { apiClient } from '../../api/client';
import { ThemeProvider, useTheme } from '../../theme/ThemeProvider';
import type { FranchiseRelation } from '../series-detail/types';
import { FranchiseGraphPage } from '.';

// t 必须引用稳定：页面把它放进取数回调的依赖里。
vi.mock('../../i18n/LocaleProvider', () => {
  const t = (key: string) => key;
  return { useI18n: () => ({ t }) };
});

const relation: FranchiseRelation = {
  id: 1, source_series_id: 1, target_series_id: 2, relation_type: 'sequel',
  source_series_name: 'First', target_series_name: 'Second', source_cover_path: '', target_cover_path: '',
};
const originalAdapter = apiClient.defaults.adapter;

beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} unobserve() {} });
  apiClient.defaults.adapter = async config => ({ data: [relation], status: 200, statusText: 'OK', headers: {}, config });
});
afterEach(() => {
  cleanup();
  apiClient.defaults.adapter = originalAdapter;
  vi.unstubAllGlobals();
});

function ThemeSwitch() {
  const { setTheme } = useTheme();
  return <><button onClick={() => setTheme('paper')}>paper</button><button onClick={() => setTheme('midnight')}>midnight</button></>;
}

it('forces the React Flow color mode from the app theme rather than the system preference', async () => {
  const { container } = render(
    <ThemeProvider>
      <ThemeSwitch />
      <MemoryRouter initialEntries={['/series/1/franchise-graph']}>
        <Routes><Route path="/series/:id/franchise-graph" element={<FranchiseGraphPage />} /></Routes>
      </MemoryRouter>
    </ThemeProvider>,
  );
  const flow = () => container.querySelector('.react-flow');

  fireEvent.click(screen.getByText('paper'));
  await waitFor(() => expect(flow()).not.toBeNull());
  expect(flow()!.classList).toContain('light');
  expect(flow()!.classList).not.toContain('dark');

  fireEvent.click(screen.getByText('midnight'));
  await waitFor(() => expect(flow()!.classList).toContain('dark'));
  expect(flow()!.classList).not.toContain('light');
});
