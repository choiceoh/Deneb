import { describe, expect, it } from "vitest";
import { normalizeBoxTables, normalizeHtmlBlocks, normalizeMarkdown, normalizePipeTables } from "./normalize";
import { parseBlocks } from "./parse";

describe("normalizeHtmlBlocks", () => {
  it("converts headings, hr, lists, quotes", () => {
    expect(normalizeHtmlBlocks("<h2>제목</h2>")).toBe("## 제목");
    expect(normalizeHtmlBlocks("<hr/>")).toBe("---");
    expect(normalizeHtmlBlocks("<ul>\n<li>첫째</li>\n<li>둘째</li>\n</ul>")).toBe("- 첫째\n- 둘째");
    expect(normalizeHtmlBlocks('<ol start="3">\n<li>x</li>\n<li>y</li>\n</ol>')).toBe("3. x\n4. y");
    expect(normalizeHtmlBlocks("<blockquote>인용</blockquote>")).toBe("> 인용");
  });
  it("leaves fenced HTML and mid-line tags alone", () => {
    const src = "```\n<h1>예시</h1>\n```";
    expect(normalizeHtmlBlocks(src)).toBe(src);
    expect(normalizeHtmlBlocks("보세요 <p>중요</p> 입니다")).toBe("보세요 <p>중요</p> 입니다");
  });
});

describe("normalizePipeTables", () => {
  it("inserts a missing delimiter into a bordered pipe table", () => {
    const lines = normalizePipeTables("| Track | Status |\n| 물품조달 | 진행 |\n| 전기공사 | 도면완료 |").split("\n");
    expect(lines[0]).toBe("| Track | Status |");
    expect(lines[1]).toBe("| --- | --- |");
    expect(lines[2]).toBe("| 물품조달 | 진행 |");
  });
  it("leaves genuine tables and prose pipes untouched", () => {
    const md = "| a | b |\n| --- | --- |\n| 1 | 2 |";
    expect(normalizePipeTables(md)).toBe(md);
    expect(normalizePipeTables("사과 | 오렌지\n포도 | 바나나")).toBe("사과 | 오렌지\n포도 | 바나나");
  });
  it("unwraps a bare-fenced markdown table", () => {
    const md = normalizePipeTables("```\n| A | B |\n| --- | --- |\n| 1 | 2 |\n```");
    expect(md).not.toContain("```");
    expect(parseBlocks(md)[0].type).toBe("table");
  });
});

describe("normalizeBoxTables", () => {
  it("converts a box-drawing table to GFM", () => {
    const box = [
      "┌────────┬────────┐",
      "│ Track  │ Status │",
      "├────────┼────────┤",
      "│ 물품조달 │ 진행   │",
      "└────────┴────────┘",
    ].join("\n");
    const lines = normalizeBoxTables(box).split("\n");
    expect(lines[0]).toBe("| Track | Status |");
    expect(lines[1]).toBe("| --- | --- |");
    expect(lines[2]).toBe("| 물품조달 | 진행 |");
  });
});

describe("normalizeMarkdown", () => {
  it("chains footnotes → html → box → pipe so separator-less tables parse", () => {
    const blocks = parseBlocks(normalizeMarkdown("| A | B |\n| 1 | 2 |"));
    expect(blocks[0].type).toBe("table");
  });

  it("rewrites footnotes before later passes", () => {
    const out = normalizeMarkdown("본문[^a]\n\n[^a]: 각주");
    expect(out).toContain("¹");
    expect(out).toContain("각주");
    expect(out).not.toContain("[^a]");
  });
});
