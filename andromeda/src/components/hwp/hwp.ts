// hwp.ts — a dependency-free extractor for HWP 5.x documents.
//
// Pulls the readable CONTENT — paragraphs, tables (as a grid), and embedded
// images — out of a 한/글 document so the desktop viewer can PREVIEW a
// quote/contract/report attachment inline instead of forcing a download. Fonts,
// exact page geometry, and drawing objects are not reconstructed.
//
// Pipeline: CFB container -> FileHeader (version + compressed flag) -> each
// BodyText/Section stream (raw-deflate-decompressed when compressed) -> HWP
// record tree -> blocks (paragraph | table | image). Images come from the
// BinData/ storage streams (magic-sniffed, deflate-decompressed when needed).
//
// Reference: the openly published 한글문서파일형식 5.0 spec (hancom support
// center) — the same source the community hwp.js/hwpjs parsers use.

import { Cfb } from "./cfb";

// HWP record tag ids (HWPTAG_BEGIN = 0x10).
const HWPTAG_BEGIN = 0x10;
const HWPTAG_PARA_TEXT = HWPTAG_BEGIN + 51; // 67
const HWPTAG_CTRL_HEADER = HWPTAG_BEGIN + 55; // 71
const HWPTAG_LIST_HEADER = HWPTAG_BEGIN + 56; // 72 (a table cell / list)
const HWPTAG_TABLE = HWPTAG_BEGIN + 61; // 77

// Control ids are 4 ASCII bytes stored little-endian, so they read reversed:
// "tbl " (table) arrives as bytes for " lbt".
const CTRL_TABLE = "tbl ";

export type HwpBlock =
  | { type: "para"; text: string }
  | { type: "table"; rows: string[][] }
  | { type: "image"; dataUri: string; name: string };

export interface HwpDocument {
  version: string; // e.g. "5.1.1.0"
  blocks: HwpBlock[]; // paragraphs, tables, images in document order (images appended)
  paragraphs: string[]; // paragraph text only (search/summary/back-compat)
  text: string; // paragraphs joined by blank lines
}

// hasNativeInflate reports whether the runtime provides DecompressionStream —
// the browser/Tauri webview does; jsdom (tests) does not.
export function hasNativeInflate(): boolean {
  return typeof (globalThis as { DecompressionStream?: unknown }).DecompressionStream === "function";
}

// Decompressed-size cap per stream. The viewer's 16MiB guard checks the
// COMPRESSED blob only, and raw deflate expands up to ~1000:1 — a hostile
// mail-borne HWP could otherwise balloon to gigabytes on the UI thread. 64MiB
// of inflated output is far beyond any real HWP section/BinData payload.
const MAX_INFLATED_BYTES = 64 << 20; // 64 MiB

async function inflateRaw(data: Uint8Array): Promise<Uint8Array> {
  const DS = (globalThis as { DecompressionStream?: typeof DecompressionStream }).DecompressionStream;
  if (!DS) throw new Error("DecompressionStream unavailable");
  const stream = new DS("deflate-raw");
  const writer = stream.writable.getWriter();
  // Corrupt deflate errors both sides of the transform; the reader loop below is
  // the single error channel. Swallow the writer promises' mirror rejections so
  // they don't surface as unhandled rejections.
  writer.write(new Uint8Array(data)).catch(() => {});
  writer.close().catch(() => {});
  const chunks: Uint8Array[] = [];
  let total = 0;
  let capped = false;
  const reader = stream.readable.getReader();
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      if (!value) continue;
      total += value.length;
      if (total > MAX_INFLATED_BYTES) {
        capped = true;
        await reader.cancel().catch(() => {});
        break;
      }
      chunks.push(value);
    }
  } catch {
    // Real HWP sections zero-pad the tail (streams live in 64-byte mini-sectors /
    // 512-byte FAT sectors), so the deflate data is followed by junk. The strict
    // DecompressionStream rejects that as "trailing junk" — but only AFTER it has
    // already emitted the complete valid output, which zlib silently accepts. Keep
    // the bytes we decoded in that case. A failure with NO output (genuinely
    // corrupt data, or a stream flagged compressed but actually stored plaintext)
    // falls through to the total===0 guard below and re-throws, so the caller
    // keeps the raw bytes (its per-section fallback).
  }
  if (capped) {
    throw new Error("압축 해제 결과가 미리보기 한도를 넘습니다. 원본을 내려받아 확인하세요.");
  }
  if (total === 0) {
    throw new Error("deflate produced no output");
  }
  const out = new Uint8Array(total);
  let off = 0;
  for (const c of chunks) {
    out.set(c, off);
    off += c.length;
  }
  return out;
}

// --- record tree ----------------------------------------------------------

interface Rec {
  tag: number;
  ctrlId: string; // reversed 4-char control id for CTRL_HEADER, else ""
  data: Uint8Array;
  children: Rec[];
}

// parseRecords turns a section's flat record stream into a level-nested tree.
// Each 4-byte header packs tag(10) | level(10) | size(12); size 0xfff means the
// real size follows as a UINT32.
function parseRecords(section: Uint8Array): Rec[] {
  const dv = new DataView(section.buffer, section.byteOffset, section.byteLength);
  const roots: Rec[] = [];
  const byLevel: Rec[] = []; // byLevel[l] = most recent record at level l
  let pos = 0;
  while (pos + 4 <= section.byteLength) {
    const header = dv.getUint32(pos, true);
    const tag = header & 0x3ff;
    const level = (header >> 10) & 0x3ff;
    let size = (header >> 20) & 0xfff;
    pos += 4;
    if (size === 0xfff) {
      if (pos + 4 > section.byteLength) break;
      size = dv.getUint32(pos, true);
      pos += 4;
    }
    if (pos + size > section.byteLength) break;
    const data = section.subarray(pos, pos + size);
    pos += size;

    let ctrlId = "";
    if (tag === HWPTAG_CTRL_HEADER && data.byteLength >= 4) {
      ctrlId = String.fromCharCode(data[3], data[2], data[1], data[0]);
    }
    const rec: Rec = { tag, ctrlId, data, children: [] };
    if (level === 0 || !byLevel[level - 1]) roots.push(rec);
    else byLevel[level - 1].children.push(rec);
    byLevel[level] = rec;
    byLevel.length = level + 1; // deeper stale entries can't parent later records
  }
  return roots;
}

// --- text ------------------------------------------------------------------

const CHAR_CONTROLS = new Set([0, 10, 13, 24, 25, 26, 27, 28, 29, 30, 31]);

// paraTextToString applies the HWP control-char rules to one PARA_TEXT payload
// (UTF-16LE WCHARs): char controls occupy 1 WCHAR, inline/extended controls 8.
// Tabs and breaks become whitespace; Private-Use-Area decorative glyphs and all
// other controls are dropped.
function paraTextToString(payload: Uint8Array): string {
  const dv = new DataView(payload.buffer, payload.byteOffset, payload.byteLength);
  const n = Math.floor(payload.byteLength / 2);
  let out = "";
  for (let i = 0; i < n;) {
    const c = dv.getUint16(i * 2, true);
    if (c >= 0xe000 && c <= 0xf8ff) {
      i += 1; // Private Use Area: font-specific decorative glyphs — drop
    } else if (c >= 32) {
      out += String.fromCharCode(c);
      i += 1;
    } else if (c === 9) {
      out += "\t";
      i += 8;
    } else if (c === 10 || c === 13) {
      out += "\n";
      i += 1;
    } else if (CHAR_CONTROLS.has(c)) {
      i += 1;
    } else {
      i += 8;
    }
  }
  return out;
}

// recParagraphText concatenates the PARA_TEXT of a record subtree (used for a
// table cell, whose text may span several paragraphs).
function recParagraphText(rec: Rec): string {
  const parts: string[] = [];
  const walk = (r: Rec) => {
    if (r.tag === HWPTAG_PARA_TEXT) {
      const t = paraTextToString(r.data).replace(/\s+$/, "");
      if (t) parts.push(t);
    }
    r.children.forEach(walk);
  };
  walk(rec);
  return parts.join("\n");
}

// --- tables ----------------------------------------------------------------

// tableFromCtrl reconstructs a grid from a CTRL_HEADER("tbl ") subtree: the
// child TABLE record gives the column count; each LIST_HEADER starts a cell and
// the PARA_HEADERs up to the next LIST_HEADER are that cell's text. Cells fill
// left-to-right, top-to-bottom, chunked by column count (simple grids — the
// common 견적서/계약서 case; colSpan/rowSpan are not expanded).
function tableFromCtrl(ctrl: Rec): HwpBlock | null {
  const meta = ctrl.children.find((c) => c.tag === HWPTAG_TABLE);
  if (!meta || meta.data.byteLength < 8) return null;
  const mdv = new DataView(meta.data.buffer, meta.data.byteOffset, meta.data.byteLength);
  const nCols = mdv.getUint16(6, true) || 1;

  const cells: string[] = [];
  let current: Rec | null = null;
  for (const child of ctrl.children) {
    if (child.tag === HWPTAG_LIST_HEADER) {
      current = child;
      cells.push(recParagraphText(child)); // cell's own paras (if nested here)
    } else if (current && child.tag !== HWPTAG_TABLE) {
      // A sibling PARA_HEADER after a LIST_HEADER belongs to that cell.
      const t = recParagraphText(child);
      if (t) cells[cells.length - 1] = [cells[cells.length - 1], t].filter(Boolean).join("\n");
    }
  }
  if (cells.length === 0) return null;
  const rows: string[][] = [];
  for (let i = 0; i < cells.length; i += nCols) {
    rows.push(cells.slice(i, i + nCols));
  }
  return { type: "table", rows };
}

// --- images ----------------------------------------------------------------

const IMG_MAGIC: { mime: string; bytes: number[] }[] = [
  { mime: "image/png", bytes: [0x89, 0x50, 0x4e, 0x47] },
  { mime: "image/jpeg", bytes: [0xff, 0xd8, 0xff] },
  { mime: "image/gif", bytes: [0x47, 0x49, 0x46] },
  { mime: "image/bmp", bytes: [0x42, 0x4d] },
];

function sniffImageMime(bytes: Uint8Array): string | null {
  for (const m of IMG_MAGIC) {
    if (m.bytes.every((b, i) => bytes[i] === b)) return m.mime;
  }
  return null;
}

function bytesToBase64(bytes: Uint8Array): string {
  let bin = "";
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) {
    bin += String.fromCharCode(...bytes.subarray(i, i + chunk));
  }
  return btoa(bin);
}

// extractImages pulls every BinData image stream out, decompressing when the
// bytes are stored deflated (BinData compression is per-item; sniff the magic
// and inflate on a miss). Appended to the document in BinData order — inline
// placement would need the picture-control record layout, which is deferred.
async function extractImages(cfb: Cfb): Promise<HwpBlock[]> {
  const names = cfb
    .streams()
    .map((s) => s.name)
    .filter((n) => /^BIN[0-9A-Fa-f]+\.(png|jpe?g|gif|bmp)$/i.test(n))
    .sort();
  const out: HwpBlock[] = [];
  for (const name of names) {
    const raw = cfb.read(name);
    if (!raw) continue;
    let bytes = raw;
    let mime = sniffImageMime(bytes);
    if (!mime && hasNativeInflate()) {
      try {
        bytes = await inflateRaw(raw);
        mime = sniffImageMime(bytes);
      } catch {
        mime = null;
      }
    }
    if (!mime) continue; // undecodable — skip rather than show a broken image
    out.push({ type: "image", dataUri: `data:${mime};base64,${bytesToBase64(bytes)}`, name });
  }
  return out;
}

// --- top level -------------------------------------------------------------

// extractBlocks walks a section's top-level PARA_HEADERs, emitting a paragraph
// for their inline text and a table for each CTRL_HEADER("tbl ") they carry.
function extractBlocks(roots: Rec[]): HwpBlock[] {
  const blocks: HwpBlock[] = [];
  for (const para of roots) {
    for (const child of para.children) {
      if (child.tag === HWPTAG_PARA_TEXT) {
        const t = paraTextToString(child.data).replace(/\s+$/, "");
        if (t.trim()) blocks.push({ type: "para", text: t });
      } else if (child.tag === HWPTAG_CTRL_HEADER && child.ctrlId === CTRL_TABLE) {
        const tbl = tableFromCtrl(child);
        if (tbl) blocks.push(tbl);
      }
    }
  }
  return blocks;
}

// parseHwp extracts the content of an HWP 5.x document from its raw bytes.
export async function parseHwp(buf: ArrayBuffer): Promise<HwpDocument> {
  const cfb = new Cfb(buf);

  const header = cfb.read("FileHeader");
  if (!header || header.byteLength < 40) throw new Error("HWP FileHeader missing");
  const sig = new TextDecoder("ascii").decode(header.subarray(0, 17));
  if (!sig.startsWith("HWP Document File")) throw new Error("not an HWP 5.x document");
  const version = `${header[35]}.${header[34]}.${header[33]}.${header[32]}`;
  const flags = new DataView(header.buffer, header.byteOffset, header.byteLength).getUint32(36, true);
  const compressed = (flags & 0x01) === 1;
  // Property bit 2 = 배포용 문서 (view-only distribution format): the body lives
  // encrypted under a ViewText storage, so the plain Section scan below would
  // render garbage. Fail clearly instead — the viewer shows this message with
  // the download escape hatch.
  if ((flags & 0x04) !== 0) {
    throw new Error("배포용(보기 전용) HWP 문서는 미리보기를 지원하지 않습니다. 원본을 내려받아 확인하세요.");
  }

  const sectionNames = cfb
    .streams()
    .map((s) => s.name)
    .filter((name) => /^Section\d+$/.test(name))
    .sort((a, b) => Number(a.slice(7)) - Number(b.slice(7)));

  const blocks: HwpBlock[] = [];
  for (const name of sectionNames) {
    const raw = cfb.read(name);
    if (!raw) continue;
    let bytes = raw;
    if (compressed) {
      if (!hasNativeInflate()) throw new Error("압축 해제 불가 (이 환경에 DecompressionStream 없음)");
      try {
        bytes = await inflateRaw(raw);
      } catch {
        // Some producers set the compressed flag but store a section as
        // plaintext — one such stream shouldn't kill the whole preview. Feed
        // the raw bytes to the record parser (its size checks stop quietly on
        // genuine garbage, yielding an empty section instead of a throw).
        bytes = raw;
      }
    }
    blocks.push(...extractBlocks(parseRecords(bytes)));
  }

  // Images are appended after the text/tables (inline placement is deferred).
  blocks.push(...(await extractImages(cfb)));

  const paragraphs = blocks.filter((b): b is { type: "para"; text: string } => b.type === "para").map((b) => b.text);
  return { version, blocks, paragraphs, text: paragraphs.join("\n\n") };
}
