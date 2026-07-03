import { describe, expect, it } from "vitest";
import { Cfb } from "./cfb";
import { parseHwp } from "./hwp";
import { buildCfb, buildFileHeader, buildParaTextRecord, hwpWchars } from "./testfixtures";

// These tests exercise the parser against a HAND-BUILT compound file (no real
// HWP binary needed): a minimal CFB with a FileHeader + one uncompressed
// Section0 whose records we control. That covers the CFB reader, the record
// walker, and the control-char rules deterministically. Compression is left to
// the platform DecompressionStream at runtime (jsdom lacks it), so the fixtures
// use the uncompressed flag.

describe("Cfb", () => {
  it("reads named streams out of a synthetic compound file", () => {
    const cfb = new Cfb(
      buildCfb([
        { name: "FileHeader", data: new Uint8Array([1, 2, 3, 4]) },
        { name: "Section0", data: new Uint8Array([9, 9]) },
      ]),
    );
    expect(cfb.names()).toContain("FileHeader");
    expect(cfb.names()).toContain("Section0");
    expect(cfb.read("FileHeader")).toEqual(new Uint8Array([1, 2, 3, 4]));
    expect(cfb.read("Section0")).toEqual(new Uint8Array([9, 9]));
    expect(cfb.read("Nope")).toBeNull();
  });

  it("rejects a non-compound file", () => {
    expect(() => new Cfb(new Uint8Array([0, 1, 2, 3, 4, 5, 6, 7, 8]).buffer)).toThrow(/compound/);
  });
});

describe("parseHwp", () => {
  it("extracts paragraph text (incl. table-cell paragraphs) from an uncompressed doc", async () => {
    const section = concat(
      buildParaTextRecord("탑솔라 견적서"),
      buildParaTextRecord("품목: 태양광 모듈"),
      buildParaTextRecord("금액: 1,200,000원"),
    );
    const buf = buildCfb([
      { name: "FileHeader", data: buildFileHeader({ compressed: false, version: [0, 0, 1, 5] }) },
      { name: "Section0", data: section },
    ]);

    const doc = await parseHwp(buf);
    expect(doc.version).toBe("5.1.0.0");
    expect(doc.paragraphs).toEqual(["탑솔라 견적서", "품목: 태양광 모듈", "금액: 1,200,000원"]);
    expect(doc.text).toContain("탑솔라 견적서");
    expect(doc.text).toContain("금액: 1,200,000원");
  });

  it("skips inline/extended controls but keeps tabs and breaks", async () => {
    // WCHARs: "가" <tab=9 (8 wchars)> "나" <extended=11 (8 wchars)> "다" <break=10> "라"
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
    const section = buildParaTextRecord(wchars);
    const buf = buildCfb([
      { name: "FileHeader", data: buildFileHeader({ compressed: false, version: [0, 0, 1, 5] }) },
      { name: "Section0", data: section },
    ]);

    const doc = await parseHwp(buf);
    // 나 keeps, extended control gone, tab preserved, break -> newline.
    expect(doc.paragraphs[0]).toBe("가\t나다\n라");
  });

  it("errors clearly on a non-HWP compound file", async () => {
    const buf = buildCfb([{ name: "FileHeader", data: new Uint8Array(40) }]);
    await expect(parseHwp(buf)).rejects.toThrow(/not an HWP/);
  });
});

function concat(...parts: Uint8Array[]): Uint8Array {
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
