import { describe, expect, it } from "vitest";

import { parseErpText, type ErpBlock } from "@/erpText";

// Real snapshot shapes, captured from the live gateway 2026-07-26.
const STOCK = `현재고 요약 (구매/자재 · Amaranth · 품목 집계)
기준연도: 2026 · 필터: 모듈·인버터(기본 — 전체는 query=전체)
원본행: 113 · 집계품목: 103 · 재고수량합: 158,590

재고 상위(창고 합산):
1. LR7-72HGD-615Ma · 재고 42,284 · 가용 42,284 · 모듈 · M-LR0615-03
2. JKM635N-78HL4-BDV-S · 재고 22,515 · 가용 22,515 · 모듈 · M-JK0635-01

출처: POST /purchase/pom0010/0pu00000 · searchType=coCd · 품목코드별 합산`;

const SALES = `매출
기간: 2026 YTD (2026-01-01 ~ 2026-07-26)
건수: 2,192
매출: 3,334억 5,028만 8,281원`;

const kinds = (bs: ErpBlock[]) => bs.map((b) => b.kind);

describe("parseErpText", () => {
  it("separates title, pair strip, section, ranked rows and the source note", () => {
    expect(kinds(parseErpText(STOCK))).toEqual(["title", "meta", "section", "row", "row", "note"]);
  });

  it("keeps a value that contains a BARE '·' whole", () => {
    // The field separator is " · " (spaced). "모듈·인버터" uses the bare form, so
    // splitting on the bare character would tear one filter value into two.
    const meta = parseErpText(STOCK).find((b) => b.kind === "meta");
    if (meta?.kind !== "meta") throw new Error("expected a meta strip");
    expect(meta.pairs).toEqual([
      { label: "기준연도", value: "2026" },
      { label: "필터", value: "모듈·인버터(기본 — 전체는 query=전체)" },
      { label: "원본행", value: "113" },
      { label: "집계품목", value: "103" },
      { label: "재고수량합", value: "158,590" },
    ]);
  });

  it("splits a ranked row into its leading name and the remaining fields", () => {
    const row = parseErpText(STOCK).find((b) => b.kind === "row");
    if (row?.kind !== "row") throw new Error("expected a row");
    expect(row.rank).toBe("1");
    expect(row.name).toBe("LR7-72HGD-615Ma");
    expect(row.fields).toEqual(["재고 42,284", "가용 42,284", "모듈", "M-LR0615-03"]);
  });

  it("folds consecutive single-pair lines into one strip", () => {
    // 매출 reports 기간/건수/매출 on three separate lines that are one summary;
    // left as separate blocks they would render as three stacked strips.
    const blocks = parseErpText(SALES);
    expect(kinds(blocks)).toEqual(["title", "meta"]);
    if (blocks[1].kind !== "meta") throw new Error("expected a meta strip");
    expect(blocks[1].pairs.map((p) => p.label)).toEqual(["기간", "건수", "매출"]);
    expect(blocks[1].pairs[2].value).toBe("3,334억 5,028만 8,281원");
  });

  it("drops a section header that has no rows under it", () => {
    // Truncation at the row limit leaves a trailing "거래처 상위:" with nothing
    // beneath it — a header the reader cannot use.
    const truncated = `발주현황 요약\n라인: 11\n\n금액 상위(품목 합산):\n1. HS645AE · 16억원\n\n거래처 상위:`;
    expect(kinds(parseErpText(truncated))).toEqual(["title", "meta", "section", "row"]);
  });

  it("keeps an unrecognized line instead of dropping it", () => {
    const blocks = parseErpText(`제목\n설명 없는 자유 문장입니다`);
    expect(kinds(blocks)).toEqual(["title", "note"]);
    if (blocks[1].kind !== "note") throw new Error("expected a note");
    expect(blocks[1].text).toBe("설명 없는 자유 문장입니다");
  });

  it("returns nothing for blank input", () => {
    expect(parseErpText("")).toEqual([]);
    expect(parseErpText("   \n  \n")).toEqual([]);
  });
});
