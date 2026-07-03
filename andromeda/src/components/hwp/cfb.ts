// cfb.ts — a minimal reader for the OLE2 Compound File Binary format (aka
// CFBF / "structured storage"), the container HWP 5.x files are stored in.
// No dependency: HWP viewing needs only "pull a few named streams out of the
// container", so this implements the read path (header → FAT → directory →
// stream reassembly, including the mini-stream) and nothing else — no writing,
// no transactions, no red-black-tree balancing.
//
// Spec: [MS-CFB]. A CFB file is a flat array of fixed-size sectors. The FAT is
// a linked list of sector chains; the directory is a tree of entries naming
// storages (folders) and streams (files). Streams smaller than the header's
// cutoff live in the "mini stream" — a stream owned by the root entry, itself
// chained through the mini-FAT in 64-byte mini-sectors.

const SIGNATURE = [0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1];
const ENDOFCHAIN = 0xfffffffe;
const FREESECT = 0xffffffff;

export interface CfbEntry {
  name: string;
  type: number; // 1 = storage, 2 = stream, 5 = root
  start: number; // starting sector (or mini-sector for small streams)
  size: number;
}

// Cfb exposes the parsed directory and a stream reader keyed by name.
export class Cfb {
  private data: DataView;
  private sectorSize: number;
  private miniSectorSize: number;
  private miniCutoff: number;
  private fat: number[] = [];
  private miniFat: number[] = [];
  private entries: CfbEntry[] = [];
  private miniStream: Uint8Array = new Uint8Array(0);

  constructor(buf: ArrayBuffer) {
    this.data = new DataView(buf);
    for (let i = 0; i < 8; i++) {
      if (this.data.getUint8(i) !== SIGNATURE[i]) {
        throw new Error("not a compound-file (HWP 5.x) document");
      }
    }
    const sectorShift = this.data.getUint16(30, true);
    const miniShift = this.data.getUint16(32, true);
    this.sectorSize = 1 << sectorShift; // 512 (v3) or 4096 (v4)
    this.miniSectorSize = 1 << miniShift; // 64
    this.miniCutoff = this.data.getUint32(56, true); // usually 4096

    this.readFat();
    this.readDirectory();
    this.readMiniFatAndStream();
  }

  // sectorOffset maps a sector number to its byte offset. Per [MS-CFB] sector N
  // begins at (N+1)*sectorSize: the header occupies the first full sector. In v3
  // that's 512 bytes; in v4 the 512-byte header is padded to a full 4096-byte
  // sector, so hardcoding 512 here would mis-locate every v4 sector.
  private sectorOffset(sector: number): number {
    return (sector + 1) * this.sectorSize;
  }

  private readFat() {
    const numFatSectors = this.data.getUint32(44, true);
    // The first 109 FAT sector locations sit in the header (DIFAT); that covers
    // every HWP we care about (109 * sectorSize/4 * sectorSize ≫ any office doc),
    // so the extended DIFAT chain is intentionally not followed.
    const fatSectors: number[] = [];
    for (let i = 0; i < Math.min(numFatSectors, 109); i++) {
      const s = this.data.getUint32(76 + i * 4, true);
      if (s === FREESECT) break;
      fatSectors.push(s);
    }
    const perSector = this.sectorSize / 4;
    for (const fs of fatSectors) {
      const base = this.sectorOffset(fs);
      for (let i = 0; i < perSector; i++) {
        this.fat.push(this.data.getUint32(base + i * 4, true));
      }
    }
  }

  // followChain walks a FAT sector chain from a start sector to ENDOFCHAIN,
  // guarding against a cyclic/corrupt chain by capping at the FAT length.
  private followChain(start: number): number[] {
    const chain: number[] = [];
    let s = start;
    while (s !== ENDOFCHAIN && s !== FREESECT && chain.length <= this.fat.length) {
      chain.push(s);
      s = this.fat[s];
      if (s === undefined) break;
    }
    return chain;
  }

  private readSectors(start: number): Uint8Array {
    const chain = this.followChain(start);
    const out = new Uint8Array(chain.length * this.sectorSize);
    chain.forEach((s, i) => {
      const base = this.sectorOffset(s);
      out.set(new Uint8Array(this.data.buffer, base, this.sectorSize), i * this.sectorSize);
    });
    return out;
  }

  private readDirectory() {
    const dirStart = this.data.getUint32(48, true);
    const bytes = this.readSectors(dirStart);
    const dv = new DataView(bytes.buffer);
    const entrySize = 128;
    for (let off = 0; off + entrySize <= bytes.length; off += entrySize) {
      const nameLen = dv.getUint16(off + 64, true); // bytes incl. terminating NUL
      const type = dv.getUint8(off + 66);
      if (type === 0) continue; // unallocated
      let name = "";
      for (let i = 0; i < nameLen - 2 && i < 64; i += 2) {
        const c = dv.getUint16(off + i, true);
        if (c === 0) break;
        name += String.fromCharCode(c);
      }
      this.entries.push({
        name,
        type,
        start: dv.getUint32(off + 116, true),
        size: dv.getUint32(off + 120, true), // low 32 bits — enough for HWP
      });
    }
  }

  private readMiniFatAndStream() {
    const miniFatStart = this.data.getUint32(60, true);
    if (miniFatStart !== ENDOFCHAIN) {
      const bytes = this.readSectors(miniFatStart);
      const dv = new DataView(bytes.buffer);
      for (let i = 0; i + 4 <= bytes.length; i += 4) this.miniFat.push(dv.getUint32(i, true));
    }
    // The root entry's stream IS the mini-stream (owns all small streams).
    const root = this.entries.find((e) => e.type === 5);
    if (root && root.size > 0) {
      this.miniStream = this.readSectors(root.start).slice(0, root.size);
    }
  }

  // readStreamFromMini reassembles a small stream from the mini-FAT chain.
  private readStreamFromMini(start: number, size: number): Uint8Array {
    const out = new Uint8Array(size);
    let s = start;
    let written = 0;
    let guard = 0;
    while (s !== ENDOFCHAIN && s !== FREESECT && written < size && guard++ <= this.miniFat.length) {
      const base = s * this.miniSectorSize;
      const n = Math.min(this.miniSectorSize, size - written);
      out.set(this.miniStream.subarray(base, base + n), written);
      written += n;
      s = this.miniFat[s];
      if (s === undefined) break;
    }
    return out;
  }

  // Names of all streams/storages in the directory (for discovery/debugging).
  names(): string[] {
    return this.entries.map((e) => e.name);
  }

  // read returns the bytes of a named stream, from the FAT or the mini-FAT
  // depending on its size, or null when the name is absent.
  read(name: string): Uint8Array | null {
    const e = this.entries.find((x) => x.name === name && x.type === 2);
    if (!e) return null;
    if (e.size < this.miniCutoff) return this.readStreamFromMini(e.start, e.size);
    return this.readSectors(e.start).slice(0, e.size);
  }

  // Stream entries only (type 2), for callers that enumerate sections.
  streams(): CfbEntry[] {
    return this.entries.filter((e) => e.type === 2);
  }
}
