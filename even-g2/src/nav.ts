import { HUD_LINES } from "./refresh";
import type { GlanceSettings } from "./settings";

// Turn-by-turn policy for the glasses — pure, so it is fully testable without
// a TMap key, a GPS fix, or hardware.
//
// The gateway resolves the destination and normalizes the route; everything
// here is the part that must keep working when the network does not. Once a
// route is in hand the glasses need no further calls, which matters on foot in
// a stairwell or underground.
//
// The one non-obvious rule is MONOTONIC ADVANCE. A GPS fix in a Korean city
// scatters by 10–20m and much worse between buildings, so "show the nearest
// maneuver" makes the instruction flip back and forth while the wearer stands
// still — the single worst failure mode for a display three centimetres from
// someone's eye. Step index therefore only ever increases.

export interface NavCoord {
  lat: number;
  lon: number;
}

export interface NavStep {
  /** Fitted form — "190m 앞 좌회전". Always present. */
  short: string;
  /** TMap's own sentence, may be long or empty. */
  full: string;
  /** Metres from the previous maneuver, as planned. */
  distanceM: number;
  turnType: number;
  coord: NavCoord;
}

export interface NavRoute {
  steps: NavStep[];
  totalM: number;
  totalSec: number;
  mode: string;
}

export interface NavState {
  /** Index of the maneuver the wearer is currently heading toward. */
  stepIndex: number;
  arrived: boolean;
}

/**
 * ARRIVE_RADIUS_M — how close counts as "reached this maneuver".
 *
 * Sized above typical urban GPS scatter (10–20m): tighter and a wearer who
 * actually made the turn keeps being told to make it, which is worse than
 * advancing a beat early — they can see the junction, the HUD only reminds.
 */
export const ARRIVE_RADIUS_M = 25;

/**
 * DESTINATION_RADIUS_M — the final step is more forgiving. Overshooting the
 * destination and being told to keep walking is a distinctly annoying way to
 * end a route, and by then the wearer is looking at the building, not the HUD.
 */
export const DESTINATION_RADIUS_M = 40;

/** Fresh state for a newly fetched route, before any position is known. */
export function initialNavState(): NavState {
  return { stepIndex: 0, arrived: false };
}

const EARTH_RADIUS_M = 6_371_000;

/**
 * distanceM — great-circle metres between two fixes.
 *
 * Haversine rather than a flat approximation: the flat version is cheaper and
 * fine at Korean latitudes, but it needs a cos(lat) term that is easy to omit
 * and the resulting error is a silent 20% at 37°N — exactly the kind of bug
 * that never shows up in a unit test written by whoever omitted it.
 */
export function distanceM(a: NavCoord, b: NavCoord): number {
  const toRad = (d: number) => (d * Math.PI) / 180;
  const dLat = toRad(b.lat - a.lat);
  const dLon = toRad(b.lon - a.lon);
  const lat1 = toRad(a.lat);
  const lat2 = toRad(b.lat);
  const h =
    Math.sin(dLat / 2) ** 2 +
    Math.cos(lat1) * Math.cos(lat2) * Math.sin(dLon / 2) ** 2;
  return Math.round(2 * EARTH_RADIUS_M * Math.asin(Math.min(1, Math.sqrt(h))));
}

/**
 * advanceNav decides which maneuver is current after a new position fix.
 *
 * Scans FORWARD for the furthest maneuver the wearer is standing on, then
 * points at the one after it. Only checking the current step looks simpler and
 * wedges: lose signal through a junction — a tunnel, an underground passage,
 * a covered market — and you surface two maneuvers along, never having been
 * seen at the one the state machine is waiting for. The HUD would then spend
 * the rest of the route naming a turn that is already behind you.
 *
 * Scanning forward only is what keeps that from becoming a licence to jump
 * around: the index never decreases, so GPS scatter cannot re-issue a
 * completed turn.
 */
export function advanceNav(
  route: NavRoute,
  state: NavState,
  pos: NavCoord,
): NavState {
  if (state.arrived || route.steps.length === 0) return state;

  const last = route.steps.length - 1;
  const from = Math.min(state.stepIndex, last);

  // Destination first: standing at the destination means arrived, whatever
  // maneuvers were missed getting there. Being told to turn left when you are
  // already at the door is worse than losing the record of a skipped step.
  if (distanceM(pos, route.steps[last].coord) <= DESTINATION_RADIUS_M) {
    return { stepIndex: last, arrived: true };
  }

  let reached = -1;
  for (let i = from; i < last; i += 1) {
    if (distanceM(pos, route.steps[i].coord) <= ARRIVE_RADIUS_M) reached = i;
  }
  if (reached < 0) return state;

  const index = Math.min(reached + 1, last);
  if (index === state.stepIndex) return state;
  return { stepIndex: index, arrived: false };
}

/** remainingM is the straight-line distance to the current maneuver. */
export function remainingM(
  route: NavRoute,
  state: NavState,
  pos: NavCoord,
): number {
  const step = route.steps[Math.min(state.stepIndex, route.steps.length - 1)];
  if (!step) return 0;
  return distanceM(pos, step.coord);
}

/** formatMetres mirrors the gateway's own distance wording. */
export function formatMetres(m: number): string {
  if (m <= 0) return "";
  if (m < 1000) return `${m}m`;
  return `${(m / 1000).toFixed(1).replace(/\.0$/, "")}km`;
}

/** How long a route request may take before the wearer is told it failed. */
const ROUTE_TIMEOUT_MS = 12_000;

export interface RouteResult {
  route: NavRoute;
  destination: { name: string; address: string };
}

/**
 * fetchRoute asks the gateway to resolve a destination and plan a route.
 *
 * One call per navigation, not per fix: everything after this happens on the
 * glasses, so losing the network mid-route costs nothing.
 */
export async function fetchRoute(
  settings: GlanceSettings,
  from: NavCoord,
  to: string,
  mode: "walk" | "car",
): Promise<RouteResult> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), ROUTE_TIMEOUT_MS);
  try {
    const res = await fetch(`${settings.baseUrl}/api/even/route`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${settings.token}`,
        "Content-Type": "application/json",
        Accept: "application/json",
      },
      body: JSON.stringify({ from, to, mode }),
      signal: controller.signal,
    });
    if (!res.ok) {
      // The gateway distinguishes "no key configured" (503) from a routing
      // failure, and so must the glass — one of them the wearer can do
      // nothing about.
      const detail = (await res.json().catch(() => null)) as {
        error?: { message?: string };
      } | null;
      throw new Error(detail?.error?.message || `HTTP ${res.status}`);
    }
    const data = (await res.json()) as {
      route?: NavRoute;
      destination?: { name?: string; address?: string };
    };
    if (!data.route || !Array.isArray(data.route.steps)) {
      throw new Error("경로를 받지 못했습니다");
    }
    return {
      route: data.route,
      destination: {
        name: data.destination?.name ?? "",
        address: data.destination?.address ?? "",
      },
    };
  } catch (err) {
    if (err instanceof Error && err.name === "AbortError") {
      throw new Error("시간 초과");
    }
    throw err;
  } finally {
    clearTimeout(timer);
  }
}

/**
 * GLYPH_PROBE — candidate arrow glyphs, grouped one row per family.
 *
 * The G2 font does NOT cover everything: `▸` renders as literally nothing,
 * which is why the list cursor is a plain `>`. Guessing a second time is not
 * worth it — one look at the real glasses settles which families exist, and a
 * blank row in this probe is the answer for that whole family.
 *
 * ASCII is last on purpose: it is the known-good baseline, so if row 6 is also
 * blank the probe itself is broken rather than the font.
 */
export const GLYPH_PROBE: Array<{ label: string; glyphs: string }> = [
  { label: "1 기본", glyphs: "\u2190 \u2192 \u2191 \u2193" },
  { label: "2 회전", glyphs: "\u21b0 \u21b1 \u2934 \u2935" },
  { label: "3 삼각", glyphs: "\u25c0 \u25b6 \u25b2 \u25bc" },
  { label: "4 굵은", glyphs: "\u2b05 \u27a1 \u2b06 \u2b07" },
  { label: "5 유턴", glyphs: "\u2936 \u2937 \u21a9 \u21aa" },
  { label: "6 아스키", glyphs: "< > ^ v" },
];

/** glyphProbeLines renders the probe within the HUD line budget. */
export function glyphProbeLines(): string[] {
  const rows = GLYPH_PROBE.map((r) => `${r.label}  ${r.glyphs}`);
  return ["글리프 확인 — 빈 줄=미지원", ...rows].slice(0, HUD_LINES);
}

/**
 * initialNavStateAt is the state a fresh route should OPEN in, given where the
 * wearer is standing right now.
 *
 * Not the same as initialNavState(): step 0 is 출발, the place they are already
 * at. Opening there points the HUD at a completed maneuver and — since 출발 has
 * no direction — leaves the arrow blank until some later fix happens to arrive.
 * This exists as a named function because the bug was not in the advance rule
 * (that was correct) but in a caller forgetting to apply it, which a test of
 * advanceNav alone cannot catch.
 */
export function initialNavStateAt(route: NavRoute, pos: NavCoord): NavState {
  return advanceNav(route, initialNavState(), pos);
}

/**
 * remainingSummary — "4.2km · 8분", the trip-level line both shipping G2
 * navigation plugins carry along the bottom.
 *
 * Sums the planned distance of every maneuver still ahead and scales the route's
 * total time by the fraction left, rather than asking the server again: the
 * glasses hold the route and must keep working with no network.
 */
export function remainingSummary(route: NavRoute, state: NavState): string {
  if (route.steps.length === 0) return "";
  if (state.arrived) return "도착";
  let metres = 0;
  for (let i = state.stepIndex; i < route.steps.length; i += 1) {
    metres += route.steps[i].distanceM;
  }
  const parts = [formatMetres(metres) || formatMetres(route.totalM)];
  if (route.totalSec > 0 && route.totalM > 0) {
    const secs = Math.round(
      route.totalSec * Math.min(1, metres / route.totalM),
    );
    const mins = Math.max(1, Math.round(secs / 60));
    parts.push(
      mins >= 60 ? `${Math.floor(mins / 60)}시간 ${mins % 60}분` : `${mins}분`,
    );
  }
  return parts.filter(Boolean).join(" · ");
}

/**
 * navTextLines — what belongs in the TEXT container when the bitmap is carrying
 * the arrow and the distance.
 *
 * The distance is deliberately absent: it is already the largest thing on the
 * panel, drawn into the image. Repeating it here is the two-numbers problem
 * again, one line lower.
 */
export function navTextLines(route: NavRoute, state: NavState): string[] {
  if (route.steps.length === 0) return ["경로 없음"];
  if (state.arrived) return ["도착", "", "탭=종료"];
  const idx = Math.min(state.stepIndex, route.steps.length - 1);
  // Numbers and symbols, no prose (operator's call, 2026-07-26).
  //
  // The arrow and the distance are already the two largest things on the panel,
  // drawn into the bitmap. TMap's sentence was a third element competing for the
  // same glance and it is the one a wearer at speed reads last, so it is gone.
  // What stays is countable: how far is left, how long, where in the route.
  const lines: string[] = [];
  const summary = remainingSummary(route, state);
  if (summary) lines.push(summary);
  lines.push(`${idx + 1}/${route.steps.length}`);
  lines.push("");
  lines.push("탭=중지");
  return lines.slice(0, HUD_LINES);
}
