import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError } from './api/client';

/** 页面取数的统一壳：loading / error / reload */
export function useLoad<T>(fn: () => Promise<T>, deps: unknown[] = []) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [loading, setLoading] = useState(true);
  const seq = useRef(0);
  const reload = useCallback(async () => {
    const my = ++seq.current;
    setLoading(true);
    try { const d = await fn(); if (my === seq.current) { setData(d); setError(null); } }
    catch (e) { if (my === seq.current) setError(e instanceof ApiError ? e : new ApiError(-1, String(e))); }
    finally { if (my === seq.current) setLoading(false); }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  useEffect(() => { void reload(); }, [reload]);
  return { data, setData, error, loading, reload };
}

export function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => { const id = setTimeout(() => setV(value), ms); return () => clearTimeout(id); }, [value, ms]);
  return v;
}

/** 把 ApiError 变成「发生了什么：原因」一行 + 详情 */
export const errText = (e: ApiError | null) => (e ? { message: e.message, detail: e.detail } : null);
