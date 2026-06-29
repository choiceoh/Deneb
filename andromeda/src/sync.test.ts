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
  it("maps every work-feed event type to the workfeed list", () => {
    expect(resourcesForSyncEventType("workfeed.created")).toEqual(["workfeed"]);
    expect(resourcesForSyncEventType("workfeed.updated")).toEqual(["workfeed"]);
    expect(resourcesForSyncEventType("workfeed.action.run")).toEqual(["workfeed"]);
  });

  it("fans calendar changes out to both calendar lists (dashboard + month)", () => {
    expect(resourcesForSyncEventType("calendar.changed")).toEqual(["calendar", "calendar-range"]);
  });

  it("ignores event types with no desktop list resource", () => {
    expect(resourcesForSyncEventType("transcript.appended")).toEqual([]);
    expect(resourcesForSyncEventType("nonsense")).toEqual([]);
  });
});

describe("drainSync", () => {
  it("drains multiple pages and de-duplicates the affected resources", async () => {
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

    const { cursor, affected } = await drainSync(pull, 0, 4);

    expect(pull).toHaveBeenCalledTimes(1);
    expect(cursor).toBe(0);
    expect(affected).toEqual([]);
  });
});

describe("sync cursor persistence", () => {
  it("returns undefined before anything is stored, then round-trips", () => {
    expect(loadSyncCursor()).toBeUndefined();
    saveSyncCursor(42);
    expect(loadSyncCursor()).toBe(42);
  });
});
