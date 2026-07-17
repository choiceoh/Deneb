// Thin DIY data layer that replaces @refinedev/core for Andromeda's CRUD panes.
// Same call shapes the panes already use (useList/useOne/useCreate/…/useInvalidate)
// over a local query cache + the deneb DataProvider.
import {
  createContext,
  createElement,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useSyncExternalStore,
  type ReactNode,
} from "react";

export type BaseKey = string | number;
/** Unconstrained row type — domain interfaces (Todo, digests, …) often omit `id`. */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type BaseRecord = any;

export interface DataProvider {
  getApiUrl: () => string;
  getList: (params: { resource: string; meta?: any }) => Promise<{ data: any[]; total: number }>;
  getOne: (params: { resource: string; id: BaseKey }) => Promise<{ data: any }>;
  create: (params: { resource: string; variables: unknown }) => Promise<{ data: any }>;
  update: (params: { resource: string; id: BaseKey; variables: unknown }) => Promise<{ data: any }>;
  deleteOne: (params: { resource: string; id: BaseKey }) => Promise<{ data: any }>;
}

type ListResult<T> = { data: T[]; total: number };
type QueryStatus = "pending" | "success" | "error";

interface CacheEntry {
  status: QueryStatus;
  data?: unknown;
  error?: unknown;
  dataUpdatedAt: number;
  /** Bumped by invalidate(); observers refetch when this changes. */
  epoch: number;
}

type Listener = () => void;

function stableMeta(meta: unknown): string {
  if (meta == null) return "";
  try {
    return JSON.stringify(meta);
  } catch {
    return String(meta);
  }
}

class QueryCache {
  private entries = new Map<string, CacheEntry>();
  private listeners = new Map<string, Set<Listener>>();
  /** Active query keys per resource (list + one), so invalidate hits mounted observers. */
  private resourceKeys = new Map<string, Set<string>>();

  listKey(resource: string, meta?: unknown): string {
    return `list:${resource}:${stableMeta(meta)}`;
  }

  oneKey(resource: string, id: BaseKey): string {
    return `one:${resource}:${String(id)}`;
  }

  private ensure(key: string): CacheEntry {
    let e = this.entries.get(key);
    if (!e) {
      e = { status: "pending", dataUpdatedAt: 0, epoch: 0 };
      this.entries.set(key, e);
    }
    return e;
  }

  get(key: string): CacheEntry | undefined {
    return this.entries.get(key);
  }

  subscribe(key: string, resource: string, listener: Listener): () => void {
    this.ensure(key);
    let set = this.listeners.get(key);
    if (!set) {
      set = new Set();
      this.listeners.set(key, set);
    }
    set.add(listener);
    let keys = this.resourceKeys.get(resource);
    if (!keys) {
      keys = new Set();
      this.resourceKeys.set(resource, keys);
    }
    keys.add(key);
    return () => {
      set!.delete(listener);
      if (set!.size === 0) {
        this.listeners.delete(key);
        keys!.delete(key);
      }
    };
  }

  private notify(key: string) {
    const set = this.listeners.get(key);
    if (set) for (const l of set) l();
  }

  set(key: string, patch: Partial<CacheEntry>) {
    const prev = this.ensure(key);
    this.entries.set(key, { ...prev, ...patch });
    this.notify(key);
  }

  invalidate(resource: string, kinds: Array<"list" | "one"> = ["list"]) {
    const keys = this.resourceKeys.get(resource);
    if (!keys) return;
    for (const key of keys) {
      const kind = key.startsWith("list:") ? "list" : key.startsWith("one:") ? "one" : null;
      if (!kind || !kinds.includes(kind)) continue;
      const cur = this.ensure(key);
      this.entries.set(key, { ...cur, epoch: cur.epoch + 1 });
      this.notify(key);
    }
  }
}

interface DataCtx {
  provider: DataProvider;
  cache: QueryCache;
}

const DataContext = createContext<DataCtx | null>(null);

export function DataProviderScope({
  dataProvider,
  children,
}: {
  dataProvider: DataProvider;
  children: ReactNode;
}) {
  // Fresh cache per mount so tests don't leak query state across renders.
  const cache = useMemo(() => new QueryCache(), [dataProvider]);
  const value = useMemo(() => ({ provider: dataProvider, cache }), [dataProvider, cache]);
  return createElement(DataContext.Provider, { value }, children);
}

function useDataCtx(): DataCtx {
  const ctx = useContext(DataContext);
  if (!ctx) throw new Error("andromeda: DataProviderScope missing");
  return ctx;
}

export interface QueryObserver {
  isLoading: boolean;
  isPending: boolean;
  isFetching: boolean;
  isSuccess: boolean;
  isError: boolean;
  error: unknown;
  dataUpdatedAt: number;
  refetch: () => Promise<unknown>;
  status: QueryStatus;
}

function useQueryObserver(
  key: string,
  resource: string,
  enabled: boolean,
  fetch: () => Promise<void>,
  staleTime = 0,
): QueryObserver {
  const { cache } = useDataCtx();
  const fetchRef = useRef(fetch);
  fetchRef.current = fetch;

  const version = useSyncExternalStore(
    (onStoreChange) => cache.subscribe(key, resource, onStoreChange),
    () => {
      const e = cache.get(key);
      return `${e?.status ?? "none"}|${e?.dataUpdatedAt ?? 0}|${e?.epoch ?? 0}|${enabled}`;
    },
    () => "ssr",
  );
  void version;

  const entry = cache.get(key);
  const refetch = useCallback(async () => {
    if (!enabled) return;
    const keepData = cache.get(key)?.data != null;
    if (!keepData) cache.set(key, { status: "pending" });
    await fetchRef.current();
  }, [cache, enabled, key]);

  useEffect(() => {
    if (!enabled) return;
    const cur = cache.get(key);
    // invalidate() bumps epoch — always refetch. Otherwise honor staleTime so a
    // fresh initialData snapshot is not immediately overwritten by an empty/
    // slower network response (Refine-compatible).
    const invalidated = (cur?.epoch ?? 0) > 0;
    const fresh =
      !invalidated &&
      cur?.data != null &&
      cur.status === "success" &&
      staleTime > 0 &&
      Date.now() - cur.dataUpdatedAt < staleTime;
    if (fresh) return;
    void refetch();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, key, entry?.epoch, staleTime]);

  const hasData = entry?.data != null;
  const isLoading = enabled && !hasData && entry?.status !== "error";
  return {
    isLoading: Boolean(isLoading),
    isPending: Boolean(isLoading),
    isFetching: enabled && entry?.status === "pending",
    isSuccess: entry?.status === "success" || (hasData && entry?.status !== "error"),
    isError: entry?.status === "error",
    error: entry?.error,
    dataUpdatedAt: entry?.dataUpdatedAt ?? 0,
    refetch,
    status: entry?.status ?? "pending",
  };
}

export function useList<T extends BaseRecord = BaseRecord>(opts: {
  resource: string;
  meta?: Record<string, unknown>;
  queryOptions?: {
    enabled?: boolean;
    initialData?: ListResult<T>;
    initialDataUpdatedAt?: number;
    staleTime?: number;
    gcTime?: number;
    refetchOnWindowFocus?: boolean;
  };
}) {
  const { provider, cache } = useDataCtx();
  const enabled = opts.queryOptions?.enabled ?? true;
  const key = cache.listKey(opts.resource, opts.meta);
  const initial = opts.queryOptions?.initialData;
  const initialAt = opts.queryOptions?.initialDataUpdatedAt;

  // Seed synchronously before observers fetch so the first paint can show cache.
  if (initial && cache.get(key)?.data == null) {
    cache.set(key, {
      status: "success",
      data: initial,
      dataUpdatedAt: initialAt ?? Date.now(),
      epoch: 0,
    });
  }

  const query = useQueryObserver(
    key,
    opts.resource,
    enabled,
    async () => {
      try {
        const res = await provider.getList({ resource: opts.resource, meta: opts.meta });
        cache.set(key, {
          status: "success",
          data: { data: res.data as T[], total: res.total } satisfies ListResult<T>,
          error: undefined,
          dataUpdatedAt: Date.now(),
          epoch: 0,
        });
      } catch (e) {
        cache.set(key, { status: "error", error: e, dataUpdatedAt: Date.now(), epoch: 0 });
      }
    },
    opts.queryOptions?.staleTime ?? 0,
  );

  const entry = cache.get(key);
  const result = (entry?.data as ListResult<T> | undefined) ?? { data: [] as T[], total: 0 };
  return { query, result };
}

export function useOne<T extends BaseRecord = BaseRecord>(opts: {
  resource: string;
  id?: BaseKey;
  queryOptions?: {
    enabled?: boolean;
    initialData?: { data: T };
    initialDataUpdatedAt?: number;
    staleTime?: number;
    gcTime?: number;
    refetchOnWindowFocus?: boolean;
  };
}) {
  const { provider, cache } = useDataCtx();
  const enabled = (opts.queryOptions?.enabled ?? true) && opts.id !== undefined;
  const key = opts.id !== undefined ? cache.oneKey(opts.resource, opts.id) : `one:${opts.resource}:_`;
  const initial = opts.queryOptions?.initialData;
  const initialAt = opts.queryOptions?.initialDataUpdatedAt;

  if (opts.id !== undefined && initial && cache.get(key)?.data == null) {
    cache.set(key, {
      status: "success",
      data: initial.data,
      dataUpdatedAt: initialAt ?? Date.now(),
      epoch: 0,
    });
  }

  const id = opts.id;
  const query = useQueryObserver(
    key,
    opts.resource,
    enabled,
    async () => {
      if (id === undefined) return;
      try {
        const res = await provider.getOne({ resource: opts.resource, id });
        cache.set(key, {
          status: "success",
          data: res.data as T,
          error: undefined,
          dataUpdatedAt: Date.now(),
          epoch: 0,
        });
      } catch (e) {
        cache.set(key, { status: "error", error: e, dataUpdatedAt: Date.now(), epoch: 0 });
      }
    },
    opts.queryOptions?.staleTime ?? 0,
  );

  const entry = cache.get(key);
  return { query, result: (entry?.data as T | undefined) ?? undefined };
}

type MutateHandlers = { onSuccess?: () => void; onError?: (e: unknown) => void };

export function useCreate() {
  const { provider } = useDataCtx();
  const mutate = useCallback(
    (args: { resource: string; values: unknown }, handlers: MutateHandlers = {}) => {
      void provider
        .create({ resource: args.resource, variables: args.values })
        .then(() => handlers.onSuccess?.())
        .catch((e) => handlers.onError?.(e));
    },
    [provider],
  );
  return { mutate };
}

export function useUpdate() {
  const { provider } = useDataCtx();
  const mutate = useCallback(
    (args: { resource: string; id: BaseKey; values: unknown }, handlers: MutateHandlers = {}) => {
      void provider
        .update({ resource: args.resource, id: args.id, variables: args.values })
        .then(() => handlers.onSuccess?.())
        .catch((e) => handlers.onError?.(e));
    },
    [provider],
  );
  return { mutate };
}

export function useDelete() {
  const { provider } = useDataCtx();
  const mutate = useCallback(
    (args: { resource: string; id: BaseKey }, handlers: MutateHandlers = {}) => {
      void provider
        .deleteOne({ resource: args.resource, id: args.id })
        .then(() => handlers.onSuccess?.())
        .catch((e) => handlers.onError?.(e));
    },
    [provider],
  );
  return { mutate };
}

export function useInvalidate() {
  const { cache } = useDataCtx();
  return useCallback(
    (opts: { resource: string; invalidates?: Array<"list" | "detail" | "many" | "resourceAll"> }) => {
      const inv = opts.invalidates ?? ["list"];
      const kinds: Array<"list" | "one"> = [];
      if (inv.includes("list") || inv.includes("resourceAll") || inv.includes("many")) kinds.push("list");
      if (inv.includes("detail") || inv.includes("resourceAll")) kinds.push("one");
      cache.invalidate(opts.resource, kinds.length ? kinds : ["list"]);
    },
    [cache],
  );
}
