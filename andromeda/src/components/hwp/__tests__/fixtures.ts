// fixtures.ts — hand-built CFB / HWP bytes for the parser tests. Kept out
// of the .test files so the byte-layout knowledge lives in one place.
//
// By default the synthetic CFB uses a mini-stream cutoff of 0, so every stream
// (however small) is stored through the regular FAT. Pass { miniCutoff: 4096 }
// (the real HWP value) to pack small streams into the root entry's mini-stream
// instead — that exercises the reader's mini-FAT path the way real files do.

const SECTOR = 512;
const MINI_SECTOR = 64;
const FREESECT = 0xffffffff;
const ENDOFCHAIN = 0xfffffffe;
const FATSECT = 0xfffffffd;
const DIFSECT = 0xfffffffc;

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
// out at [32,33,34,35]; parseHwp reads them high-to-low ("5.1.0.0"). Property
// bits: 0 = compressed, 2 = 배포용 문서 (view-only distribution format).
export function buildFileHeader(opts: {
  compressed: boolean;
  version: [number, number, number, number];
  distribution?: boolean;
}): Uint8Array {
  const b = new Uint8Array(256);
  const sig = "HWP Document File";
  for (let i = 0; i < sig.length; i++) b[i] = sig.charCodeAt(i);
  b[32] = opts.version[0];
  b[33] = opts.version[1];
  b[34] = opts.version[2];
  b[35] = opts.version[3];
  new DataView(b.buffer).setUint32(36, (opts.compressed ? 1 : 0) | (opts.distribution ? 4 : 0), true);
  return b;
}

// buildCfb assembles a minimal compound file: FAT at sector 0, directory at
// sector 1, then (with a non-zero miniCutoff) the mini-FAT sector + the root's
// mini-stream, then regular stream data. Fits multiple directory sectors when
// more than 4 entries are needed. With { miniCutoff: 4096 } every non-empty
// stream under the cutoff is packed into 64-byte mini-sectors, matching how
// real HWP files store FileHeader and small sections.
export function buildCfb(
  streams: { name: string; data: Uint8Array }[],
  opts: { miniCutoff?: number } = {},
): ArrayBuffer {
  const miniCutoff = opts.miniCutoff ?? 0;
  const fat: number[] = [];
  const setFat = (i: number, v: number) => {
    while (fat.length <= i) fat.push(FREESECT);
    fat[i] = v;
  };

  type DirEntry = { name: string; type: number; start: number; size: number };
  const root: DirEntry = { name: "Root Entry", type: 5, start: ENDOFCHAIN, size: 0 };
  const dir: DirEntry[] = [root];

  // Directory occupies ceil((1+streams)/4) sectors starting at sector 1.
  const dirSectors = Math.max(1, Math.ceil((streams.length + 1) / 4));
  setFat(0, FATSECT);
  for (let k = 0; k < dirSectors; k++) setFat(1 + k, k === dirSectors - 1 ? ENDOFCHAIN : 2 + k);

  let nextSector = 1 + dirSectors;
  const placed: { data: Uint8Array; sectors: number[] }[] = [];

  // Pack small streams into the mini-stream first: chain their 64-byte
  // mini-sectors through the mini-FAT and remember each start mini-sector.
  const isMini = (s: { data: Uint8Array }) => miniCutoff > 0 && s.data.length > 0 && s.data.length < miniCutoff;
  const miniFat: number[] = [];
  const miniChunks: Uint8Array[] = [];
  const miniStart = new Map<(typeof streams)[number], number>();
  let nextMini = 0;
  for (const s of streams) {
    if (!isMini(s)) continue;
    const nSec = Math.ceil(s.data.length / MINI_SECTOR);
    miniStart.set(s, nextMini);
    for (let k = 0; k < nSec; k++) miniFat[nextMini + k] = k === nSec - 1 ? ENDOFCHAIN : nextMini + k + 1;
    nextMini += nSec;
    const padded = new Uint8Array(nSec * MINI_SECTOR);
    padded.set(s.data);
    miniChunks.push(padded);
  }
  const miniData = concatBytes(...miniChunks);
  let firstMiniFatSector = ENDOFCHAIN;
  const miniFatSectors: number[] = [];
  if (miniData.length > 0) {
    // Mini-FAT: ceil(entries / 128) sectors, chained through the regular FAT, so a
    // mini-stream larger than 128 mini-sectors (8 KB) is represented faithfully
    // instead of truncating the chain at one sector — the layout real HWPs use.
    const entriesPerSector = SECTOR / 4; // 128 mini-FAT entries per 512-byte sector
    const mfCount = Math.max(1, Math.ceil(miniFat.length / entriesPerSector));
    for (let k = 0; k < mfCount; k++) miniFatSectors.push(nextSector++);
    miniFatSectors.forEach((sec, k) => setFat(sec, k === mfCount - 1 ? ENDOFCHAIN : miniFatSectors[k + 1]));
    firstMiniFatSector = miniFatSectors[0];
    // The mini-stream itself is a regular FAT chain owned by the root entry.
    const nSec = Math.ceil(miniData.length / SECTOR);
    const secs: number[] = [];
    for (let k = 0; k < nSec; k++) secs.push(nextSector++);
    secs.forEach((sec, k) => setFat(sec, k === nSec - 1 ? ENDOFCHAIN : secs[k + 1]));
    placed.push({ data: miniData, sectors: secs });
    root.start = secs[0];
    root.size = miniData.length;
  }

  for (const s of streams) {
    if (isMini(s)) {
      dir.push({ name: s.name, type: 2, start: miniStart.get(s)!, size: s.data.length });
      continue;
    }
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
  dv.setUint32(56, miniCutoff, true); // mini-stream cutoff (0 = all streams via FAT)
  dv.setUint32(60, firstMiniFatSector, true); // first mini-FAT sector (ENDOFCHAIN when unused)
  dv.setUint32(64, miniFatSectors.length, true); // mini-FAT sector count
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

  miniFatSectors.forEach((sec, k) => {
    const base = SECTOR + sec * SECTOR;
    for (let i = 0; i < 128; i++) {
      const idx = k * 128 + i;
      dv.setUint32(base + i * 4, idx < miniFat.length ? miniFat[idx] : FREESECT, true);
    }
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

// buildCfbDifatChain hand-rolls a compound file whose FAT is only fully
// reachable through the extended DIFAT chain (header offset 68): FAT sector 0
// is listed in the header DIFAT, FAT sector 1 only inside a DIFAT sector. The
// lone stream ("Big") is chained across sectors 128 → 129, and BOTH links live
// in FAT sector 1 — a reader that ignores the DIFAT chain truncates the stream
// to its first sector. Returns the buffer plus the expected stream bytes.
export function buildCfbDifatChain(): { buf: ArrayBuffer; expected: Uint8Array } {
  const totalSectors = 130; // 0:FAT0, 1:dir, 2:DIFAT, 3:FAT1, …, 128–129: stream
  const buf = new ArrayBuffer(SECTOR + totalSectors * SECTOR);
  const bytes = new Uint8Array(buf);
  const dv = new DataView(buf);
  const sectorBase = (sec: number) => SECTOR + sec * SECTOR;

  [0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1].forEach((b, i) => (bytes[i] = b));
  dv.setUint16(26, 3, true);
  dv.setUint16(28, 0xfffe, true);
  dv.setUint16(30, 9, true); // 512-byte sectors
  dv.setUint16(32, 6, true);
  dv.setUint32(44, 2, true); // TWO FAT sectors — the second only via DIFAT
  dv.setUint32(48, 1, true); // directory at sector 1
  dv.setUint32(56, 0, true); // cutoff 0 → the stream reads via the FAT
  dv.setUint32(60, ENDOFCHAIN, true);
  dv.setUint32(68, 2, true); // first DIFAT sector = sector 2
  dv.setUint32(72, 1, true); // one DIFAT sector
  dv.setUint32(76, 0, true); // header DIFAT[0] → FAT sector 0
  for (let i = 1; i < 109; i++) dv.setUint32(76 + i * 4, FREESECT, true);

  // FAT sector 0 (covers sectors 0..127).
  const fat0 = sectorBase(0);
  for (let i = 0; i < 128; i++) dv.setUint32(fat0 + i * 4, FREESECT, true);
  dv.setUint32(fat0 + 0 * 4, FATSECT, true); // sector 0: FAT
  dv.setUint32(fat0 + 1 * 4, ENDOFCHAIN, true); // sector 1: directory (single)
  dv.setUint32(fat0 + 2 * 4, DIFSECT, true); // sector 2: DIFAT
  dv.setUint32(fat0 + 3 * 4, FATSECT, true); // sector 3: FAT

  // DIFAT sector (at sector 2): slot 0 → FAT sector 3; last slot ends the chain.
  const dsec = sectorBase(2);
  for (let i = 0; i < 127; i++) dv.setUint32(dsec + i * 4, FREESECT, true);
  dv.setUint32(dsec + 0 * 4, 3, true);
  dv.setUint32(dsec + 127 * 4, ENDOFCHAIN, true);

  // FAT sector 1 (at sector 3, covers sectors 128..255): the stream's chain.
  const fat1 = sectorBase(3);
  for (let i = 0; i < 128; i++) dv.setUint32(fat1 + i * 4, FREESECT, true);
  dv.setUint32(fat1 + 0 * 4, 129, true); // sector 128 → 129
  dv.setUint32(fat1 + 1 * 4, ENDOFCHAIN, true); // sector 129 ends the chain

  // Directory: root + the "Big" stream starting at sector 128.
  const size = 1000;
  const writeDirEntry = (idx: number, name: string, type: number, start: number, sz: number) => {
    const off = sectorBase(1) + idx * 128;
    for (let i = 0; i < name.length; i++) dv.setUint16(off + i * 2, name.charCodeAt(i), true);
    dv.setUint16(off + 64, (name.length + 1) * 2, true);
    dv.setUint8(off + 66, type);
    dv.setUint32(off + 116, start, true);
    dv.setUint32(off + 120, sz, true);
  };
  writeDirEntry(0, "Root Entry", 5, ENDOFCHAIN, 0);
  writeDirEntry(1, "Big", 2, 128, size);

  // Stream content spanning both sectors — a repeating counter, so a truncated
  // read (sector 128 only) differs from the expected bytes in the second half.
  const expected = new Uint8Array(size);
  for (let i = 0; i < size; i++) expected[i] = i % 251;
  bytes.set(expected.subarray(0, SECTOR), sectorBase(128));
  bytes.set(expected.subarray(SECTOR), sectorBase(129));

  return { buf, expected };
}
