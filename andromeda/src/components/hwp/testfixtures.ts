// testfixtures.ts — hand-built CFB / HWP bytes for the parser tests. Kept out
// of the .test files so the byte-layout knowledge lives in one place.
//
// The synthetic CFB uses a mini-stream cutoff of 0, so every stream (however
// small) is stored through the regular FAT — this keeps the builder simple
// while still exercising the reader's FAT path. Real HWP files set the cutoff
// to 4096; the mini-FAT path is separate and not fixture-tested here.

const SECTOR = 512;
const FREESECT = 0xffffffff;
const ENDOFCHAIN = 0xfffffffe;
const FATSECT = 0xfffffffd;

const HB = 0x10;
export const TAG_PARA_HEADER = HB + 50;
export const TAG_PARA_TEXT = HB + 51;
export const TAG_CTRL_HEADER = HB + 55;
export const TAG_LIST_HEADER = HB + 56;
export const TAG_TABLE = HB + 61;

export function hwpWchars(s: string): number[] {
  return Array.from(s).map((c) => c.charCodeAt(0));
}

export function concatBytes(...parts: Uint8Array[]): Uint8Array {
  let total = 0;
  for (const p of parts) total += p.length;
  const out = new Uint8Array(total);
  let off = 0;
  for (const p of parts) {
    out.set(p, off);
    off += p.length;
  }
  return out;
}

// buildRecord emits one HWP record: 4-byte header (tag | level | size), with
// the extended UINT32 size form when the payload is ≥ 0xfff.
export function buildRecord(tag: number, level: number, payload: Uint8Array): Uint8Array {
  if (payload.length < 0xfff) {
    const header = ((tag & 0x3ff) | ((level & 0x3ff) << 10) | ((payload.length & 0xfff) << 20)) >>> 0;
    const out = new Uint8Array(4 + payload.length);
    new DataView(out.buffer).setUint32(0, header, true);
    out.set(payload, 4);
    return out;
  }
  const header = ((tag & 0x3ff) | ((level & 0x3ff) << 10) | (0xfff << 20)) >>> 0;
  const out = new Uint8Array(8 + payload.length);
  const dv = new DataView(out.buffer);
  dv.setUint32(0, header, true);
  dv.setUint32(4, payload.length, true);
  out.set(payload, 8);
  return out;
}

function wcharsToBytes(wchars: number[]): Uint8Array {
  const p = new Uint8Array(wchars.length * 2);
  const dv = new DataView(p.buffer);
  wchars.forEach((w, i) => dv.setUint16(i * 2, w & 0xffff, true));
  return p;
}

// buildParagraph makes a top-level PARA_HEADER(level) followed by its
// PARA_TEXT(level+1) child — the real nesting the extractor walks.
export function buildParagraph(input: string | number[], level = 0): Uint8Array {
  const wchars = typeof input === "string" ? hwpWchars(input) : input;
  return concatBytes(
    buildRecord(TAG_PARA_HEADER, level, new Uint8Array(24)),
    buildRecord(TAG_PARA_TEXT, level + 1, wcharsToBytes(wchars)),
  );
}

// u16le packs a UINT16 little-endian into a 2-byte array.
function u16le(v: number): Uint8Array {
  const b = new Uint8Array(2);
  new DataView(b.buffer).setUint16(0, v, true);
  return b;
}

// buildTable makes a CTRL_HEADER("tbl ") subtree: the TABLE record carries the
// column count (offset 6), then one LIST_HEADER + PARA_HEADER/PARA_TEXT per
// cell, in row-major order. Mirrors the layout reverse-engineered from real
// HWP files. rows is a 2-D array of cell strings.
export function buildTable(rows: string[][], level = 1): Uint8Array {
  const nCols = rows[0]?.length ?? 1;
  const nRows = rows.length;
  // TABLE payload: property(4) + nRows(2) + nCols(2) — the extractor reads nCols
  // at offset 6; the rest can be zero for the test.
  const tablePayload = new Uint8Array(8);
  tablePayload.set(u16le(nRows), 4);
  tablePayload.set(u16le(nCols), 6);

  const parts: Uint8Array[] = [
    buildRecord(TAG_CTRL_HEADER, level, new TextEncoder().encode(" lbt")), // reversed "tbl "
    buildRecord(TAG_TABLE, level + 1, tablePayload),
  ];
  for (const row of rows) {
    for (const cell of row) {
      parts.push(buildRecord(TAG_LIST_HEADER, level + 1, new Uint8Array(4)));
      parts.push(buildParagraph(cell, level + 1)); // cell paragraph as a sibling
    }
  }
  // Wrap the control under a top-level PARA_HEADER, as HWP does.
  return concatBytes(buildRecord(TAG_PARA_HEADER, level - 1, new Uint8Array(24)), ...parts);
}

// buildFileHeader makes a 256-byte HWP FileHeader stream. version bytes are laid
// out at [32,33,34,35]; parseHwp reads them high-to-low ("5.1.0.0").
export function buildFileHeader(opts: { compressed: boolean; version: [number, number, number, number] }): Uint8Array {
  const b = new Uint8Array(256);
  const sig = "HWP Document File";
  for (let i = 0; i < sig.length; i++) b[i] = sig.charCodeAt(i);
  b[32] = opts.version[0];
  b[33] = opts.version[1];
  b[34] = opts.version[2];
  b[35] = opts.version[3];
  new DataView(b.buffer).setUint32(36, opts.compressed ? 1 : 0, true);
  return b;
}

// buildCfb assembles a minimal compound file: FAT at sector 0, directory at
// sector 1, stream data from sector 2. Fits multiple directory sectors when
// more than 4 entries are needed.
export function buildCfb(streams: { name: string; data: Uint8Array }[]): ArrayBuffer {
  const fat: number[] = [];
  const setFat = (i: number, v: number) => {
    while (fat.length <= i) fat.push(FREESECT);
    fat[i] = v;
  };

  type DirEntry = { name: string; type: number; start: number; size: number };
  const dir: DirEntry[] = [{ name: "Root Entry", type: 5, start: ENDOFCHAIN, size: 0 }];

  // Directory occupies ceil((1+streams)/4) sectors starting at sector 1.
  const dirSectors = Math.max(1, Math.ceil((streams.length + 1) / 4));
  setFat(0, FATSECT);
  for (let k = 0; k < dirSectors; k++) setFat(1 + k, k === dirSectors - 1 ? ENDOFCHAIN : 2 + k);

  let nextSector = 1 + dirSectors;
  const placed: { data: Uint8Array; sectors: number[] }[] = [];
  for (const s of streams) {
    const numSec = Math.max(1, Math.ceil(s.data.length / SECTOR));
    const secs: number[] = [];
    for (let k = 0; k < numSec; k++) secs.push(nextSector++);
    secs.forEach((sec, k) => setFat(sec, k === numSec - 1 ? ENDOFCHAIN : secs[k + 1]));
    placed.push({ data: s.data, sectors: secs });
    dir.push({ name: s.name, type: 2, start: s.data.length === 0 ? ENDOFCHAIN : secs[0], size: s.data.length });
  }

  const totalSectors = nextSector;
  const buf = new ArrayBuffer(SECTOR + totalSectors * SECTOR);
  const bytes = new Uint8Array(buf);
  const dv = new DataView(buf);

  [0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1].forEach((byte, i) => (bytes[i] = byte));
  dv.setUint16(26, 3, true); // major version → 512-byte sectors
  dv.setUint16(28, 0xfffe, true);
  dv.setUint16(30, 9, true); // sector shift (512)
  dv.setUint16(32, 6, true); // mini sector shift (64)
  dv.setUint32(44, 1, true); // number of FAT sectors
  dv.setUint32(48, 1, true); // first directory sector
  dv.setUint32(56, 0, true); // mini-stream cutoff = 0 (all streams via FAT)
  dv.setUint32(60, ENDOFCHAIN, true);
  dv.setUint32(68, ENDOFCHAIN, true);
  dv.setUint32(76, 0, true); // DIFAT[0] → FAT at sector 0
  for (let i = 1; i < 109; i++) dv.setUint32(76 + i * 4, FREESECT, true);

  const fatBase = SECTOR;
  for (let i = 0; i < 128; i++) dv.setUint32(fatBase + i * 4, i < fat.length ? fat[i] : FREESECT, true);

  const dirBase = SECTOR * 2; // sector 1
  dir.forEach((e, idx) => {
    const off = dirBase + idx * 128;
    for (let i = 0; i < e.name.length; i++) dv.setUint16(off + i * 2, e.name.charCodeAt(i), true);
    dv.setUint16(off + 64, (e.name.length + 1) * 2, true);
    dv.setUint8(off + 66, e.type);
    dv.setUint32(off + 116, e.start, true);
    dv.setUint32(off + 120, e.size, true);
  });

  for (const p of placed) {
    let written = 0;
    for (const sec of p.sectors) {
      const base = SECTOR + sec * SECTOR;
      const n = Math.min(SECTOR, p.data.length - written);
      bytes.set(p.data.subarray(written, written + n), base);
      written += n;
    }
  }
  return buf;
}
