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

/** Fresh state for a newly fetched route. */
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

/**
 * liveInstruction rewrites the maneuver with the distance actually left.
 *
 * The gateway's Short carries the PLANNED distance ("190m 앞 좌회전"), which is
 * right when the step begins and a lie thirty seconds later. On a HUD the
 * number is the part the wearer acts on, so it has to track reality.
 */
export function liveInstruction(step: NavStep, metresLeft: number): string {
  const bare = step.short.replace(/^[0-9.]+(?:m|km)\s*(?:앞\s*)?/, "");
  if (bare === "출발" || bare === "목적지" || bare === "경유지") return bare;
  const d = formatMetres(metresLeft);
  if (!d) return bare;
  return bare === "직진" ? `${d} 직진` : `${d} 앞 ${bare}`;
}

/** formatMetres mirrors the gateway's own distance wording. */
export function formatMetres(m: number): string {
  if (m <= 0) return "";
  if (m < 1000) return `${m}m`;
  return `${(m / 1000).toFixed(1).replace(/\.0$/, "")}km`;
}

/**
 * navLines renders the HUD for the current fix.
 *
 * Deliberately sparse. Ten lines are available but a navigating wearer is
 * looking at the road, so the instruction gets the top line alone and
 * everything else is subordinate — a dense screen here is not more information,
 * it is a longer glance away from traffic.
 */
export function navLines(
  route: NavRoute,
  state: NavState,
  pos: NavCoord,
  opts?: { arrow?: boolean },
): string[] {
  if (route.steps.length === 0) return ["경로 없음"];
  if (state.arrived) return ["도착했습니다"];

  const idx = Math.min(state.stepIndex, route.steps.length - 1);
  const step = route.steps[idx];
  const left = distanceM(pos, step.coord);

  // With an arrow on screen the direction is already shown, so the text drops
  // to the distance alone. This also removes a real hazard: the instruction and
  // TMap's sentence carry DIFFERENT distances (metres to the maneuver vs metres
  // travelled after it), and two competing numbers three centimetres from the
  // eye is a misread waiting to happen.
  const lines = opts?.arrow
    ? [formatMetres(left) || liveInstruction(step, left)]
    : [liveInstruction(step, left)];

  // The sentence only earns a line when it says more than the fitted form —
  // TMap often repeats "목적지" or "출발" verbatim. Its trailing travel clause
  // is dropped with an arrow present, for the same two-numbers reason.
  const sentence = opts?.arrow ? stripTrailingDistance(step.full) : step.full;
  if (
    sentence &&
    sentence !== step.short &&
    sentence !== liveInstruction(step, left)
  ) {
    lines.push(sentence);
  }

  const next = route.steps[idx + 1];
  if (next) lines.push(`다음: ${next.short}`);

  lines.push(
    `${idx + 1}/${route.steps.length} · 총 ${formatMetres(route.totalM)}`,
  );
  return lines.slice(0, HUD_LINES);
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
 * stripTrailingDistance removes TMap's "…N m 이동" travel clause.
 *
 * That clause measures distance AFTER the maneuver, while the headline shows
 * distance TO it. Both are true and they are different numbers; with an arrow
 * carrying the direction, the sentence is only there for the street and
 * landmark, so the number is noise.
 */
export function stripTrailingDistance(full: string): string {
  return full
    .replace(/\s*[0-9.]+\s*(?:m|km)\s*이동\s*$/, "")
    .replace(/\s*(?:을|를)?\s*따라\s*$/, "")
    .trim();
}
