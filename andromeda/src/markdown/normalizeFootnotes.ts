// GFM footnote pre-pass — mirrors native FootnoteNormalizer.kt.
// Collect `[^id]: def` lines, rewrite `[^id]` refs to superscript ordinals,
// append a notes section. Undefined refs stay literal; no matched ref → untouched.

const FOOTNOTE_DEF = /^\s{0,3}\[\^([^\]\s]+)]:\s+(.+)$/;
const FOOTNOTE_REF = /\[\^([^\]\s]+)]/g;
const FENCE = /^(\s{0,3})(`{3,}|~{3,})\s*(.*?)\s*$/;
const CODE_SPAN = /(`+)[^`]*?\1/g;

const SUP: Record<string, string> = {
  "0": "⁰",
  "1": "¹",
  "2": "²",
  "3": "³",
  "4": "⁴",
  "5": "⁵",
  "6": "⁶",
  "7": "⁷",
  "8": "⁸",
  "9": "⁹",
};

function superscriptOrdinal(n: number): string {
  return String(n)
    .split("")
    .map((c) => SUP[c] ?? c)
    .join("");
}

function fenceCloses(m: RegExpExecArray, openCh: string, openLen: number): boolean {
  const run = m[2];
  return run[0] === openCh && run.length >= openLen && !m[3].trim();
}

function replaceRefs(segment: string, defs: Map<string, string>, order: Map<string, number>): string {
  if (!segment.includes("[^")) return segment;
  return segment.replace(FOOTNOTE_REF, (full, id: string) => {
    if (!defs.has(id)) return full;
    let n = order.get(id);
    if (n === undefined) {
      n = order.size + 1;
      order.set(id, n);
    }
    return superscriptOrdinal(n);
  });
}

function replaceRefsOutsideCode(line: string, defs: Map<string, string>, order: Map<string, number>): string {
  if (!line.includes("`")) return replaceRefs(line, defs, order);
  let out = "";
  let last = 0;
  CODE_SPAN.lastIndex = 0;
  let m: RegExpExecArray | null;
  while ((m = CODE_SPAN.exec(line))) {
    out += replaceRefs(line.slice(last, m.index), defs, order);
    out += m[0];
    last = m.index + m[0].length;
  }
  out += replaceRefs(line.slice(last), defs, order);
  return out;
}

/** Rewrite GFM footnotes; returns input unchanged when unused. */
export function normalizeFootnotes(text: string): string {
  if (!text.includes("[^")) return text;
  const lines = text.split("\n");
  const defs = new Map<string, string>();
  const body: string[] = [];
  let inFence = false;
  let fenceCh = " ";
  let fenceLen = 0;

  for (const line of lines) {
    const fence = FENCE.exec(line);
    if (inFence) {
      body.push(line);
      if (fence && fenceCloses(fence, fenceCh, fenceLen)) inFence = false;
      continue;
    }
    if (fence) {
      inFence = true;
      fenceCh = fence[2][0];
      fenceLen = fence[2].length;
      body.push(line);
      continue;
    }
    const def = FOOTNOTE_DEF.exec(line);
    if (def) {
      defs.set(def[1], def[2].trim());
      body.push("");
      continue;
    }
    body.push(line);
  }
  if (defs.size === 0) return text;

  const order = new Map<string, number>();
  inFence = false;
  fenceCh = " ";
  fenceLen = 0;
  for (let idx = 0; idx < body.length; idx++) {
    const line = body[idx];
    const fence = FENCE.exec(line);
    if (inFence) {
      if (fence && fenceCloses(fence, fenceCh, fenceLen)) inFence = false;
      continue;
    }
    if (fence) {
      inFence = true;
      fenceCh = fence[2][0];
      fenceLen = fence[2].length;
      continue;
    }
    if (line.includes("[^")) body[idx] = replaceRefsOutsideCode(line, defs, order);
  }
  if (order.size === 0) return text;

  const sorted = [...order.entries()].sort((a, b) => a[1] - b[1]);
  let out = body.join("\n").replace(/\s+$/, "");
  out += "\n\n---\n";
  for (const [id, ord] of sorted) {
    out += `\n${superscriptOrdinal(ord)} ${defs.get(id)}\n`;
  }
  return out;
}
