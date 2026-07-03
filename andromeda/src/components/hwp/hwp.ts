// hwp.ts — a dependency-free text extractor for HWP 5.x documents.
//
// Scope is deliberate: pull the readable text (paragraphs, table-cell text)
// out of a 한/글 document so the desktop viewer can PREVIEW a quote/contract
// attachment inline instead of forcing a download. Layout, fonts, images, and
// exact table geometry are NOT reconstructed — for those the user opens the
// original (the viewer keeps the download link). Table cells are stored as
// their own paragraphs in HWP, so an itemized 견적서 still reads top-to-bottom.
//
// Pipeline: CFB container -> FileHeader (version + compressed flag) -> each
// BodyText/Section stream (raw-deflate-decompressed when compressed) -> HWP
// record tree -> HWPTAG_PARA_TEXT records -> UTF-16 text with the control-char
// rules applied.
//
// Reference: the openly published 한글문서파일형식 5.0 spec (hancom support
// center) — the same source the community hwp.js/hwpjs parsers use.

import { Cfb } from "./cfb";

// HWP record tag ids (HWPTAG_BEGIN = 0x10). Only PARA_TEXT is consumed here.
const HWPTAG_BEGIN = 0x10;
const HWPTAG_PARA_TEXT = HWPTAG_BEGIN + 51; // 67

export interface HwpDocument {
  version: string; // e.g. "5.1.0.0"
  paragraphs: string[]; // one entry per PARA_TEXT record (incl. table cells)
  text: string; // paragraphs joined by blank lines
}

// hasNativeInflate reports whether the runtime provides DecompressionStream —
// the browser/Tauri webview does; jsdom (tests) does not, so callers can pass
// uncompressed docs there and expect a clear error on compressed ones.
export function hasNativeInflate(): boolean {
  return typeof (globalThis as { DecompressionStream?: unknown }).DecompressionStream === "function";
}

// inflateRaw decompresses a raw-DEFLATE stream (HWP compresses each stream with
// no zlib header) using the platform DecompressionStream. Throws when the API
// is unavailable so parseHwp can report clearly.
async function inflateRaw(data: Uint8Array): Promise<Uint8Array> {
  const DS = (globalThis as { DecompressionStream?: typeof DecompressionStream }).DecompressionStream;
  if (!DS) throw new Error("DecompressionStream unavailable");
  const stream = new DS("deflate-raw");
  const writer = stream.writable.getWriter();
  // Copy into an ArrayBuffer-backed view (data may be SharedArrayBuffer-backed
  // per the DOM lib types; the write sink wants a plain BufferSource).
  void writer.write(new Uint8Array(data));
  void writer.close();
  const chunks: Uint8Array[] = [];
  const reader = stream.readable.getReader();
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    if (value) chunks.push(value);
  }
  let total = 0;
  for (const c of chunks) total += c.length;
  const out = new Uint8Array(total);
  let off = 0;
  for (const c of chunks) {
    out.set(c, off);
    off += c.length;
  }
  return out;
}

// The HWP control-char classes. char controls occupy 1 WCHAR; inline and
// extended controls occupy 8 WCHARs (the marker + 7 payload). Anything not
// listed here that is < 32 is treated as an 8-WCHAR control.
const CHAR_CONTROLS = new Set([0, 10, 13, 24, 25, 26, 27, 28, 29, 30, 31]);

// paraTextToString applies those rules to one PARA_TEXT payload (UTF-16LE
// WCHARs). Tabs and line breaks become their whitespace; all other controls
// are dropped. Intra-paragraph spaces are preserved (real content).
function paraTextToString(payload: Uint8Array): string {
  const dv = new DataView(payload.buffer, payload.byteOffset, payload.byteLength);
  const n = Math.floor(payload.byteLength / 2);
  let out = "";
  for (let i = 0; i < n; ) {
    const c = dv.getUint16(i * 2, true);
    if (c >= 0xe000 && c <= 0xf8ff) {
      // Private Use Area: HWP maps decorative/bullet glyphs here per font.
      // They render as tofu (□) outside 한/글, so drop them from the preview.
      i += 1;
    } else if (c >= 32) {
      out += String.fromCharCode(c);
      i += 1;
    } else if (c === 9) {
      out += "\t"; // tab is an inline control (8 WCHARs) rendered as a tab
      i += 8;
    } else if (c === 10 || c === 13) {
      out += "\n"; // line / paragraph break
      i += 1;
    } else if (CHAR_CONTROLS.has(c)) {
      i += 1; // other 1-WCHAR controls: drop
    } else {
      i += 8; // inline / extended controls: skip the whole 8-WCHAR unit
    }
  }
  return out;
}

// extractParagraphs walks a decompressed section's record tree and collects the
// text of every PARA_TEXT record in document order.
function extractParagraphs(section: Uint8Array): string[] {
  const dv = new DataView(section.buffer, section.byteOffset, section.byteLength);
  const paras: string[] = [];
  let pos = 0;
  while (pos + 4 <= section.byteLength) {
    const header = dv.getUint32(pos, true);
    const tagId = header & 0x3ff;
    let size = (header >> 20) & 0xfff;
    pos += 4;
    if (size === 0xfff) {
      if (pos + 4 > section.byteLength) break;
      size = dv.getUint32(pos, true);
      pos += 4;
    }
    if (pos + size > section.byteLength) break;
    if (tagId === HWPTAG_PARA_TEXT) {
      const text = paraTextToString(section.subarray(pos, pos + size)).replace(/\s+$/, "");
      if (text.trim()) paras.push(text);
    }
    pos += size;
  }
  return paras;
}

// parseHwp extracts the text of an HWP 5.x document from its raw bytes.
export async function parseHwp(buf: ArrayBuffer): Promise<HwpDocument> {
  const cfb = new Cfb(buf);

  const header = cfb.read("FileHeader");
  if (!header || header.byteLength < 40) throw new Error("HWP FileHeader missing");
  const sig = new TextDecoder("ascii").decode(header.subarray(0, 17));
  if (!sig.startsWith("HWP Document File")) throw new Error("not an HWP 5.x document");
  const version = `${header[35]}.${header[34]}.${header[33]}.${header[32]}`;
  // FileHeader flags at offset 36 (uint32 LE): bit 0 = whole-doc compressed.
  const flags = new DataView(header.buffer, header.byteOffset, header.byteLength).getUint32(36, true);
  const compressed = (flags & 0x01) === 1;

  // BodyText sections are named "Section0", "Section1", … Enumerate them in
  // order rather than guessing a count.
  const sectionNames = cfb
    .streams()
    .map((s) => s.name)
    .filter((name) => /^Section\d+$/.test(name))
    .sort((a, b) => Number(a.slice(7)) - Number(b.slice(7)));

  const paragraphs: string[] = [];
  for (const name of sectionNames) {
    const raw = cfb.read(name);
    if (!raw) continue;
    let bytes = raw;
    if (compressed) {
      if (!hasNativeInflate()) throw new Error("압축 해제 불가 (이 환경에 DecompressionStream 없음)");
      bytes = await inflateRaw(raw);
    }
    paragraphs.push(...extractParagraphs(bytes));
  }

  return { version, paragraphs, text: paragraphs.join("\n\n") };
}
