// Scalar parsing and text helpers shared by the deneb-ui HTML tokenizer and converter.

import type { Node } from "./denebUiParse";

// Wraps a merged text run as an implicit node — as markdown when the run
// carries markdown block structure (auto-correcting the "markdown table
// inside a card" habit: markdown nodes route through the full markdown
// renderer, tables included), else as plain text.
export function textBlockNode(s: string): Node {
  const t = s.trim();
  if (looksLikeMarkdownBlock(t)) return { type: "markdown", value: t };
  return { type: "text", value: t };
}

// Whether text carries markdown block structure (table rows, headings, list
// runs, fences) that a plain text node would render broken. Conservative:
// single bullets or lone pipes stay text.
export function looksLikeMarkdownBlock(s: string): boolean {
  if (s.includes("```")) return true;
  let pipeRows = 0;
  let bullets = 0;
  for (const line of s.split("\n")) {
    const t = line.trim();
    if (!t) continue;
    if (t.startsWith("|")) {
      if (++pipeRows >= 2) return true;
      continue;
    }
    if (isMarkdownHeading(t)) return true;
    if (isMarkdownBullet(t) && ++bullets >= 2) return true;
  }
  return false;
}

export function isMarkdownHeading(t: string): boolean {
  let n = 0;
  while (n < t.length && t[n] === "#") n++;
  return n >= 1 && n <= 6 && n < t.length && t[n] === " ";
}

export function isMarkdownBullet(t: string): boolean {
  if (t.length >= 2 && (t[0] === "-" || t[0] === "*") && t[1] === " ") return true;
  if (t.startsWith("• ")) return true;
  let i = 0;
  while (i < t.length && t[i] >= "0" && t[i] <= "9") i++;
  return i >= 1 && i + 1 < t.length && (t[i] === "." || t[i] === ")") && t[i + 1] === " ";
}

// ---------------------------------------------------------------------------
// Element → node conversion
// ---------------------------------------------------------------------------

// Absolute index of the first `</name` at or after `from`, comparing the
// (ASCII, whitelist-only) tag name case-insensitively via manual folding.
// Never lowercase the whole source for index math: Unicode case mapping can
// change string length (e.g. "İ".toLowerCase() === "i̇"), skewing indexes into
// the original — the fuzzer-found crash in the Go port of this parser.
export function indexOfCloseTag(s: string, from: number, name: string): number {
  const n = name.length;
  for (let i = from; i + 2 + n <= s.length; i++) {
    if (s[i] !== "<" || s[i + 1] !== "/") continue;
    let match = true;
    for (let k = 0; k < n; k++) {
      let c = s.charCodeAt(i + 2 + k);
      if (c >= 65 && c <= 90) c += 32;
      if (c !== name.charCodeAt(k)) {
        match = false;
        break;
      }
    }
    if (match) {
      const end = i + 2 + n;
      if (end === s.length || s[end] === ">" || isSpace(s[end])) return i;
    }
  }
  return -1;
}

export function truthy(v: string): boolean {
  return !["false", "0", "no", "off"].includes(v.trim().toLowerCase());
}

// Extracts a number from a lenient attribute value: exact floats parse as-is;
// otherwise units, thousands commas, and stray symbols are tolerated
// ("1,200톤" → 1200, "68%" → 68, "16px" → 16).
export function lenientFloat(v: string | undefined): number | undefined {
  const t = v?.trim() ?? "";
  if (!t) return undefined;
  const exact = Number(t);
  if (Number.isFinite(exact)) return exact;
  let start = -1;
  for (let i = 0; i < t.length; i++) {
    if (t[i] >= "0" && t[i] <= "9") {
      start = i;
      break;
    }
  }
  if (start < 0) return undefined;
  let b = start > 0 && t[start - 1] === "-" ? "-" : "";
  let dot = false;
  for (let i = start; i < t.length; i++) {
    const c = t[i];
    if (c >= "0" && c <= "9") b += c;
    else if (c === ",")
      continue; // thousands separator: skip
    else if (c === "." && !dot) {
      dot = true;
      b += c;
    } else break;
  }
  const f = Number(b.replace(/\.$/, ""));
  return Number.isFinite(f) ? f : undefined;
}

// Folds common CSS color words onto the badge tint enum.
export function canonBadgeColor(v: string | undefined): string | undefined {
  switch (v?.trim().toLowerCase()) {
    case "red":
      return "error";
    case "green":
      return "success";
    case "yellow":
    case "amber":
    case "orange":
      return "warning";
    case "blue":
      return "primary";
    case "gray":
    case "grey":
    case "neutral":
      return "secondary";
    default:
      return v;
  }
}

// Folds severity synonyms onto the alert enum.
export function canonSeverity(v: string | undefined): string | undefined {
  switch (v?.trim().toLowerCase()) {
    case "warn":
    case "caution":
      return "warning";
    case "danger":
    case "critical":
    case "fatal":
      return "error";
    case "ok":
    case "done":
      return "success";
    case "note":
    case "notice":
    case "information":
      return "info";
    default:
      return v;
  }
}

// Folds chart-type synonyms onto bar/line.
export function canonChartType(v: string | undefined): string | undefined {
  switch (v?.trim().toLowerCase()) {
    case "bars":
    case "column":
    case "columns":
      return "bar";
    case "lines":
    case "area":
    case "trend":
      return "line";
    default:
      return v;
  }
}

// Folds text-style synonyms onto the style enum.
export function canonTextStyle(v: string | undefined): string | undefined {
  switch (v?.trim().toLowerCase()) {
    case "heading":
    case "header":
      return "headline";
    case "subtitle":
    case "subheading":
      return "title";
    default:
      return v;
  }
}

export function isSpace(c: string): boolean {
  return c === " " || c === "\t" || c === "\n" || c === "\r";
}

export function isNameStart(c: string): boolean {
  return (c >= "a" && c <= "z") || (c >= "A" && c <= "Z");
}

export function isNameChar(c: string): boolean {
  return isNameStart(c) || (c >= "0" && c <= "9") || c === "-" || c === "_";
}

// Decodes the grammar's small entity set: named basics + numeric refs.
export function decodeEntities(s: string): string {
  if (!s.includes("&")) return s;
  let out = "";
  for (let i = 0; i < s.length;) {
    if (s[i] !== "&") {
      out += s[i];
      i++;
      continue;
    }
    const semi = s.indexOf(";", i);
    if (semi < 0 || semi - i > 10) {
      out += s[i];
      i++;
      continue;
    }
    const ent = s.slice(i + 1, semi);
    let decoded: string | null = null;
    switch (ent.toLowerCase()) {
      case "lt":
        decoded = "<";
        break;
      case "gt":
        decoded = ">";
        break;
      case "amp":
        decoded = "&";
        break;
      case "quot":
        decoded = '"';
        break;
      case "apos":
        decoded = "'";
        break;
      case "nbsp":
        decoded = " ";
        break;
      default:
        if (ent.startsWith("#")) {
          const code = ent[1] === "x" || ent[1] === "X" ? parseInt(ent.slice(2), 16) : parseInt(ent.slice(1), 10);
          if (Number.isFinite(code) && code > 0 && code <= 0x10ffff && (code < 0xd800 || code > 0xdfff)) {
            decoded = String.fromCodePoint(code);
          }
        }
    }
    if (decoded == null) {
      out += s[i];
      i++;
    } else {
      out += decoded;
      i = semi + 1;
    }
  }
  return out;
}
