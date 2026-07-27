// Maneuver arrows for the HUD.
//
// Why bitmaps and not a font glyph: the G2 font's coverage is undocumented and
// demonstrably incomplete — `▸` renders as literally nothing, which is why the
// list cursor is a plain `>`. Betting the most safety-relevant element on the
// glasses on a glyph that may silently render blank is not a bet worth taking.
//
// `updateImageRawData` takes ENCODED image bytes (PNG/JPEG); the SDK decodes,
// resizes and converts to the panel's 4-bit greyscale itself. Even's own image
// template states that "line art, icons, and QR codes render fine raw" with no
// dithering — an arrow is exactly that case. So a canvas-drawn PNG is both
// simpler than packing pixels and immune to the font question.
//
// The classifier below folds every maneuver onto ONE OF FOUR DIRECTIONS rather
// than trying to have a distinct picture per turnType. That is the right
// reduction for a HUD: what the wearer needs at speed is which way to go, and
// "좌측 횡단보도" is, for that purpose, simply left.

export type ArrowKind = "left" | "right" | "straight" | "uturn" | "goal";

/**
 * maneuverArrow folds a normalized instruction onto a direction.
 *
 * Reads the gateway's fitted text rather than turnType, on purpose: the gateway
 * already resolved unmapped pedestrian codes by lifting the maneuver out of
 * TMap's own sentence, so the text covers cases the code list does not
 * (crossings being the common one on foot).
 *
 * null when there is no direction to draw — 출발 and 경유지 say nothing about
 * where to point, and a wrong arrow is worse than none.
 */
export function maneuverArrow(instruction: string): ArrowKind | null {
  const s = instruction.trim();
  if (!s) return null;
  if (s.includes("목적지") || s.includes("도착")) return "goal";
  // U-turn before left/right: "U턴" contains neither, but some phrasings pair
  // it with a side ("좌측 U턴") and the turn is what matters.
  if (s.includes("유턴") || s.includes("U턴") || s.includes("u턴"))
    return "uturn";
  if (s.includes("좌")) return "left";
  if (s.includes("우")) return "right";
  if (s.includes("직진")) return "straight";
  return null;
}

// ARROW_W/H — the image container's box. 288×144 is the SDK's maximum for an
// image container, and the whole box is used: the arrow takes the left third and
// the distance the rest.
//
// The distance lives in the BITMAP, not in a text container, because
// TextContainerProperty has no font-size field — the panel renders text at one
// size. Both shipping G2 navigation plugins (Navigaze #1, G2 Maps #11) make the
// distance the largest thing on screen, which is only reachable this way. That
// is the single biggest readability difference between their HUD and a text-only
// one.
// 280×140, not the documented maximum of 288×144: the spec's bounds are stated
// as a range and an inclusive-vs-exclusive edge is exactly the kind of thing a
// host rejects silently. Backing off two pixels costs nothing and removes the
// question.
// Square, and the image matches its container exactly — a non-square container
// squashes the glyph when the SDK resizes into it.
export const ARROW_W = 120;
export const ARROW_H = 120;

/** Where the arrow half ends and the distance half begins. */
const ARROW_COL = 72;

/**
 * arrowPng draws one maneuver arrow and returns PNG bytes.
 *
 * Built to the MUTCD's Type C construction (Section 2D.08): the shaft BENDS at
 * 90°, it does not sweep. Earlier revisions curved the whole path, which is why
 * they read as hand-drawn next to the shipping G2 navigation plugins — both
 * Navigaze and G2 Maps use the bent form, and so does every road sign. The head
 * is 2.7× the shaft width, inside the standard's 1.5–1.75× cap-height guidance
 * for the legend it sits beside.
 *
 * Each shape is ONE explicit polygon with hand-placed vertices. Two earlier
 * attempts generated the outline by offsetting a centreline and both
 * self-intersected near the head, erasing the arrowhead entirely on the device;
 * a nine-point polygon cannot do that.
 */
export async function arrowPng(kind: ArrowKind): Promise<Uint8Array> {
  const canvas = document.createElement("canvas");
  canvas.width = ARROW_W;
  canvas.height = ARROW_H;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("2d canvas context unavailable");

  const W = ARROW_W;
  const H = ARROW_H;
  const cx = W / 2;
  const w = W * 0.115; // shaft width, constant (the standard's form)
  const hh = w * 1.35; // half the head width
  const hl = hh * 1.3; // head length
  const bot = H * 0.94;

  ctx.fillStyle = "#000000";
  ctx.fillRect(0, 0, W, H);
  ctx.fillStyle = "#ffffff";
  ctx.strokeStyle = "#ffffff";

  const poly = (pts: Array<[number, number]>) => {
    ctx.beginPath();
    ctx.moveTo(pts[0][0], pts[0][1]);
    for (let i = 1; i < pts.length; i += 1) ctx.lineTo(pts[i][0], pts[i][1]);
    ctx.closePath();
    ctx.fill();
  };

  switch (kind) {
    case "straight": {
      const tipY = H * 0.06;
      const base = tipY + hl;
      poly([
        [cx - w / 2, bot],
        [cx - w / 2, base],
        [cx - hh, base],
        [cx, tipY],
        [cx + hh, base],
        [cx + w / 2, base],
        [cx + w / 2, bot],
      ]);
      break;
    }
    case "left":
    case "right": {
      const s = kind === "left" ? -1 : 1;
      const by = H * 0.4; // bend height, shaft centre
      const tipX = cx + s * (W * 0.44);
      const hbx = tipX - s * hl; // head base
      poly([
        [cx - (s * w) / 2, bot],
        [cx - (s * w) / 2, by - w / 2],
        [hbx, by - w / 2],
        [hbx, by - hh],
        [tipX, by],
        [hbx, by + hh],
        [hbx, by + w / 2],
        [cx + (s * w) / 2, by + w / 2],
        [cx + (s * w) / 2, bot],
      ]);
      break;
    }
    case "uturn": {
      // Outer and inner arcs walked in opposite directions close into one ring;
      // the head hangs off the left leg pointing DOWN, because a u-turn brings
      // you back toward yourself.
      const r = W * 0.2;
      const top = H * 0.3;
      const rx = cx + r;
      const lx = cx - r;
      const headBase = H * 0.7;
      const pts: Array<[number, number]> = [
        [rx + w / 2, bot],
        [rx + w / 2, top],
      ];
      for (let a = 0; a <= 180; a += 3) {
        const t = (a * Math.PI) / 180;
        pts.push([
          cx + Math.cos(t) * (r + w / 2),
          top - Math.sin(t) * (r + w / 2),
        ]);
      }
      pts.push(
        [lx - w / 2, top],
        [lx - w / 2, headBase],
        [lx - hh, headBase],
        [lx, H * 0.94],
        [lx + hh, headBase],
        [lx + w / 2, headBase],
        [lx + w / 2, top],
      );
      for (let a = 180; a >= 0; a -= 3) {
        const t = (a * Math.PI) / 180;
        pts.push([
          cx + Math.cos(t) * (r - w / 2),
          top - Math.sin(t) * (r - w / 2),
        ]);
      }
      pts.push([rx - w / 2, top], [rx - w / 2, bot]);
      poly(pts);
      break;
    }
    case "goal": {
      // A ring reads as "here" without implying a direction. Stroke matched to
      // the shaft width so it carries the same weight as the arrows.
      const o = W * 0.3;
      ctx.lineWidth = w;
      ctx.beginPath();
      ctx.arc(cx, H / 2, o, 0, Math.PI * 2);
      ctx.stroke();
      ctx.beginPath();
      ctx.arc(cx, H / 2, o * 0.34, 0, Math.PI * 2);
      ctx.fill();
      break;
    }
  }

  const blob: Blob = await new Promise((resolve, reject) => {
    canvas.toBlob(
      (b) => (b ? resolve(b) : reject(new Error("toBlob failed"))),
      "image/png",
    );
  });
  return new Uint8Array(await blob.arrayBuffer());
}

/** SPEED_W/H — the current-speed bitmap, right of the maneuver box. */
export const SPEED_W = 200;
export const SPEED_H = 120;

/**
 * kmhFromMs converts the SDK's speed to km/h.
 *
 * `AppLocation.speed` is undocumented in the SDK, but every platform location
 * API underneath it (Android `Location.getSpeed`, iOS `CLLocation.speed`)
 * reports metres per second, so that is the assumption. Negative means "no
 * fix" on both platforms and must not render as 0 — a confident zero while
 * moving is worse than showing nothing.
 */
export function kmhFromMs(speed: number | undefined): number | null {
  if (speed == null || !Number.isFinite(speed) || speed < 0) return null;
  return Math.round(speed * 3.6);
}

/**
 * speedPng draws the current speed big, the way both shipping G2 navigation
 * plugins do. Separate from the maneuver bitmap on purpose: speed changes on
 * every position fix while the maneuver changes every few minutes, and
 * redrawing the arrow at fix rate would spend BLE bandwidth for nothing.
 */
export async function speedPng(kmh: number): Promise<Uint8Array> {
  const canvas = document.createElement("canvas");
  canvas.width = SPEED_W;
  canvas.height = SPEED_H;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("2d canvas context unavailable");
  ctx.fillStyle = "#000000";
  ctx.fillRect(0, 0, SPEED_W, SPEED_H);
  ctx.fillStyle = "#ffffff";
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  ctx.font = "bold 82px sans-serif";
  ctx.fillText(String(kmh), SPEED_W / 2, SPEED_H / 2 - 8);
  ctx.font = "bold 26px sans-serif";
  ctx.fillText("km/h", SPEED_W / 2, SPEED_H - 20);
  const blob: Blob = await new Promise((resolve, reject) => {
    canvas.toBlob(
      (b) => (b ? resolve(b) : reject(new Error("toBlob failed"))),
      "image/png",
    );
  });
  return new Uint8Array(await blob.arrayBuffer());
}

/**
 * arrowText — the direction as characters, for when the bitmap cannot be sent.
 *
 * Measured on the device: `updateImageRawData` returns `sendFailed` for a single
 * 2.4 KB PNG, paced four seconds apart and retried. Halving the image did not
 * help and neither did dropping the second one, so it is the image transport
 * itself and not the size, the rate, or the encoding. Navigation needs a
 * direction indicator today.
 *
 * Deliberately ASCII. The G2 font's coverage is unknown and demonstrably partial
 * (`▸` renders as nothing), so a Unicode arrow risks a blank where the most
 * safety-relevant element belongs. `<` and `>` are already proven on this
 * display — the list cursor uses one.
 */
export function arrowText(kind: ArrowKind): string {
  switch (kind) {
    case "left":
      return "<<<";
    case "right":
      return ">>>";
    case "straight":
      return "^^^";
    case "uturn":
      return "U-turn";
    case "goal":
      return "[O]";
  }
}

/** DIST_W/H — the distance bitmap, right of the arrow. */
export const DIST_W = 280;
export const DIST_H = 140;

/**
 * distancePng draws the remaining distance as large as the box allows.
 *
 * A bitmap because TextContainerProperty has no font-size field: on this panel
 * every text container renders at one size, so the number that the wearer
 * actually acts on could never be made to dominate. Both shipping G2 navigation
 * plugins make it the largest element, and this is the only way to match that.
 *
 * The size is searched rather than fixed so "80m" fills the box and "1.2km"
 * still fits without wrapping.
 */
export async function distancePng(text: string): Promise<Uint8Array> {
  const canvas = document.createElement("canvas");
  canvas.width = DIST_W;
  canvas.height = DIST_H;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("2d canvas context unavailable");
  ctx.fillStyle = "#000000";
  ctx.fillRect(0, 0, DIST_W, DIST_H);
  ctx.fillStyle = "#ffffff";
  ctx.textAlign = "left";
  ctx.textBaseline = "middle";

  let size = DIST_H;
  for (; size > 24; size -= 2) {
    ctx.font = `bold ${size}px sans-serif`;
    if (ctx.measureText(text).width <= DIST_W - 8) break;
  }
  ctx.font = `bold ${size}px sans-serif`;
  ctx.fillText(text, 4, DIST_H / 2);

  const blob: Blob = await new Promise((resolve, reject) => {
    canvas.toBlob(
      (b) => (b ? resolve(b) : reject(new Error("toBlob failed"))),
      "image/png",
    );
  });
  return new Uint8Array(await blob.arrayBuffer());
}
