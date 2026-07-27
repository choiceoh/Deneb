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

// SQUARE, and the image matches its container exactly.
//
// It was 176×96 with the arrow crammed into the left 72px — that space was for
// the distance, which moved out to the text line and left the glyph drawn small
// in a corner of a wide, mostly-black bitmap. A non-square container also
// squashes a vertical arrow when the SDK resizes into it. Square container +
// square image + proportional geometry = the shape that was drawn is the shape
// that appears.
//
// 120 sits inside the SDK's 20~288 × 20~144 bounds with room to spare.
export const ARROW_W = 120;
export const ARROW_H = 120;

/**
 * arrowPng draws one maneuver arrow and returns PNG bytes.
 *
 * Deliberately fat strokes and no anti-aliasing niceties: the panel has 16
 * shades and is read in sunlight at a glance, so a thin elegant arrow is a grey
 * smudge. White on black matches the panel's native rendering (green on black).
 */
export async function arrowPng(kind: ArrowKind): Promise<Uint8Array> {
  const canvas = document.createElement("canvas");
  canvas.width = ARROW_W;
  canvas.height = ARROW_H;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("2d canvas context unavailable");

  // Everything below is a fraction of the canvas, so the shape survives any
  // future resize of the box.
  const W = ARROW_W;
  const H = ARROW_H;
  const cx = W / 2;
  const stroke = Math.round(W * 0.15);
  const headHalf = Math.round(W * 0.26); // half-width of the arrowhead base
  const headLen = Math.round(H * 0.26); // tip to base
  const pad = Math.round(W * 0.08);

  ctx.fillStyle = "#000000";
  ctx.fillRect(0, 0, W, H);
  ctx.strokeStyle = "#ffffff";
  ctx.fillStyle = "#ffffff";
  ctx.lineWidth = stroke;
  ctx.lineCap = "butt";
  ctx.lineJoin = "miter";

  /** Filled triangle pointing `dir`, tip at (x, y). */
  const head = (
    x: number,
    y: number,
    dir: "up" | "down" | "left" | "right",
  ) => {
    ctx.beginPath();
    if (dir === "up") {
      ctx.moveTo(x, y);
      ctx.lineTo(x - headHalf, y + headLen);
      ctx.lineTo(x + headHalf, y + headLen);
    } else if (dir === "down") {
      ctx.moveTo(x, y);
      ctx.lineTo(x - headHalf, y - headLen);
      ctx.lineTo(x + headHalf, y - headLen);
    } else if (dir === "left") {
      ctx.moveTo(x, y);
      ctx.lineTo(x + headLen, y - headHalf);
      ctx.lineTo(x + headLen, y + headHalf);
    } else {
      ctx.moveTo(x, y);
      ctx.lineTo(x - headLen, y - headHalf);
      ctx.lineTo(x - headLen, y + headHalf);
    }
    ctx.closePath();
    ctx.fill();
  };

  switch (kind) {
    case "straight": {
      ctx.beginPath();
      ctx.moveTo(cx, H - pad);
      ctx.lineTo(cx, pad + headLen);
      ctx.stroke();
      head(cx, pad, "up");
      break;
    }
    case "left":
    case "right": {
      // An L, not a diagonal: the stem says you are going forward and the bend
      // says where the turn is. Reads faster than a slash at a junction.
      const bendY = Math.round(H * 0.42);
      const dir = kind === "left" ? "left" : "right";
      const tipX = kind === "left" ? pad : W - pad;
      const bendEnd = kind === "left" ? tipX + headLen : tipX - headLen;
      ctx.beginPath();
      ctx.moveTo(cx, H - pad);
      ctx.lineTo(cx, bendY);
      ctx.lineTo(bendEnd, bendY);
      ctx.stroke();
      head(tipX, bendY, dir);
      break;
    }
    case "uturn": {
      // Up the right side, over the top, back down the left — head pointing
      // DOWN, because a u-turn brings you back toward yourself.
      const r = Math.round(W * 0.22);
      const rightX = cx + r;
      const leftX = cx - r;
      const topY = Math.round(H * 0.3);
      ctx.beginPath();
      ctx.moveTo(rightX, H - pad);
      ctx.lineTo(rightX, topY);
      ctx.arc(cx, topY, r, 0, Math.PI, true);
      ctx.lineTo(leftX, H - pad - headLen);
      ctx.stroke();
      head(leftX, H - pad, "down");
      break;
    }
    case "goal": {
      // A ring reads as "here" without implying a direction.
      const outer = Math.round(W * 0.3);
      ctx.beginPath();
      ctx.arc(cx, H / 2, outer, 0, Math.PI * 2);
      ctx.lineWidth = Math.round(stroke * 0.9);
      ctx.stroke();
      ctx.beginPath();
      ctx.arc(cx, H / 2, Math.round(outer * 0.32), 0, Math.PI * 2);
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
