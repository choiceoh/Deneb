import { describe, expect, it } from "vitest";

import {
  canonBadgeColor,
  canonChartType,
  canonSeverity,
  canonTextStyle,
  decodeEntities,
  indexOfCloseTag,
  isMarkdownBullet,
  isMarkdownHeading,
  isNameChar,
  isNameStart,
  isSpace,
  lenientFloat,
  looksLikeMarkdownBlock,
  textBlockNode,
  truthy,
} from "./denebUiHtmlHelpers";

describe("markdown block recognition", () => {
  it.each([
    ["# heading", true],
    ["###### heading", true],
    ["####### heading", false],
    ["#missing-space", false],
    ["plain # marker", false],
    ["", false],
  ])("recognizes heading %j", (input, expected) => {
    expect(isMarkdownHeading(input)).toBe(expected);
  });

  it.each([
    ["- item", true],
    ["* item", true],
    ["• item", true],
    ["1. item", true],
    ["20) item", true],
    ["-item", false],
    ["1 item", false],
    ["item", false],
  ])("recognizes bullet %j", (input, expected) => {
    expect(isMarkdownBullet(input)).toBe(expected);
  });

  it("requires repeated list or pipe rows", () => {
    expect(looksLikeMarkdownBlock("- one")).toBe(false);
    expect(looksLikeMarkdownBlock("- one\n- two")).toBe(true);
    expect(looksLikeMarkdownBlock("| one | two |")).toBe(false);
    expect(looksLikeMarkdownBlock("| one | two |\n| --- | --- |")).toBe(true);
  });

  it("recognizes fenced code and headings anywhere in the run", () => {
    expect(looksLikeMarkdownBlock("before\n```ts\nconst x = 1\n```\nafter")).toBe(true);
    expect(looksLikeMarkdownBlock("before\n## details\nafter")).toBe(true);
  });

  it("chooses the correct implicit node kind and trims the run", () => {
    expect(textBlockNode("  ordinary text  ")).toEqual({ type: "text", value: "ordinary text" });
    expect(textBlockNode("\n- first\n- second\n")).toEqual({
      type: "markdown",
      value: "- first\n- second",
    });
  });
});

describe("close-tag search", () => {
  it("finds an ASCII tag without changing Unicode indexes", () => {
    const source = "앞İ쪽<code>본문</CoDe>tail";
    expect(indexOfCloseTag(source, source.indexOf("본문"), "code")).toBe(source.indexOf("</CoDe>"));
  });

  it("honors the starting offset", () => {
    const source = "</code> first </CODE> second";
    expect(indexOfCloseTag(source, 1, "code")).toBe(source.indexOf("</CODE>"));
  });

  it("does not accept a longer tag name as the requested close tag", () => {
    expect(indexOfCloseTag("text</codebase>tail", 0, "code")).toBe(-1);
  });

  it.each(["no tag", "<code>open", "</cod>", "< /code>"])("returns -1 for %j", (source) => {
    expect(indexOfCloseTag(source, 0, "code")).toBe(-1);
  });
});

describe("truthy", () => {
  it.each([
    ["false", false],
    [" FALSE ", false],
    ["0", false],
    ["no", false],
    ["OFF", false],
    ["true", true],
    ["1", true],
    ["yes", true],
    ["", true],
    ["unexpected", true],
  ])("maps %j to %s", (input, expected) => {
    expect(truthy(input)).toBe(expected);
  });
});

describe("lenientFloat", () => {
  it.each([
    [undefined, undefined],
    ["", undefined],
    ["   ", undefined],
    ["12", 12],
    ["-12.5", -12.5],
    [".5", 0.5],
    ["1,234.50kg", 1234.5],
    ["약 -42.75%", -42.75],
    ["16px", 16],
    ["USD 9,000", 9000],
    ["none", undefined],
    ["1.2.3", 1.2],
    ["10.", 10],
  ])("extracts %j", (input, expected) => {
    expect(lenientFloat(input)).toBe(expected);
  });

  it("keeps exact exponential notation", () => {
    expect(lenientFloat("1e3")).toBe(1000);
    expect(lenientFloat("-2E-2")).toBe(-0.02);
  });
});

describe("enum canonicalization", () => {
  it.each([
    ["red", "error"],
    ["GREEN", "success"],
    [" amber ", "warning"],
    ["orange", "warning"],
    ["blue", "primary"],
    ["grey", "secondary"],
    ["custom", "custom"],
    [undefined, undefined],
  ])("canonicalizes badge color %j", (input, expected) => {
    expect(canonBadgeColor(input)).toBe(expected);
  });

  it.each([
    ["warn", "warning"],
    ["caution", "warning"],
    ["critical", "error"],
    ["fatal", "error"],
    ["ok", "success"],
    ["done", "success"],
    ["notice", "info"],
    ["information", "info"],
    ["custom", "custom"],
  ])("canonicalizes severity %j", (input, expected) => {
    expect(canonSeverity(input)).toBe(expected);
  });

  it.each([
    ["bars", "bar"],
    ["column", "bar"],
    ["columns", "bar"],
    ["lines", "line"],
    ["area", "line"],
    ["trend", "line"],
    ["pie", "pie"],
  ])("canonicalizes chart type %j", (input, expected) => {
    expect(canonChartType(input)).toBe(expected);
  });

  it.each([
    ["heading", "headline"],
    ["header", "headline"],
    ["subtitle", "title"],
    ["subheading", "title"],
    ["caption", "caption"],
  ])("canonicalizes text style %j", (input, expected) => {
    expect(canonTextStyle(input)).toBe(expected);
  });
});

describe("token character helpers", () => {
  it.each([
    [" ", true],
    ["\t", true],
    ["\n", true],
    ["\r", true],
    ["a", false],
    ["\u00a0", false],
  ])("classifies space %j", (input, expected) => {
    expect(isSpace(input)).toBe(expected);
  });

  it.each([
    ["a", true],
    ["Z", true],
    ["0", false],
    ["-", false],
    ["_", false],
    ["한", false],
  ])("classifies name start %j", (input, expected) => {
    expect(isNameStart(input)).toBe(expected);
  });

  it.each([
    ["a", true],
    ["Z", true],
    ["0", true],
    ["-", true],
    ["_", true],
    [":", false],
    ["한", false],
  ])("classifies name continuation %j", (input, expected) => {
    expect(isNameChar(input)).toBe(expected);
  });
});

describe("entity decoding", () => {
  it("decodes named entities case-insensitively", () => {
    expect(decodeEntities("&lt;&GT;&amp;&quot;&apos;&nbsp;")).toBe(`<>&"' `);
  });

  it("decodes decimal and hexadecimal numeric references", () => {
    expect(decodeEntities("&#65;&#x42;&#X1F600;")).toBe("AB😀");
  });

  it("leaves malformed and unknown entities intact", () => {
    expect(decodeEntities("a &unknown; b &amp c &#; d &toolongentity; e")).toBe(
      "a &unknown; b &amp c &#; d &toolongentity; e",
    );
  });

  it("does not throw for an out-of-range numeric reference", () => {
    expect(() => decodeEntities("before &#99999999; after")).not.toThrow();
    expect(decodeEntities("before &#99999999; after")).toBe("before &#99999999; after");
  });

  it("handles consecutive entities and ordinary ampersands", () => {
    expect(decodeEntities("A&B &amp;&amp; C")).toBe("A&B && C");
  });

  it("returns text without ampersands unchanged", () => {
    const input = "plain 한글 text";
    expect(decodeEntities(input)).toBe(input);
  });
});
