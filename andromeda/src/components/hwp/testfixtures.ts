// testfixtures.ts — hand-built CFB / HWP bytes for the parser tests. Kept out
// of the .test file so the byte-layout knowledge lives in one place.
//
// The synthetic CFB uses a mini-stream cutoff of 0, so every stream (however
// small) is stored through the regular FAT — this keeps the fixture builder
// simple while still exercising the reader's FAT path. Real HWP files set the
// cutoff to 4096; the mini-FAT path is separate and not fixture-tested here.

const SECTOR = 512;
const FREESECT = 0xffffffff;
const ENDOFCHAIN = 0xfffffffe;
const FATSECT = 0xfffffffd;

export function hwpWchars(s: string): number[] {
  return Array.from(s).map((c) => c.charCodeAt(0));
}

// buildParaTextRecord makes one HWPTAG_PARA_TEXT record (tag 67) from a string
// or an explicit WCHAR array (for control-char cases).
export function buildParaTextRecord(input: string | number[]): Uint8Array {
  const wchars = typeof input === "string" ? hwpWchars(input) : input;
  const payload = new Uint8Array(wchars.length * 2);
  const pdv = new DataView(payload.buffer);
  wchars.forEach((w, i) => pdv.setUint16(i * 2, w & 0xffff, true));
  const size = payload.length;
  const header = ((67 & 0x3ff) | ((size & 0xfff) << 20)) >>> 0;
  const rec = new Uint8Array(4 + size);
  new DataView(rec.buffer).setUint32(0, header, true);
  rec.set(payload, 4);
  return rec;
}

// buildFileHeader makes a 256-byte HWP FileHeader stream. version is laid out
// as bytes [32,33,34,35]; parseHwp reads them high-to-low ("5.1.0.0").
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
// sector 1, stream data from sector 2. Enough entries fit one directory sector
// (4 × 128 B), which covers the fixtures (root + ≤3 streams).
export function buildCfb(streams: { name: string; data: Uint8Array }[]): ArrayBuffer {
  const fat: number[] = [];
  const setFat = (i: number, v: number) => {
    while (fat.length <= i) fat.push(FREESECT);
    fat[i] = v;
  };
  setFat(0, FATSECT); // FAT self-reference
  setFat(1, ENDOFCHAIN); // directory (1 sector)

  type DirEntry = { name: string; type: number; start: number; size: number };
  const dir: DirEntry[] = [{ name: "Root Entry", type: 5, start: ENDOFCHAIN, size: 0 }];

  let nextSector = 2;
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
  dv.setUint16(28, 0xfffe, true); // byte order
  dv.setUint16(30, 9, true); // sector shift (512)
  dv.setUint16(32, 6, true); // mini sector shift (64)
  dv.setUint32(44, 1, true); // number of FAT sectors
  dv.setUint32(48, 1, true); // first directory sector
  dv.setUint32(56, 0, true); // mini-stream cutoff = 0 (all streams via FAT)
  dv.setUint32(60, ENDOFCHAIN, true); // first mini-FAT sector
  dv.setUint32(68, ENDOFCHAIN, true); // first DIFAT sector
  dv.setUint32(76, 0, true); // DIFAT[0] → FAT lives at sector 0
  for (let i = 1; i < 109; i++) dv.setUint32(76 + i * 4, FREESECT, true);

  // FAT at sector 0 (offset 512), padded to 128 entries.
  const fatBase = SECTOR;
  for (let i = 0; i < 128; i++) dv.setUint32(fatBase + i * 4, i < fat.length ? fat[i] : FREESECT, true);

  // Directory at sector 1 (offset 1024), 128 bytes per entry.
  const dirBase = SECTOR * 2;
  dir.forEach((e, idx) => {
    const off = dirBase + idx * 128;
    for (let i = 0; i < e.name.length; i++) dv.setUint16(off + i * 2, e.name.charCodeAt(i), true);
    dv.setUint16(off + 64, (e.name.length + 1) * 2, true); // name length incl. NUL
    dv.setUint8(off + 66, e.type);
    dv.setUint32(off + 116, e.start, true);
    dv.setUint32(off + 120, e.size, true);
  });

  // Stream bytes.
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
