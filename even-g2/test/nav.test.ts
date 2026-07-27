import { describe, expect, it } from "vitest";
import {
  ARRIVE_RADIUS_M,
  advanceNav,
  distanceM,
  formatMetres,
  initialNavState,
  initialNavStateAt,
  navTextLines,
  remainingSummary,
  fetchRoute,
  remainingM,
  type NavRoute,
} from "../src/nav";

// Points along 소공로 in central Seoul, roughly 100m apart, so the numbers
// below are the real thing rather than synthetic degrees.
const P0 = { lat: 37.5636, lon: 126.9784 };
const P1 = { lat: 37.5645, lon: 126.9784 }; // ~100m north of P0
const P2 = { lat: 37.5654, lon: 126.9784 }; // ~100m north of P1

const route: NavRoute = {
  totalM: 200,
  totalSec: 150,
  mode: "walk",
  steps: [
    { short: "출발", full: "출발", distanceM: 0, turnType: 200, coord: P0 },
    {
      short: "100m 앞 좌회전",
      full: "소공로을 따라 100m 이동",
      distanceM: 100,
      turnType: 12,
      coord: P1,
    },
    {
      short: "목적지",
      full: "목적지",
      distanceM: 100,
      turnType: 201,
      coord: P2,
    },
  ],
};

describe("distanceM", () => {
  it("measures real-world metres", () => {
    // 0.0009° of latitude ≈ 100m; a flat approximation without the cos(lat)
    // term would be visibly off here.
    expect(distanceM(P0, P1)).toBeGreaterThan(90);
    expect(distanceM(P0, P1)).toBeLessThan(110);
  });

  it("is zero for the same point and symmetric", () => {
    expect(distanceM(P0, P0)).toBe(0);
    expect(distanceM(P0, P2)).toBe(distanceM(P2, P0));
  });
});

describe("advanceNav", () => {
  it("advances when the maneuver is reached", () => {
    const next = advanceNav(route, initialNavState(), P0);
    expect(next.stepIndex).toBe(1);
  });

  it("holds position while still far from the maneuver", () => {
    const state = { stepIndex: 1, arrived: false };
    expect(advanceNav(route, state, P0).stepIndex).toBe(1);
  });

  it("never regresses when GPS jitters backwards", () => {
    // The failure this rule exists for: a fix that lands behind the wearer
    // must not re-issue a turn they already made. On a display this close to
    // the eye, an instruction that flips back and forth is the worst outcome.
    const state = { stepIndex: 2, arrived: false };
    const jittered = advanceNav(route, state, P0);
    expect(jittered.stepIndex).toBe(2);
  });

  it("arrives when the fix lands at the destination, whatever was missed", () => {
    // Surfacing from a tunnel at the door is arrival. Insisting on the skipped
    // maneuver first would tell a wearer standing at the destination to turn.
    const nearDest = { lat: P2.lat - 0.0001, lon: P2.lon };
    expect(advanceNav(route, initialNavState(), nearDest).arrived).toBe(true);
  });

  it("re-syncs forward after a signal gap instead of wedging", () => {
    // The bug this covers: checking only the current step means a wearer who
    // was never seen at step 1 stays pinned to step 0 for the whole route,
    // with the HUD naming a turn already behind them.
    const long: NavRoute = {
      totalM: 500,
      totalSec: 400,
      mode: "walk",
      steps: [
        { short: "출발", full: "", distanceM: 0, turnType: 200, coord: P0 },
        {
          short: "100m 앞 좌회전",
          full: "",
          distanceM: 100,
          turnType: 12,
          coord: P1,
        },
        {
          short: "100m 앞 우회전",
          full: "",
          distanceM: 100,
          turnType: 13,
          coord: P2,
        },
        {
          short: "목적지",
          full: "",
          distanceM: 100,
          turnType: 201,
          coord: { lat: 37.57, lon: 126.9784 },
        },
      ],
    };
    // Standing on step 2 having never been seen at steps 0 or 1.
    const next = advanceNav(long, initialNavState(), P2);
    expect(next.stepIndex).toBe(3);
    expect(next.arrived).toBe(false);
  });

  it("arrives at the destination", () => {
    const next = advanceNav(route, { stepIndex: 2, arrived: false }, P2);
    expect(next.arrived).toBe(true);
  });

  it("stays arrived", () => {
    const done = { stepIndex: 2, arrived: true };
    expect(advanceNav(route, done, P0)).toBe(done);
  });

  it("survives an empty route", () => {
    const empty: NavRoute = { steps: [], totalM: 0, totalSec: 0, mode: "walk" };
    expect(advanceNav(empty, initialNavState(), P0).stepIndex).toBe(0);
  });

  it("advances exactly at the radius boundary", () => {
    const state = { stepIndex: 1, arrived: false };
    // Construct a fix just inside ARRIVE_RADIUS_M of P1.
    const justInside = {
      lat: P1.lat - (ARRIVE_RADIUS_M - 5) / 111_320,
      lon: P1.lon,
    };
    expect(distanceM(justInside, P1)).toBeLessThanOrEqual(ARRIVE_RADIUS_M);
    expect(advanceNav(route, state, justInside).stepIndex).toBe(2);
  });
});

describe("formatMetres", () => {
  it("formats the way the gateway does", () => {
    expect(formatMetres(0)).toBe("");
    expect(formatMetres(999)).toBe("999m");
    expect(formatMetres(1000)).toBe("1km");
    expect(formatMetres(1500)).toBe("1.5km");
  });
});

describe("remainingM", () => {
  it("measures to the current maneuver", () => {
    expect(remainingM(route, { stepIndex: 1, arrived: false }, P0)).toBe(
      distanceM(P0, P1),
    );
  });

  it("clamps a stale index instead of throwing", () => {
    expect(remainingM(route, { stepIndex: 99, arrived: false }, P0)).toBe(
      distanceM(P0, P2),
    );
  });
});

describe("fetchRoute", () => {
  const settings = { baseUrl: "http://gw", token: "tok" };
  const okBody = {
    route: { steps: route.steps, totalM: 200, totalSec: 150, mode: "walk" },
    destination: { name: "강남역", address: "서울 강남구" },
  };

  function stubFetch(status: number, body: unknown) {
    globalThis.fetch = (async () =>
      ({
        ok: status === 200,
        status,
        json: async () => body,
      }) as unknown as Response) as typeof fetch;
  }

  it("sends the bearer and returns the route", async () => {
    let seen: RequestInit | undefined;
    globalThis.fetch = (async (_url: string, init: RequestInit) => {
      seen = init;
      return {
        ok: true,
        status: 200,
        json: async () => okBody,
      } as unknown as Response;
    }) as unknown as typeof fetch;

    const got = await fetchRoute(settings, P0, "강남역", "walk");
    expect(got.destination.name).toBe("강남역");
    expect(got.route.steps).toHaveLength(3);
    expect((seen?.headers as Record<string, string>).Authorization).toBe(
      "Bearer tok",
    );
  });

  it("surfaces the gateway's message so 'no key' stays distinguishable", async () => {
    // 503 with a named key is the operator's problem; a generic failure is not.
    // Collapsing them into one message would send the wearer looking for a
    // different destination when the fix is a config line.
    stubFetch(503, {
      error: { message: "길안내가 설정되지 않았습니다 (TMAP_APP_KEY)" },
    });
    await expect(fetchRoute(settings, P0, "강남역", "walk")).rejects.toThrow(
      /TMAP_APP_KEY/,
    );
  });

  it("falls back to the status code when there is no message", async () => {
    stubFetch(502, null);
    await expect(fetchRoute(settings, P0, "강남역", "walk")).rejects.toThrow(
      /502/,
    );
  });

  it("rejects a malformed body rather than rendering a blank route", async () => {
    stubFetch(200, { destination: { name: "x" } });
    await expect(fetchRoute(settings, P0, "강남역", "walk")).rejects.toThrow(
      /경로/,
    );
  });
});

describe("initialNavStateAt", () => {
  it("opens past 출발 even when the route origin is far from the fix", () => {
    // Observed on the device as image:arrow=skipped:출발 with the text stuck at
    // 1/7: TMap snaps the origin to the road, so the start point is routinely
    // outside ARRIVE_RADIUS_M and a distance-based advance never fires.
    const farFromStart = { lat: 37.56, lon: 126.97 };
    expect(initialNavStateAt(route, farFromStart).stepIndex).toBeGreaterThan(0);
  });

  it("opens on the first real maneuver, not on 출발", () => {
    // The defect this pins: opening at step 0 points the HUD at 출발 — where
    // the wearer already stands — and because 출발 carries no direction the
    // arrow stayed blank until a later fix arrived. Reported from the device as
    // "화살표가 안떠".
    const state = initialNavStateAt(route, P0);
    expect(state.stepIndex).toBe(1);
    expect(state.arrived).toBe(false);
    expect(route.steps[state.stepIndex].short).toContain("좌회전");
  });

  it("still arrives when the route starts at the destination", () => {
    const state = initialNavStateAt(route, P2);
    expect(state.arrived).toBe(true);
  });
});

describe("remainingSummary / navTextLines", () => {
  it("sums the maneuvers still ahead and scales the time", () => {
    // route: 출발(0) → 좌회전(100) → 목적지(100), total 200m/150s
    const s = remainingSummary(route, { stepIndex: 1, arrived: false });
    expect(s).toMatch(/^200m · \d+분$/);
  });

  it("shrinks as the route is consumed", () => {
    const early = remainingSummary(route, { stepIndex: 1, arrived: false });
    const late = remainingSummary(route, { stepIndex: 2, arrived: false });
    expect(early).not.toBe(late);
  });

  it("reports 도착 once arrived", () => {
    expect(remainingSummary(route, { stepIndex: 2, arrived: true })).toBe(
      "도착",
    );
  });

  it("carries numbers only — no prose (operator's call)", () => {
    // The arrow and distance are already the two biggest things on the panel;
    // TMap's sentence was a third element competing for the same glance, and it
    // is the one read last at speed.
    const lines = navTextLines(route, { stepIndex: 1, arrived: false });
    expect(lines.join("\n")).not.toContain("소공로");
    expect(lines.join("\n")).not.toContain("따라");
    expect(lines[0]).toMatch(/^\d/);
    expect(lines).toContain("2/3");
  });

  it("keeps the tap hint and stays within the line budget", () => {
    const lines = navTextLines(route, { stepIndex: 1, arrived: false });
    expect(lines).toContain("탭=중지");
    expect(lines.length).toBeLessThanOrEqual(10);
  });
});
