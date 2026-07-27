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
export const ARROW_W = 176;
export const ARROW_H = 96;

/** Where the arrow half ends and the distance half begins. */
const ARROW_COL = 72;

/**
 * arrowPng draws one maneuver arrow and returns PNG bytes.
 *
 * Deliberately fat strokes and no anti-aliasing niceties: the panel has 16
 * shades and is read in sunlight at a glance, so a thin elegant arrow is a grey
 * smudge. White on black matches the panel's native rendering (green on black).
 */
export async function arrowPng(
  kind: ArrowKind,
  distance?: string,
): Promise<Uint8Array> {
  const canvas = document.createElement("canvas");
  canvas.width = ARROW_W;
  canvas.height = ARROW_H;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("2d canvas context unavailable");

  ctx.fillStyle = "#000000";
  ctx.fillRect(0, 0, ARROW_W, ARROW_H);
  ctx.strokeStyle = "#ffffff";
  ctx.fillStyle = "#ffffff";
  ctx.lineWidth = 14;
  ctx.lineCap = "butt";
  ctx.lineJoin = "miter";

  // Arrow on the LEFT, distance to its right — the layout both shipping plugins
  // use. Reads in the same order as the sentence below it.
  const cx = ARROW_COL / 2;
  const head = (x: number, y: number, dir: ArrowKind) => {
    const s = 34;
    ctx.beginPath();
    if (dir === "left") {
      ctx.moveTo(x - s, y);
      ctx.lineTo(x + s * 0.2, y - s * 0.8);
      ctx.lineTo(x + s * 0.2, y + s * 0.8);
    } else if (dir === "right") {
      ctx.moveTo(x + s, y);
      ctx.lineTo(x - s * 0.2, y - s * 0.8);
      ctx.lineTo(x - s * 0.2, y + s * 0.8);
    } else {
      ctx.moveTo(x, y - s);
      ctx.lineTo(x - s * 0.8, y + s * 0.2);
      ctx.lineTo(x + s * 0.8, y + s * 0.2);
    }
    ctx.closePath();
    ctx.fill();
  };

  switch (kind) {
    case "straight": {
      ctx.beginPath();
      ctx.moveTo(cx, ARROW_H - 12);
      ctx.lineTo(cx, 44);
      ctx.stroke();
      head(cx, 34, "straight");
      break;
    }
    case "left":
    case "right": {
      // An L, not a diagonal: the stem shows you are travelling forward and the
      // bend shows where the turn happens, which reads faster than a slash.
      const bendY = 44;
      const tipX = kind === "left" ? 30 : ARROW_W - 30;
      ctx.beginPath();
      ctx.moveTo(cx, ARROW_H - 12);
      ctx.lineTo(cx, bendY);
      ctx.lineTo(tipX, bendY);
      ctx.stroke();
      head(tipX, bendY, kind);
      break;
    }
    case "uturn": {
      ctx.beginPath();
      ctx.moveTo(cx + 34, ARROW_H - 12);
      ctx.lineTo(cx + 34, 60);
      ctx.arc(cx, 60, 34, 0, Math.PI, true);
      ctx.lineTo(cx - 34, ARROW_H - 46);
      ctx.stroke();
      // Head points DOWN — you come back toward yourself. Drawn directly
      // rather than drawn-then-erased: clearRect punches transparency, not
      // black, and the SDK's greyscale conversion has no defined behaviour for
      // an alpha hole.
      ctx.beginPath();
      ctx.moveTo(cx - 34, ARROW_H - 8);
      ctx.lineTo(cx - 34 - 27, ARROW_H - 46);
      ctx.lineTo(cx - 34 + 27, ARROW_H - 46);
      ctx.closePath();
      ctx.fill();
      break;
    }
    case "goal": {
      // A filled ring reads as "here" without implying a direction.
      ctx.beginPath();
      ctx.arc(cx, ARROW_H / 2, 40, 0, Math.PI * 2);
      ctx.lineWidth = 18;
      ctx.stroke();
      ctx.beginPath();
      ctx.arc(cx, ARROW_H / 2, 12, 0, Math.PI * 2);
      ctx.fill();
      break;
    }
  }

  const d = (distance ?? "").trim();
  if (d) {
    // Sized to fill the remaining width; shrink only when it would overflow, so
    // "80m" is as large as the box allows and "1.5km" still fits.
    const boxW = ARROW_W - ARROW_COL - 8;
    ctx.fillStyle = "#ffffff";
    ctx.textAlign = "left";
    ctx.textBaseline = "middle";
    let size = 76;
    do {
      ctx.font = `bold ${size}px sans-serif`;
      if (ctx.measureText(d).width <= boxW) break;
      size -= 4;
    } while (size > 28);
    ctx.fillText(d, ARROW_COL + 4, ARROW_H / 2);
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
