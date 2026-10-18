import { useCallback, useEffect, useRef, useState } from 'react';

export interface Resource<T> {
  data: T | undefined;
  error: Error | undefined;
  loading: boolean;
  reload: () => Promise<void>;
  set: (v: T) => void;
}

/** useResource loads data on mount and on reload; a stale answer never lands. */
export function useResource<T>(load: () => Promise<T>): Resource<T> {
  const [data, setData] = useState<T>();
  const [error, setError] = useState<Error>();
  const [loading, setLoading] = useState(true);
  const generation = useRef(0);
  const loadRef = useRef(load);
  loadRef.current = load;
  const reload = useCallback(async () => {
    const g = ++generation.current;
    setLoading(true);
    try {
      const v = await loadRef.current();
      if (g === generation.current) {
        setData(v);
        setError(undefined);
      }
    } catch (e) {
      if (g === generation.current) setError(e as Error);
    } finally {
      if (g === generation.current) setLoading(false);
    }
  }, []);
  useEffect(() => {
    reload();
  }, [reload]);
  return { data, error, loading, reload, set: setData };
}

/** useNow ticks every period, for relative times and countdowns. */
export function useNow(periodMs = 30_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), periodMs);
    return () => clearInterval(id);
  }, [periodMs]);
  return now;
}

/**
 * usePresence keeps an element mounted while it animates out. `shown` turns
 * true a frame after mounting, so a CSS transition runs from the closed
 * styles to the open ones.
 */
export function usePresence(open: boolean, exitMs: number): { mounted: boolean; shown: boolean } {
  const [mounted, setMounted] = useState(open);
  const [shown, setShown] = useState(false);
  useEffect(() => {
    if (open) {
      setMounted(true);
      let inner = 0;
      const outer = requestAnimationFrame(() => {
        inner = requestAnimationFrame(() => setShown(true));
      });
      return () => {
        cancelAnimationFrame(outer);
        cancelAnimationFrame(inner);
      };
    }
    setShown(false);
    const t = setTimeout(() => setMounted(false), exitMs);
    return () => clearTimeout(t);
  }, [open, exitMs]);
  return { mounted, shown };
}

/** useBusy wraps an async action with a busy flag. */
export function useBusy(): [boolean, <T>(p: Promise<T>) => Promise<T>] {
  const [busy, setBusy] = useState(false);
  const run = useCallback(async <T,>(p: Promise<T>) => {
    setBusy(true);
    try {
      return await p;
    } finally {
      setBusy(false);
    }
  }, []);
  return [busy, run];
}
