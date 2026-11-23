import { useCallback, useEffect, useRef, useState } from 'react';
import { apiClient, getApiErrorMessage } from '../../api/client';
import type { CleanupResult, Operation } from '../../api/generated';

// The durable cleaned_by link, not a browser cache, is the recovery boundary.
export function useOperationCleanups(rows: Operation[], failedMessage: string) {
  const [results, setResults] = useState<Record<string, CleanupResult>>({});
  const [error, setError] = useState('');
  const known = useRef(new Set<string>());
  const keys = useRef(new Map<string, string>());
  const [refresh, setRefresh] = useState(0);
  useEffect(() => {
    let active = true;
    const ids = [...new Set(rows.flatMap((row) => row.cleaned_by ? [row.cleaned_by] : []))];
    void Promise.all(ids.filter((id) => !known.current.has(id)).map(async (id) => {
      const { data } = await apiClient.get<CleanupResult>(`/api/mcp/cleanups/${encodeURIComponent(id)}`);
      if (active) { known.current.add(id); setResults((old) => ({ ...old, [id]: data })); }
    })).catch((err) => { if (active) setError(getApiErrorMessage(err, failedMessage)); });
    return () => { active = false; };
  }, [rows, failedMessage, refresh]);
  const clean = useCallback(async (row: Operation) => {
    const result = row.cleaned_by ? results[row.cleaned_by] : undefined;
    // An isolated operation must NEVER allocate a new identity, even if lookup failed.
    const key = row.cleaned_by ? result?.retry_request_key : keys.current.get(row.id) ?? crypto.randomUUID();
    if (!key || (row.cleaned_by && (result?.outcome !== 'pending' || result.operation_id !== row.id))) throw new Error(failedMessage);
    keys.current.set(row.id, key);
    const { data } = await apiClient.post<CleanupResult>(`/api/mcp/operations/${row.id}/cleanup`, { request_key: key });
    known.current.add(data.id);
    setResults((old) => ({ ...old, [data.id]: data }));
    return data;
  }, [results, failedMessage]);
  return { results, error, clean, reload: () => { known.current.clear(); setError(''); setRefresh((old) => old + 1); } };
}
