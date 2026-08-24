import { afterEach, describe, expect, it, vi } from "vitest";

import type { SyncPullResult } from "./gateway";
import { drainSync, loadSyncCursor, resourcesForSyncEventType, saveSyncCursor } from "./sync";

afterEach(() => localStorage.clear());

const page = (over: Partial<SyncPullResult>): SyncPullResult => ({
  events: [],
  cursor: 0,
  latestSeq: 0,
  hasMore: false,
  count: 0,
  ...over,
});

describe("resourcesForSyncEventType", () => {
  it("when maps every work-feed event type to the workfeed list", () => {
    expect(resourcesForSyncEventType("workfeed.created")).toEqual(["workfeed"]);
    expect(resourcesForSyncEventType("workfeed.updated")).toEqual(["workfeed"]);
    expect(resourcesForSyncEventType("workfeed.action.run")).toEqual(["workfeed"]);
  });

  it("when fans calendar changes out to both calendar lists (dashboard + month)", () => {
    expect(resourcesForSyncEventType("calendar.changed")).toEqual(["calendar", "calendar-range"]);
  });

  it("maps the gateway change-feed domains (mail/approvals/wiki) to their lists", () => {
    expect(resourcesForSyncEventType("mail.changed")).toEqual(["mail"]);
    expect(resourcesForSyncEventType("approvals.changed")).toEqual(["approvals"]);
    // Wiki fans out like the AI-mutation refresh path: page lists, search, notebooks.
    expect(resourcesForSyncEventType("wiki.changed")).toEqual(["wiki", "search", "notebook"]);
  });

  it("ignores event types with no desktop list resource", () => {
    expect(resourcesForSyncEventType("transcript.appended")).toEqual([]);
    // No desktop org pane yet — org.changed intentionally maps to nothing.
    expect(resourcesForSyncEventType("org.changed")).toEqual([]);
    expect(resourcesForSyncEventType("nonsense")).toEqual([]);
  });
});

describe("drainSync", () => {
  it("returns deduplicated resources when draining multiple sync pages", async () => {
    const pages = [
      page({
        events: [
          { seq: 1, type: "workfeed.created" },
          { seq: 2, type: "calendar.changed" },
        ],
        cursor: 2,
        hasMore: true,
      }),
      page({ events: [{ seq: 3, type: "workfeed.updated" }], cursor: 5 }),
    ];
    let i = 0;
    const pull = vi.fn(async () => pages[i++]);

    const { cursor, affected } = await drainSync(pull, 0);

    expect(pull).toHaveBeenCalledTimes(2);
    expect(cursor).toBe(5);
    expect(new Set(affected)).toEqual(new Set(["workfeed", "calendar", "calendar-range"]));
  });

  it("stops at maxPages even when the server keeps signalling hasMore", async () => {
    const pull = vi.fn(async (c: number) =>
      page({ events: [{ seq: c + 1, type: "workfeed.created" }], cursor: c + 1, hasMore: true }),
    );

    const { cursor } = await drainSync(pull, 0, 2);

    expect(pull).toHaveBeenCalledTimes(2);
    expect(cursor).toBe(2);
  });

  it("stops if the cursor fails to advance (defensive against a stuck server)", async () => {
    const pull = vi.fn(async () => page({ events: [], cursor: 0, hasMore: true }));

    const { cursor, affected, truncated } = await drainSync(pull, 0, 4);

    expect(pull).toHaveBeenCalledTimes(1);
    expect(cursor).toBe(0);
    expect(affected).toEqual([]);
    expect(truncated).toBe(false);
  });

  it("surfaces retention truncation so callers can refetch wholesale", async () => {
    const pull = vi.fn(async () =>
      page({
        events: [{ seq: 501, type: "workfeed.created" }],
        cursor: 501,
        truncated: true,
      }),
    );

    const { truncated, affected } = await drainSync(pull, 200);

    expect(truncated).toBe(true);
    expect(affected).toEqual(["workfeed"]);
  });
});

describe("sync cursor persistence", () => {
  it("returns undefined before anything is stored, then round-trips", () => {
    expect(loadSyncCursor()).toBeUndefined();
    saveSyncCursor(42);
    expect(loadSyncCursor()).toBe(42);
  });
});
