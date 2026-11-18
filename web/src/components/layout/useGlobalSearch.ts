import { useEffect, useRef, useState } from 'react';
import { apiClient } from '../../api/client';
import type { SearchResult } from '../../api/generated';
import { useSessionActive, useSessionQueryScope } from '../../session/ApplicationSessionProvider';
import type { SearchHit } from './types';

export type SearchStatus = 'idle' | 'debouncing' | 'loading' | 'success' | 'error';
export type AppendStatus = 'idle' | 'loading' | 'error' | 'invalid';

export function useGlobalSearch() {
  const [session, setSession] = useState({ query: '', target: 'all', open: false, immediate: false });
  type Response = { owner: typeof session; scope: object; hits: SearchHit[]; status: SearchStatus; appendStatus: AppendStatus; hasMore: boolean; cursor: string };
  const [response, setResponse] = useState<Response>();
  const [selectedIndex, setSelectedIndex] = useState(0);
  const scope = useSessionQueryScope();
  const active = useSessionActive();
  const continuation = useRef<{ owner: typeof session; scope: object; load: () => void } | undefined>(undefined);
  // Ownership is checked during render too: debounce must never leave old hits selectable.
  const owned = response?.owner === session && response.scope === scope;
  const searchStatus: SearchStatus = !session.open || !session.query.trim() ? 'idle'
    : owned ? response.status : 'debouncing';
  const searchResults = searchStatus === 'success' && owned ? response.hits : [];
  const appendStatus = owned ? response.appendStatus : 'idle';
  const hasMore = owned && response.hasMore;

  useEffect(() => {
    const query = session.query.trim();
    if (!session.open || !query) return;
    let current = true;
    let pending = false;
    let state: Response = { owner: session, scope, hits: [], status: 'loading', appendStatus: 'idle', hasMore: false, cursor: '' };
    const controller = new AbortController();
    const publish = () => setResponse({ ...state });
    const request = (append: boolean) => {
      if (pending || !current || !active()) return;
      pending = true; // synchronous guard also covers multiple clicks before React renders
      if (append) state.appendStatus = 'loading';
      publish();
      const cursor = append ? `&cursor=${encodeURIComponent(state.cursor)}` : '';
      apiClient.get<SearchResult>(`/api/search?q=${encodeURIComponent(query)}&target=${session.target}${cursor}`, { signal: controller.signal })
        .then(res => {
          if (!current || !active()) return;
          const hits = append ? [...state.hits] : [];
          const seen = new Set(hits.map(hit => hit.id));
          for (const hit of res.data?.hits ?? []) {
            if (!seen.has(hit.id)) { seen.add(hit.id); hits.push(hit); }
          }
          state = { ...state, hits, status: 'success', appendStatus: 'idle', hasMore: res.data?.has_more ?? false, cursor: res.data?.next_cursor ?? '' };
          publish();
          if (!append) setSelectedIndex(0);
        }).catch((error: { response?: { data?: { code?: string } } }) => {
          if (!current || !active()) return;
          if (append) state.appendStatus = error.response?.data?.code === 'invalid_search_cursor' ? 'invalid' : 'error';
          else state.status = 'error';
          publish(); // append failures retain the accepted hits and unconsumed cursor
        }).finally(() => { pending = false; });
    };
    continuation.current = { owner: session, scope, load: () => {
      if (state.status === 'success' && state.hasMore && state.cursor && state.appendStatus !== 'invalid') request(true);
    } };
    const timer = session.immediate ? undefined : setTimeout(() => request(false), 300);
    if (session.immediate) request(false);
    return () => { current = false; controller.abort(); clearTimeout(timer); };
  }, [session, scope, active]);

  const loadMore = () => {
    const next = continuation.current;
    if (session.open && next?.owner === session && next.scope === scope) next.load();
  };
  const setSearchQuery = (query: string) => { setSession(value => ({ ...value, query, immediate: false })); setSelectedIndex(0); };
  const setSearchTarget = (target: string) => { setSession(value => ({ ...value, target, immediate: false })); setSelectedIndex(0); };
  const setIsSearchModalOpen = (open: boolean) => setSession(value => value.open === open ? value : { ...value, open, immediate: false });
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault();
        setSession(value => value.open ? value : { ...value, open: true, immediate: false });
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, []);

  const resetSearch = () => { setSession(value => ({ ...value, query: '' })); setSelectedIndex(0); };
  const retrySearch = () => {
    if (searchStatus === 'error' || appendStatus === 'invalid') {
      setSession(value => ({ ...value, immediate: true }));
      setSelectedIndex(0);
    }
  };
  return {
    searchStatus, retrySearch, hasMore, appendStatus, loadMore,
    searchQuery: session.query, setSearchQuery, searchResults,
    isSearchModalOpen: session.open, setIsSearchModalOpen,
    selectedIndex, setSelectedIndex, searchTarget: session.target, setSearchTarget, resetSearch,
  };
}
