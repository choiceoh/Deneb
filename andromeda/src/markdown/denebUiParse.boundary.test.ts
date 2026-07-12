import { describe, expect, it } from "vitest";

import { coerce, collectInputs, hasInteractiveNode, parseDenebUi, splitDenebUi } from "./denebUiParse";

describe("splitDenebUi boundaries", () => {
  it("returns a single markdown segment when no fence exists", () => {
    expect(splitDenebUi("hello\nworld")).toEqual([{ kind: "md", text: "hello\nworld" }]);
    expect(splitDenebUi("")).toEqual([]);
  });

  it("preserves markdown before, between, and after complete UI fences", () => {
    const input = [
      "before",
      "```deneb-ui",
      "<text>one</text>",
      "```",
      "between",
      "``` DENEB-UI ",
      '{"type":"text","value":"two"}',
      "```   ",
      "after",
    ].join("\n");
    expect(splitDenebUi(input)).toEqual([
      { kind: "md", text: "before" },
      { kind: "ui", body: "<text>one</text>" },
      { kind: "md", text: "between" },
      { kind: "ui", body: '{"type":"text","value":"two"}' },
      { kind: "md", text: "after" },
    ]);
  });

  it("drops whitespace-only markdown around a fence", () => {
    expect(splitDenebUi(" \n```deneb-ui\n<text>x</text>\n```\n  ")).toEqual([{ kind: "ui", body: "<text>x</text>" }]);
  });

  it("returns only the pending block after an unclosed opener", () => {
    expect(splitDenebUi("intro\n```deneb-ui\n<card>partial\nwould still be body")).toEqual([
      { kind: "md", text: "intro" },
      { kind: "ui-pending", body: "<card>partial\nwould still be body" },
    ]);
  });

  it("does not treat a language prefix as the deneb-ui fence", () => {
    const input = "```deneb-ui-extra\n<text>x</text>\n```";
    expect(splitDenebUi(input)).toEqual([{ kind: "md", text: input }]);
  });

  it("recognizes an opener glued to a prose tail and keeps the prose", () => {
    expect(splitDenebUi("소스들을 다시 가져올게요.```deneb-ui\n<text>hi</text>\n```\n끝.")).toEqual([
      { kind: "md", text: "소스들을 다시 가져올게요." },
      { kind: "ui", body: "<text>hi</text>" },
      { kind: "md", text: "끝." },
    ]);
  });

  it("leaves mid-sentence fence mentions as prose", () => {
    const input = "```deneb-ui 펜스는 이렇게 씁니다\n본문";
    expect(splitDenebUi(input)).toEqual([{ kind: "md", text: input }]);
  });

  it("splits a closer glued to an HTML body tail", () => {
    expect(splitDenebUi("카드.```deneb-ui\n<text>hi</text>```\n뒤 프로즈는 카드 밖.")).toEqual([
      { kind: "md", text: "카드." },
      { kind: "ui", body: "<text>hi</text>" },
      { kind: "md", text: "뒤 프로즈는 카드 밖." },
    ]);
  });

  it("handles a one-liner fence with prose on both sides", () => {
    expect(splitDenebUi("프로즈.```deneb-ui<text>hi</text>``` 끝.")).toEqual([
      { kind: "md", text: "프로즈." },
      { kind: "ui", body: "<text>hi</text>" },
      { kind: "md", text: " 끝." },
    ]);
  });

  it("keeps the strict own-line close for legacy JSON bodies", () => {
    // JSON string values may legitimately contain ``` (markdown values).
    const body = '{"type":"markdown","value":"a```b"}';
    expect(splitDenebUi("```deneb-ui\n" + body + "\n```")).toEqual([{ kind: "ui", body }]);
  });

  it("accepts a four-backtick close line", () => {
    expect(splitDenebUi('```deneb-ui\n{"type":"divider"}\n````')).toEqual([{ kind: "ui", body: '{"type":"divider"}' }]);
  });
});

describe("parseDenebUi legacy boundaries", () => {
  it.each(["", "   ", "\n\t"])("returns null for blank body %j", (body) => {
    expect(parseDenebUi(body)).toBeNull();
  });

  it("returns object JSON directly", () => {
    const value = { type: "card", children: [{ type: "text", value: "hello" }] };
    expect(parseDenebUi(JSON.stringify(value))).toEqual(value);
  });

  it("wraps a bare array in a column", () => {
    expect(parseDenebUi('[{"type":"text","value":"a"},{"type":"text","value":"b"}]')).toEqual({
      type: "column",
      children: [
        { type: "text", value: "a" },
        { type: "text", value: "b" },
      ],
    });
  });

  it.each(["null", "true", "false", "1", '"text"'])("rejects scalar JSON %j", (body) => {
    expect(parseDenebUi(body)).toBeNull();
  });

  it("parses blank-line-tolerant NDJSON into a column", () => {
    expect(parseDenebUi('{"type":"text","value":"a"}\n\n {"type":"divider"} ')).toEqual({
      type: "column",
      children: [{ type: "text", value: "a" }, { type: "divider" }],
    });
  });

  it.each(["not json", '{"type":"text"}\nbroken', "{", "[", '{"type":"text",}'])(
    "rejects malformed legacy payload %j",
    (body) => {
      expect(parseDenebUi(body)).toBeNull();
    },
  );

  it("routes leading HTML after trimming", () => {
    expect(parseDenebUi(" \n <text id='greeting'> hello </text> ")).toEqual({
      type: "text",
      id: "greeting",
      value: "hello",
    });
  });

  it("returns null when HTML has no renderable roots", () => {
    expect(parseDenebUi("<br><unknown></unknown>")).toBeNull();
  });
});

describe("interactive tree detection", () => {
  it.each([
    "button",
    "text_input",
    "date_input",
    "time_input",
    "checkbox",
    "select",
    "switch",
    "slider",
    "radio_group",
    "chip_group",
    "countdown",
  ])("recognizes %s", (type) => {
    expect(hasInteractiveNode({ type })).toBe(true);
  });

  it.each(["text", "markdown", "badge", "image", "chart", "table", "progress"])("keeps %s display-only", (type) => {
    expect(hasInteractiveNode({ type })).toBe(false);
  });

  it("walks arrays, children, list items, and tab children", () => {
    expect(hasInteractiveNode([null, { type: "text" }, { type: "button" }])).toBe(true);
    expect(hasInteractiveNode({ type: "card", children: [{ type: "text_input" }] })).toBe(true);
    expect(hasInteractiveNode({ type: "list", items: [{ type: "switch" }] })).toBe(true);
    expect(hasInteractiveNode({ type: "tabs", tabs: [{ children: [{ type: "slider" }] }] })).toBe(true);
  });

  it("tolerates primitive and malformed nested values", () => {
    expect(hasInteractiveNode(null)).toBe(false);
    expect(hasInteractiveNode("button")).toBe(false);
    expect(hasInteractiveNode(42)).toBe(false);
    expect(hasInteractiveNode({ type: "card", children: "button", tabs: [null, {}] })).toBe(false);
  });
});

describe("form collection boundaries", () => {
  it("walks an array root", () => {
    expect(
      collectInputs([
        { type: "text_input", id: "name", value: "Ada" },
        { type: "checkbox", id: "ready", checked: true },
      ]).initial,
    ).toEqual({ name: "Ada", ready: true });
  });

  it("uses the last encountered duplicate id deterministically", () => {
    const result = collectInputs({
      type: "column",
      children: [
        { type: "text_input", id: "same", value: "first" },
        { type: "select", id: "same", selected: "second" },
      ],
    });
    expect(result.initial).toEqual({ same: "second" });
  });

  it("collects required ids even when the node type is display-only", () => {
    const result = collectInputs({ type: "text", id: "display", required: true, value: "ignored" });
    expect(result.initial).toEqual({});
    expect([...result.required]).toEqual(["display"]);
  });

  it("does not mistake truthy non-boolean values for required or checked", () => {
    const result = collectInputs({
      type: "column",
      children: [
        { type: "checkbox", id: "check", checked: "true", required: "true" },
        { type: "switch", id: "switch", checked: 1 },
      ],
    });
    expect(result.initial).toEqual({ check: false, switch: false });
    expect(result.required.size).toBe(0);
  });

  it("preserves scalar slider defaults including zero", () => {
    expect(
      collectInputs({
        type: "column",
        children: [
          { type: "slider", id: "zero", value: 0, min: 5 },
          { type: "slider", id: "negative", min: -10 },
        ],
      }).initial,
    ).toEqual({ zero: 0, negative: -10 });
  });

  it("ignores unknown control types without stopping descendant traversal", () => {
    const result = collectInputs({
      type: "custom",
      id: "outer",
      children: [{ type: "text_input", id: "inner", value: "found" }],
    });
    expect(result.initial).toEqual({ inner: "found" });
  });
});

describe("coerce boundaries", () => {
  it.each([
    [NaN, "NaN"],
    [Infinity, "Infinity"],
    [-0, "0"],
    [" already text ", " already text "],
    [{ id: 1 }, "[object Object]"],
  ])("uses JavaScript string semantics for %j", (value, expected) => {
    expect(coerce(value)).toBe(expected);
  });

  it("joins mixed arrays using the native callback separator", () => {
    expect(coerce(["a", 2, false, null])).toBe("a, 2, false, ");
  });
});
