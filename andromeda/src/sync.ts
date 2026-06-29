// Durable catch-up sync — the desktop's safety net for the work feed.
//
// The live `events` SSE push (hooks.useEvents) refreshes the feed the instant a
// proactive card arrives, but that push is best-effort: a card created while the
// stream was dropped (reconnect, the machine asleep) never reaches the feed, so
// it sits invisible until the 60s list cache happens to expire — the "알림은 왔는데
// 작업 피드엔 안 뜸" symptom. The native client solves this with a cursor-based
// catch-up pull (miniapp.sync.pull); this mirrors it for Andromeda.
//
// Andromeda doesn't hold lists in memory like the native client — Refine owns
// them — so reconciling just means invalidating the affected resource list(s) so
// they refetch.
import { useEffect } from "react";
import { useInvalidate } from "@refinedev/core";

import { clearCachedResource } from "./cachedList";
import { errText } from "./format";
import { type GatewayConfig, type SyncPullResult, syncPull } from "./gateway";
import { log } from "./log";
import { relatedResourcesForResource } from "./resourceRefresh";
import { getJSON, setJSON } from "./storage";

const syncLog = log.child("sync");

// Poll cadence while connected. The live SSE push handles the common case
// instantly; this is the fallback that bounds worst-case staleness when a push
// is missed. 30s keeps catch-up prompt without hammering the gateway (single user).
export const SYNC_POLL_MS = 30_000;

// Pages drained per cycle (the server caps a page at 100 events). Bounds a large
// backlog (e.g. machine asleep overnight) to a finite catch-up per cycle.
export const SYNC_MAX_PAGES = 4;

const SYNC_CURSOR_KEY = "andromeda.syncCursor";

export function loadSyncCursor(): number | undefined {
  const n = getJSON<number>(SYNC_CURSOR_KEY);
  return typeof n === "number" && Number.isFinite(n) ? n : undefined;
}

export function saveSyncCursor(cursor: number): void {
  setJSON(SYNC_CURSOR_KEY, cursor);
}

// Native-sync event type → the Refine resource list(s) to refetch. The type is
// "<base>.<verb>" (e.g. "workfeed.created"); we key off the base. Types with no
// desktop list resource (transcript.appended — the chat owns its own history)
// map to nothing. Reuses relatedResourcesForResource so calendar fans out to its
// dashboard + month lists exactly like the AI-mutation refresh path.
const SYNC_TYPE_BASE_RESOURCE: Record<string, string> = {
  workfeed: "workfeed",
  calendar: "calendar",
};

export function resourcesForSyncEventType(type: string): string[] {
  const base = type.split(".")[0];
  const resource = SYNC_TYPE_BASE_RESOURCE[base];
  return resource ? relatedResourcesForResource(resource) : [];
}

// Drain the sync log from `fromCursor`, returning the advanced cursor and the
// de-duplicated set of resources whose lists changed. Pure given an injected
// `pull` (so it's tested without a gateway). Stops on !hasMore, a non-advancing
// cursor (defensive — never loop forever on a stuck server), or maxPages.
export async function drainSync(
  pull: (cursor: number) => Promise<SyncPullResult>,
  fromCursor: number,
  maxPages = SYNC_MAX_PAGES,
): Promise<{ cursor: number; affected: string[] }> {
  let cursor = fromCursor;
  const affected = new Set<string>();
  for (let pages = 0; pages < maxPages; pages++) {
    const res = await pull(cursor);
    for (const ev of res.events ?? []) {
      for (const r of resourcesForSyncEventType(ev.type)) affected.add(r);
    }
    const next = typeof res.cursor === "number" && res.cursor > cursor ? res.cursor : cursor;
    const advanced = next > cursor;
    cursor = next;
    if (!res.hasMore || !advanced) break;
  }
  return { cursor, affected: [...affected] };
}

// Background catch-up: while connected, reconcile the sync log on connect and
// every SYNC_POLL_MS. A fresh install (no stored cursor) only baselines the
// cursor to the current end — the panes' initial fetch is already current, so we
// don't refetch on startup; thereafter every cycle invalidates whatever changed.
// Mount once, session-scoped (Workstation).
export function useNativeSync(cfg: GatewayConfig, connected: boolean): void {
  const invalidate = useInvalidate();
  useEffect(() => {
    if (!connected) return;
    let cancelled = false;

    const cycle = async () => {
      const stored = loadSyncCursor();
      try {
        if (stored === undefined) {
          // Baseline: jump the cursor to the current end without refetching.
          const res = await syncPull(cfg, 0, 1);
          if (cancelled) return;
          saveSyncCursor(Math.max(res.latestSeq ?? 0, res.cursor ?? 0));
          return;
        }
        const { cursor, affected } = await drainSync((c) => syncPull(cfg, c), stored);
        if (cancelled) return;
        saveSyncCursor(cursor);
        for (const resource of affected) {
          clearCachedResource(resource);
          invalidate({ resource, invalidates: ["list"] });
        }
      } catch (e) {
        if (!cancelled) syncLog.warn("catch-up sync failed", errText(e));
      }
    };

    void cycle();
    const id = setInterval(() => void cycle(), SYNC_POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
    // Re-arm only when the connection identity changes (invalidate is stable).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cfg.url, cfg.token, connected]);
}
