// Direct AST tests for the GFM block parser. The renderer (components/Markdown)
// is tested separately; this locks the grammar itself — the file's own reason
// for existing apart from React ("so the GFM grammar is unit-testable directly").
import { describe, expect, it } from "vitest";

import { parseBlocks } from "./parse";

describe("headings", () => {
  it("when maps # depth to level and trims trailing hashes", () => {
    expect(parseBlocks("# Title")).toEqual([{ type: "heading", level: 1, text: "Title" }]);
    expect(parseBlocks("###### Deep")).toEqual([{ type: "heading", level: 6, text: "Deep" }]);
    expect(parseBlocks("## Closed ##")).toEqual([{ type: "heading", level: 2, text: "Closed" }]);
  });
  it("when needs a space after the hashes (else it is a paragraph)", () => {
    expect(parseBlocks("#NoSpace")).toEqual([{ type: "para", text: "#NoSpace" }]);
  });
});

describe("code fences", () => {
  it("when captures language and body verbatim", () => {
    expect(parseBlocks("```go\nfmt.Println(1)\n```")).toEqual([{ type: "code", lang: "go", text: "fmt.Println(1)" }]);
  });
  it("accepts ~~~ fences and empty lang", () => {
    expect(parseBlocks("~~~\nplain\n~~~")).toEqual([{ type: "code", lang: "", text: "plain" }]);
  });
  it("runs an unclosed fence to EOF (stream truncation)", () => {
    expect(parseBlocks("```\nno close")).toEqual([{ type: "code", lang: "", text: "no close" }]);
  });
  it("without interpret markup inside a fence", () => {
    expect(parseBlocks("```\n# not a heading\n- not a list\n```")).toEqual([
      { type: "code", lang: "", text: "# not a heading\n- not a list" },
    ]);
  });
});

describe("horizontal rules", () => {
  it("when recognizes -, *, _ variants including spaced", () => {
    expect(parseBlocks("---")).toEqual([{ type: "hr" }]);
    expect(parseBlocks("***")).toEqual([{ type: "hr" }]);
    expect(parseBlocks("___")).toEqual([{ type: "hr" }]);
    expect(parseBlocks("- - -")).toEqual([{ type: "hr" }]);
  });
});

describe("tables", () => {
  it("parses header, alignment, and rows", () => {
    const [block] = parseBlocks("| a | b | c |\n|:--|:-:|--:|\n| 1 | 2 | 3 |");
    expect(block).toEqual({
      type: "table",
      header: ["a", "b", "c"],
      align: ["left", "center", "right"],
      rows: [["1", "2", "3"]],
    });
  });
  it("when treats a bare column as no alignment", () => {
    const [block] = parseBlocks("a|b\n-|-\nx|y");
    expect(block).toMatchObject({ header: ["a", "b"], align: [undefined, undefined], rows: [["x", "y"]] });
  });
  it("preserves an escaped pipe as literal cell content", () => {
    const [block] = parseBlocks("| x |\n|---|\n| a \\| b |");
    expect(block).toMatchObject({ rows: [["a | b"]] });
  });
  it("is a paragraph without the separator row", () => {
    expect(parseBlocks("| a | b |\n| 1 | 2 |")[0].type).toBe("para");
  });
});

describe("blockquotes", () => {
  it("strips the marker and parses the inner text as blocks", () => {
    expect(parseBlocks("> hello\n> world")).toEqual([
      { type: "quote", children: [{ type: "para", text: "hello\nworld" }] },
    ]);
  });
  it("when recurses so a quoted list stays a list", () => {
    const [quote] = parseBlocks("> - a\n> - b");
    expect(quote).toMatchObject({ type: "quote", children: [{ type: "list", ordered: false }] });
    expect((quote as { children: { items: unknown[] }[] }).children[0].items).toHaveLength(2);
  });
});

describe("lists", () => {
  it("parses an unordered list", () => {
    const [list] = parseBlocks("- a\n- b");
    expect(list).toMatchObject({ type: "list", ordered: false, start: undefined });
    expect((list as { items: { children: unknown }[] }).items).toHaveLength(2);
  });
  it("captures ordered start number", () => {
    expect(parseBlocks("3. a\n4. b")[0]).toMatchObject({ type: "list", ordered: true, start: 3 });
  });
  it("marks task-list items with checked state", () => {
    const [list] = parseBlocks("- [ ] todo\n- [x] done");
    const items = (list as { items: { task?: boolean; checked?: boolean; children: { text: string }[] }[] }).items;
    expect(items[0]).toMatchObject({ task: true, checked: false });
    expect(items[1]).toMatchObject({ task: true, checked: true });
    expect(items[1].children[0]).toMatchObject({ type: "para", text: "done" });
  });
  it("when nests an indented child list via recursion", () => {
    const [list] = parseBlocks("- a\n  - b");
    const item0 = (list as { items: { children: { type: string }[] }[] }).items[0];
    expect(item0.children[0]).toMatchObject({ type: "para", text: "a" });
    expect(item0.children[1]).toMatchObject({ type: "list", ordered: false });
  });
  it("preserves a loose list open across a blank line between siblings", () => {
    const [list] = parseBlocks("- a\n\n- b");
    expect((list as { items: unknown[] }).items).toHaveLength(2);
  });
  it("ends the list when the next line dedents to a paragraph", () => {
    const blocks = parseBlocks("- a\n\nafter");
    expect(blocks.map((b) => b.type)).toEqual(["list", "para"]);
  });
});

describe("block math", () => {
  it("parses a single-line $$…$$", () => {
    expect(parseBlocks("$$x^2$$")).toEqual([{ type: "mathBlock", text: "x^2" }]);
  });
  it("parses a multi-line $$ block", () => {
    expect(parseBlocks("$$\na + b\n$$")).toEqual([{ type: "mathBlock", text: "a + b" }]);
  });
});

describe("paragraphs", () => {
  it("when joins consecutive lines and splits on a blank line", () => {
    expect(parseBlocks("one\ntwo\n\nthree")).toEqual([
      { type: "para", text: "one\ntwo" },
      { type: "para", text: "three" },
    ]);
  });
  it("stops a paragraph at the start of another block", () => {
    expect(parseBlocks("text\n# heading")).toEqual([
      { type: "para", text: "text" },
      { type: "heading", level: 1, text: "heading" },
    ]);
  });
  it("normalizes CRLF and ignores leading/trailing blank lines", () => {
    expect(parseBlocks("\r\n\r\nhi\r\n\r\n")).toEqual([{ type: "para", text: "hi" }]);
  });
  it("returns nothing for empty input", () => {
    expect(parseBlocks("")).toEqual([]);
    expect(parseBlocks("   \n\n")).toEqual([]);
  });
});
