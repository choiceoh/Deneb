import { describe, expect, it } from "vitest";
import { Cfb } from "./cfb";
import { type HwpBlock, parseHwp } from "./hwp";
import { buildCfb, buildFileHeader, buildParagraph, buildTable, concatBytes, hwpWchars } from "./testfixtures";

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
});
