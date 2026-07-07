import { describe, expect, it, vi } from "vitest";
import { Cfb } from "./cfb";
import { hasNativeInflate, type HwpBlock, parseHwp } from "./hwp";
import {
  buildCfb,
  buildCfbDifatChain,
  buildFileHeader,
  buildParagraph,
  buildTable,
  concatBytes,
  hwpWchars,
} from "./testfixtures";

// The parser is exercised against HAND-BUILT compound files (no real HWP binary
// needed): a minimal CFB with a FileHeader + one uncompressed Section0 whose
// record tree we control. That covers the CFB reader, the record walker, the
// control-char rules, table reconstruction, and BinData image extraction
// deterministically. Compression is left to the platform DecompressionStream at
// runtime (jsdom lacks it), so the fixtures use the uncompressed flag and
// pre-decoded (magic-valid) image bytes.

function doc(...sectionParts: Uint8Array[]) {
  return buildCfb([
    { name: "FileHeader", data: buildFileHeader({ compressed: false, version: [0, 0, 1, 5] }) },
    { name: "Section0", data: concatBytes(...sectionParts) },
  ]);
}

// deflateRawViaPlatform compresses with the same platform primitive the parser
// decompresses with, so a round-trip exercises the real inflate path (not a stub).
async function deflateRawViaPlatform(data: Uint8Array): Promise<Uint8Array> {
  const cs = new CompressionStream("deflate-raw");
  const w = cs.writable.getWriter();
  void w.write(new Uint8Array(data));
  void w.close();
  const chunks: Uint8Array[] = [];
  const r = cs.readable.getReader();
  for (;;) {
    const { value, done } = await r.read();
    if (done) break;
    if (value) chunks.push(value);
  }
  const total = chunks.reduce((a, c) => a + c.length, 0);
  const out = new Uint8Array(total);
  let off = 0;
  for (const c of chunks) {
    out.set(c, off);
    off += c.length;
  }
  return out;
}

describe("Cfb", () => {
  it("reads named streams out of a synthetic compound file", () => {
    const cfb = new Cfb(
      buildCfb([
        { name: "FileHeader", data: new Uint8Array([1, 2, 3, 4]) },
        { name: "Section0", data: new Uint8Array([9, 9]) },
      ]),
    );
    expect(cfb.names()).toContain("FileHeader");
    expect(cfb.read("FileHeader")).toEqual(new Uint8Array([1, 2, 3, 4]));
    expect(cfb.read("Nope")).toBeNull();
  });

  it("rejects a non-compound file", () => {
    expect(() => new Cfb(new Uint8Array([0, 1, 2, 3, 4, 5, 6, 7, 8]).buffer)).toThrow(/compound/);
  });

  it("follows the extended DIFAT chain for FAT sectors beyond the header's 109", () => {
    // The stream's chain links live in a FAT sector only reachable via the DIFAT
    // chain — a reader that stops at the header DIFAT truncates it to one sector.
    const { buf, expected } = buildCfbDifatChain();
    const cfb = new Cfb(buf);
    expect(cfb.read("Big")).toEqual(expected);
  });

  it("reads small streams through the mini-FAT at the real cutoff (4096)", () => {
    const cfb = new Cfb(
      buildCfb(
        [
          { name: "Tiny", data: new Uint8Array([1, 2, 3]) }, // < 64B → one mini-sector
          { name: "Spans", data: new Uint8Array(150).fill(7) }, // 3 chained mini-sectors
        ],
        { miniCutoff: 4096 },
      ),
    );
    expect(cfb.read("Tiny")).toEqual(new Uint8Array([1, 2, 3]));
    expect(cfb.read("Spans")).toEqual(new Uint8Array(150).fill(7));
  });

  it("reassembles a mini-stream whose chain crosses the 128-entry mini-FAT sector boundary", () => {
    // Three ~3 KB streams all sit under the 4096 cutoff, so the mini-stream needs
    // >128 mini-sectors and the mini-FAT spans TWO sectors. Reading the last stream
    // walks a chain into the SECOND mini-FAT sector — the exact layout a real HWP
    // with an embedded image uses (e.g. 그림.hwp: 20-sector mini-stream container,
    // 2-sector mini-FAT). A reader that only loaded the first mini-FAT sector would
    // truncate that chain and hand the record parser corrupt bytes.
    const mk = (seed: number, n: number) => {
      const a = new Uint8Array(n);
      for (let i = 0; i < n; i++) a[i] = (seed * 31 + i * 7) & 0xff;
      return a;
    };
    const a = mk(1, 3000);
    const b = mk(2, 3000);
    const c = mk(3, 3000); // FileHeader(4) + a(47) + b(47) = mini-sector 98 → chain crosses 128
    const cfb = new Cfb(
      buildCfb(
        [
          { name: "FileHeader", data: buildFileHeader({ compressed: false, version: [0, 0, 1, 5] }) },
          { name: "A", data: a },
          { name: "B", data: b },
          { name: "C", data: c },
        ],
        { miniCutoff: 4096 },
      ),
    );
    expect(cfb.read("C")).toEqual(c);
    expect(cfb.read("A")).toEqual(a);
    expect(cfb.read("B")).toEqual(b);
  });
});

describe("parseHwp", () => {
  it("extracts paragraphs in document order", async () => {
    const buf = doc(
      buildParagraph("탑솔라 견적서"),
      buildParagraph("품목: 태양광 모듈"),
      buildParagraph("금액: 1,200,000원"),
    );
    const d = await parseHwp(buf);
    expect(d.version).toBe("5.1.0.0");
    expect(d.paragraphs).toEqual(["탑솔라 견적서", "품목: 태양광 모듈", "금액: 1,200,000원"]);
    expect(d.blocks.every((b) => b.type === "para")).toBe(true);
  });

  it("reconstructs a table into a grid", async () => {
    const buf = doc(
      buildParagraph("발주 내역"),
      buildTable([
        ["품목", "수량", "금액"],
        ["모듈", "100", "12,000,000"],
        ["인버터", "5", "3,000,000"],
      ]),
    );
    const d = await parseHwp(buf);
    const table = d.blocks.find((b): b is Extract<HwpBlock, { type: "table" }> => b.type === "table");
    expect(table).toBeDefined();
    expect(table!.rows).toEqual([
      ["품목", "수량", "금액"],
      ["모듈", "100", "12,000,000"],
      ["인버터", "5", "3,000,000"],
    ]);
    // The surrounding paragraph is still a paragraph block, in order.
    expect(d.blocks[0]).toEqual({ type: "para", text: "발주 내역" });
  });

  it("skips inline/extended controls but keeps tabs and breaks", async () => {
    const wchars = [
      ...hwpWchars("가"),
      9,
      0,
      0,
      0,
      0,
      0,
      0,
      0, // tab: marker + 7 payload
      ...hwpWchars("나"),
      11,
      0,
      0,
      0,
      0,
      0,
      0,
      0, // extended control: dropped entirely
      ...hwpWchars("다"),
      10, // line break
      ...hwpWchars("라"),
    ];
    const d = await parseHwp(doc(buildParagraph(wchars)));
    expect(d.paragraphs[0]).toBe("가\t나다\n라");
  });

  it("extracts a BinData image as a data URI (magic-sniffed)", async () => {
    // A minimal PNG-magic'd stream — the sniff only needs the signature, and the
    // fixture is uncompressed so no inflate is required in jsdom.
    const png = new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3, 4]);
    const buf = buildCfb([
      { name: "FileHeader", data: buildFileHeader({ compressed: false, version: [0, 0, 1, 5] }) },
      { name: "Section0", data: buildParagraph("도면 첨부") },
      { name: "BIN0001.png", data: png },
    ]);
    const d = await parseHwp(buf);
    const img = d.blocks.find((b): b is Extract<HwpBlock, { type: "image" }> => b.type === "image");
    expect(img).toBeDefined();
    expect(img!.name).toBe("BIN0001.png");
    expect(img!.dataUri.startsWith("data:image/png;base64,")).toBe(true);
    // Images are appended after the text blocks.
    expect(d.blocks[0]).toEqual({ type: "para", text: "도면 첨부" });
  });

  it("errors clearly on a non-HWP compound file", async () => {
    const buf = buildCfb([{ name: "FileHeader", data: new Uint8Array(40) }]);
    await expect(parseHwp(buf)).rejects.toThrow(/not an HWP/);
  });

  it("parses a document stored the way real HWPs are — small streams via the mini-FAT", async () => {
    // FileHeader (256B) and a short Section0 both sit under the real 4096 cutoff,
    // so they ride the mini-stream path end to end.
    const buf = buildCfb(
      [
        { name: "FileHeader", data: buildFileHeader({ compressed: false, version: [0, 0, 1, 5] }) },
        { name: "Section0", data: buildParagraph("미니 스트림 본문") },
      ],
      { miniCutoff: 4096 },
    );
    const d = await parseHwp(buf);
    expect(d.version).toBe("5.1.0.0");
    expect(d.paragraphs).toEqual(["미니 스트림 본문"]);
  });

  it("mixes mini (FileHeader) and regular FAT (large Section0) streams at cutoff 4096", async () => {
    const long = "가나다라마바사아".repeat(300); // 4800 bytes of PARA_TEXT ≥ cutoff → regular FAT
    const buf = buildCfb(
      [
        { name: "FileHeader", data: buildFileHeader({ compressed: false, version: [0, 0, 1, 5] }) },
        { name: "Section0", data: buildParagraph(long) },
      ],
      { miniCutoff: 4096 },
    );
    const d = await parseHwp(buf);
    expect(d.paragraphs).toEqual([long]);
  });

  it("rejects 배포용(view-only distribution) documents instead of rendering garbage", async () => {
    const buf = buildCfb([
      {
        name: "FileHeader",
        data: buildFileHeader({ compressed: false, version: [0, 0, 1, 5], distribution: true }),
      },
      { name: "Section0", data: buildParagraph("암호화된 본문 자리") },
    ]);
    await expect(parseHwp(buf)).rejects.toThrow(/배포용/);
  });

  it("caps a runaway decompression (deflate bomb) and falls back to the raw bytes", async () => {
    // A fake DecompressionStream that "inflates" endlessly: without the output
    // cap parseHwp would accumulate unbounded chunks (this test would hang).
    class BombDS {
      readable = new ReadableStream<Uint8Array>({
        pull(controller) {
          controller.enqueue(new Uint8Array(1 << 20));
        },
      });
      writable = new WritableStream<Uint8Array>();
    }
    vi.stubGlobal("DecompressionStream", BombDS);
    try {
      const buf = buildCfb([
        { name: "FileHeader", data: buildFileHeader({ compressed: true, version: [0, 0, 1, 5] }) },
        { name: "Section0", data: buildParagraph("평문입니다") },
      ]);
      const d = await parseHwp(buf);
      // Aborted at the cap → the per-section fallback parses the raw bytes.
      expect(d.paragraphs).toEqual(["평문입니다"]);
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("keeps the preview alive when a stream flagged compressed is actually plaintext", async () => {
    // Some producers set the compressed bit but store plaintext records; the
    // failed inflate must fall back per-section instead of killing the preview.
    class FailingDS {
      readable = new ReadableStream<Uint8Array>({
        start(controller) {
          controller.error(new Error("invalid deflate data"));
        },
      });
      writable = new WritableStream<Uint8Array>();
    }
    vi.stubGlobal("DecompressionStream", FailingDS);
    try {
      const buf = buildCfb([
        { name: "FileHeader", data: buildFileHeader({ compressed: true, version: [0, 0, 1, 5] }) },
        { name: "Section0", data: buildParagraph("압축 표시지만 평문") },
      ]);
      const d = await parseHwp(buf);
      expect(d.paragraphs).toEqual(["압축 표시지만 평문"]);
    } finally {
      vi.unstubAllGlobals();
    }
  });

  // Real hancom files store each section as raw-deflate and then zero-pad the tail
  // to the CFB sector boundary. The strict DecompressionStream reports that padding
  // as "trailing junk" — but only AFTER emitting the full valid output (zlib ignores
  // it). Regression for the bug where the parser discarded that output and fell back
  // to the raw (still-compressed) bytes, so every real .hwp previewed as empty/뭉개진
  // 글자. Needs the platform Compression/DecompressionStream (skipped where absent).
  it.skipIf(!hasNativeInflate() || typeof CompressionStream !== "function")(
    "decodes a compressed section zero-padded after the deflate stream (real HWP layout)",
    async () => {
      const body = "한국형발사체 시험발사체 발사 예정입니다.";
      const compressed = await deflateRawViaPlatform(buildParagraph(body));
      const padded = concatBytes(compressed, new Uint8Array(48)); // CFB tail padding
      const buf = buildCfb([
        { name: "FileHeader", data: buildFileHeader({ compressed: true, version: [0, 0, 1, 5] }) },
        { name: "Section0", data: padded },
      ]);
      const d = await parseHwp(buf);
      expect(d.paragraphs).toEqual([body]);
    },
  );
});
