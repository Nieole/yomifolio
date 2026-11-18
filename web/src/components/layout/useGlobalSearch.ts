import { useEffect, useState } from 'react';
import { apiClient } from '../../api/client';
import { useSessionActive, useSessionQueryScope } from '../../session/ApplicationSessionProvider';
import type { SearchHit } from './types';

export type SearchStatus = 'idle' | 'debouncing' | 'loading' | 'success' | 'error';

export function useGlobalSearch() {
  const [session, setSession] = useState({ query: '', target: 'all', open: false, immediate: false });
  const [response, setResponse] = useState<{ owner: typeof session; scope: object; hits: SearchHit[]; status: SearchStatus }>();
  const [selectedIndex, setSelectedIndex] = useState(0);
  const scope = useSessionQueryScope();
  const active = useSessionActive();
  // Ownership is checked during render too: debounce must never leave old hits selectable.
  const owned = response?.owner === session && response.scope === scope;
  const searchStatus: SearchStatus = !session.open || !session.query.trim() ? 'idle'
    : owned ? response.status : 'debouncing';
  const searchResults = searchStatus === 'success' && owned ? response.hits : [];

  useEffect(() => {
    const query = session.query.trim();
    if (!session.open || !query) return;
    let current = true;
    const controller = new AbortController();
    const request = () => {
      setResponse({ owner: session, scope, hits: [], status: 'loading' });
      apiClient.get(`/api/search?q=${encodeURIComponent(query)}&target=${session.target}`, { signal: controller.signal })
        .then(res => {
          if (current && active()) {
            setResponse({ owner: session, scope, hits: res.data?.hits ?? [], status: 'success' });
            setSelectedIndex(0);
          }
        }).catch(() => {
          if (current && active()) setResponse({ owner: session, scope, hits: [], status: 'error' });
        });
    };
    const timer = session.immediate ? undefined : setTimeout(request, 300);
    if (session.immediate) request();
    return () => { current = false; controller.abort(); clearTimeout(timer); };
  }, [session, scope, active]);

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
    if (searchStatus === 'error') setSession(value => ({ ...value, immediate: true }));
  };
  return {
    searchStatus, retrySearch,
    searchQuery: session.query, setSearchQuery, searchResults,
    isSearchModalOpen: session.open, setIsSearchModalOpen,
    selectedIndex, setSelectedIndex, searchTarget: session.target, setSearchTarget, resetSearch,
  };
}
