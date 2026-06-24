import { useEffect, useState, type DependencyList, type Dispatch, type SetStateAction } from "react";

// Run an async `load` when the component opens and whenever `deps` change, with a
// cancellation guard so a result from a superseded (or unmounted) load never lands.
// Data resets to null at the start of each enabled load — i.e. the consumer flashes
// to its empty state on every dep change rather than keeping the previous value
// visible during a refetch. That suits the small enrichment cards here; a pane that
// needs "keep previous data" should use the cache-backed useCachedRpc instead.
// Errors are swallowed by default — pass `onError` to surface them.
//
// Returns [data, setData]; the setter lets a caller also update the value
// imperatively (e.g. a manual re-run that reuses the same state) without a second
// piece of state. Extracted from the mail enrichment cards, which all shared this
// fetch-on-open shape.
export function useAsyncOnOpen<T>(
  load: () => Promise<T>,
  deps: DependencyList,
  opts: { enabled?: boolean; onError?: (e: unknown) => void } = {},
): [T | null, Dispatch<SetStateAction<T | null>>] {
  const { enabled = true, onError } = opts;
  const [data, setData] = useState<T | null>(null);

  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    setData(null);
    load()
      .then((r) => {
        if (!cancelled) setData(r);
      })
      .catch((e) => {
        if (!cancelled) onError?.(e);
      });
    return () => {
      cancelled = true;
    };
    // `load`/`onError` are intentionally excluded — the caller controls re-runs
    // through `deps` and `enabled` (the loader is a fresh closure each render).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, ...deps]);

  return [data, setData];
}
