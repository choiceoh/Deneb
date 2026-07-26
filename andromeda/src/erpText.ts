/**
 * Amaranth ERP snapshots arrive as one flat text blob, but they are not prose —
 * every area (재고·발주·입고·출고·단가·매출·게시판) follows the same grammar:
 *
 *   현재고 요약 (구매/자재 · Amaranth · 품목 집계)      ← title
 *   기준연도: 2026 · 필터: 모듈·인버터(기본 …)          ← label: value pairs
 *   원본행: 113 · 집계품목: 103 · 재고수량합: 158,590   ← more pairs
 *                                                       ← blank
 *   재고 상위(창고 합산):                                ← section
 *   1. LR7-72HGD-615Ma · 재고 42,284 · 가용 42,284 …    ← ranked rows
 *                                                       ← blank
 *   출처: POST /purchase/… · searchType=coCd            ← provenance note
 *
 * Rendering that through Markdown collapsed the pair lines into a run-on
 * paragraph (Markdown joins consecutive lines) and left each ranked row as one
 * long "·"-joined string. Parsing the grammar lets the pane lay the parts out.
 *
 * FIELD SEPARATOR IS " · " (spaced), never a bare "·" — values legitimately
 * contain the bare form ("모듈·인버터"), so splitting on the spaced separator is
 * exactly what keeps "필터: 모듈·인버터(기본 …)" one value instead of two.
 */

export type ErpPair = { label: string; value: string };

export type ErpBlock =
  | { kind: "title"; text: string }
  | { kind: "meta"; pairs: ErpPair[] }
  | { kind: "section"; text: string }
  | { kind: "row"; rank: string; name: string; fields: string[] }
  | { kind: "note"; text: string };

const FIELD_SEP = " · ";
const RANK_RE = /^(\d+)\.\s+(.*)$/;
/** "레이블: 값" — a short colon-terminated label, the value is the rest. */
const PAIR_RE = /^([^:]{1,20}):\s+(.+)$/;

function splitFields(s: string): string[] {
  return s
    .split(FIELD_SEP)
    .map((f) => f.trim())
    .filter(Boolean);
}

/** A line is a pair strip when EVERY "·"-separated field reads as "레이블: 값". */
function asPairs(line: string): ErpPair[] | null {
  const parts = splitFields(line);
  if (!parts.length) return null;
  const pairs: ErpPair[] = [];
  for (const part of parts) {
    const m = PAIR_RE.exec(part);
    if (!m) return null;
    pairs.push({ label: m[1].trim(), value: m[2].trim() });
  }
  return pairs;
}

/**
 * Parse an ERP snapshot into layout blocks. An unrecognized line degrades to
 * `note` rather than being dropped — an unparsed shape must still reach the
 * reader, just without the nicer layout.
 */
export function parseErpText(raw: string): ErpBlock[] {
  const lines = raw.replace(/\r\n/g, "\n").trim().split("\n");
  const out: ErpBlock[] = [];
  let titled = false;

  for (const rawLine of lines) {
    const line = rawLine.trim();
    if (!line) continue;

    if (!titled) {
      out.push({ kind: "title", text: line });
      titled = true;
      continue;
    }

    const ranked = RANK_RE.exec(line);
    if (ranked) {
      const [name, ...fields] = splitFields(ranked[2]);
      out.push({ kind: "row", rank: ranked[1], name: name ?? ranked[2], fields });
      continue;
    }

    // "재고 상위(창고 합산):" / "거래처 상위:" — a header, not a pair, because
    // nothing follows the colon on the same line.
    if (line.endsWith(":")) {
      out.push({ kind: "section", text: line.slice(0, -1).trim() });
      continue;
    }

    if (line.startsWith("출처:")) {
      out.push({ kind: "note", text: line });
      continue;
    }

    const pairs = asPairs(line);
    if (pairs) {
      // Fold consecutive pair lines into ONE strip: 매출 reports three separate
      // single-pair lines that all belong to the same summary.
      const prev = out[out.length - 1];
      if (prev?.kind === "meta") prev.pairs.push(...pairs);
      else out.push({ kind: "meta", pairs });
      continue;
    }

    out.push({ kind: "note", text: line });
  }

  return dropEmptySections(out);
}

/**
 * A section header with no rows under it is scaffolding the reader cannot use —
 * it happens whenever a snapshot is truncated at the row limit ("거래처 상위:"
 * with nothing after it).
 */
function dropEmptySections(blocks: ErpBlock[]): ErpBlock[] {
  return blocks.filter((b, i) => {
    if (b.kind !== "section") return true;
    for (let j = i + 1; j < blocks.length; j++) {
      if (blocks[j].kind === "row") return true;
      if (blocks[j].kind === "section") return false;
    }
    return false;
  });
}
