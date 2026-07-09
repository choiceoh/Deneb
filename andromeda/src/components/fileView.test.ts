import { describe, expect, it } from "vitest";
import { diffLineClass, isEditableKind, parseCsv, renderableBlob, textToBase64, viewKindFor } from "./fileView";

describe("renderableBlob", () => {
  it("re-stamps a mistyped PDF blob to application/pdf, preserving bytes", () => {
    const src = new Blob(["%PDF-1.5\nbody"], { type: "text/plain" });
    const out = renderableBlob(src, "pdf");
    expect(out.type).toBe("application/pdf");
    expect(out.size).toBe(src.size);
  });

  it("re-stamps a typeless PDF blob (fetch dropped the header)", () => {
    const out = renderableBlob(new Blob(["%PDF"]), "pdf");
    expect(out.type).toBe("application/pdf");
  });

  it("leaves an already-correct PDF blob untouched", () => {
    const src = new Blob(["%PDF"], { type: "application/pdf" });
    expect(renderableBlob(src, "pdf")).toBe(src);
  });

  it("does not touch non-PDF kinds (<img> sniffs its own bytes)", () => {
    const src = new Blob([new Uint8Array([1, 2, 3])], { type: "text/plain" });
    expect(renderableBlob(src, "image")).toBe(src);
  });
});

describe("viewKindFor", () => {
  it("routes by extension first, MIME as fallback", () => {
    expect(viewKindFor("견적서.pdf")).toBe("pdf");
    expect(viewKindFor("도면.PNG")).toBe("image");
    expect(viewKindFor("메모.md")).toBe("markdown");
    expect(viewKindFor("내역.csv")).toBe("csv");
    expect(viewKindFor("수정.patch")).toBe("diff");
    expect(viewKindFor("main.go")).toBe("text");
    expect(viewKindFor("이름없음", "image/webp")).toBe("image");
    expect(viewKindFor("이름없음", "application/pdf")).toBe("pdf");
    expect(viewKindFor("이름없음", "text/plain")).toBe("text");
    expect(viewKindFor("계약서.hwp")).toBe("hwp");
    expect(viewKindFor("자료.xlsx", "application/octet-stream")).toBe("none");
  });

  it("marks only text-family kinds editable", () => {
    expect(isEditableKind("markdown")).toBe(true);
    expect(isEditableKind("text")).toBe(true);
    expect(isEditableKind("csv")).toBe(true);
    expect(isEditableKind("diff")).toBe(true);
    expect(isEditableKind("pdf")).toBe(false);
    expect(isEditableKind("image")).toBe(false);
    expect(isEditableKind("hwp")).toBe(false); // extracted text — read-only
    expect(isEditableKind("none")).toBe(false);
  });
});

describe("parseCsv", () => {
  it("handles quotes, embedded commas/newlines, and CRLF", () => {
    const rows = parseCsv('이름,금액\n"탑솔라, 주식회사","1,000"\r\n"두\n줄",2');
    expect(rows).toEqual([
      ["이름", "금액"],
      ["탑솔라, 주식회사", "1,000"],
      ["두\n줄", "2"],
    ]);
  });

  it('unescapes "" and supports TSV', () => {
    expect(parseCsv('"그가 ""안녕""이라 말함"')).toEqual([['그가 "안녕"이라 말함']]);
    expect(parseCsv("a\tb\nc\td", "\t")).toEqual([
      ["a", "b"],
      ["c", "d"],
    ]);
  });
});

describe("diffLineClass", () => {
  it("classifies unified diff lines", () => {
    expect(diffLineClass("+added")).toBe("add");
    expect(diffLineClass("-removed")).toBe("del");
    expect(diffLineClass("@@ -1,3 +1,4 @@")).toBe("hunk");
    expect(diffLineClass("+++ b/file.go")).toBe("meta");
    expect(diffLineClass("--- a/file.go")).toBe("meta");
    expect(diffLineClass("diff --git a b")).toBe("meta");
    expect(diffLineClass(" context")).toBe("");
  });
});

describe("textToBase64", () => {
  it("round-trips UTF-8 (Korean) through base64", () => {
    const encoded = textToBase64("견적서 검토 — 1,000원");
    const decoded = new TextDecoder().decode(Uint8Array.from(atob(encoded), (c) => c.charCodeAt(0)));
    expect(decoded).toBe("견적서 검토 — 1,000원");
  });
});
