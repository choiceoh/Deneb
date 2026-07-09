// Form helpers for deneb-ui trees: value coercion + the input-seeding walk.
// The parser/grammar is covered by denebUiHtml.test.ts; this covers the two
// pure functions that turn a parsed form into initial state + required-id set
// and serialize a value the way the callback (native dataAsStrings) expects.
import { describe, expect, it } from "vitest";

import { coerce, collectInputs } from "./denebUiParse";

describe("coerce", () => {
  it("maps null/undefined to an empty string", () => {
    expect(coerce(null)).toBe("");
    expect(coerce(undefined)).toBe("");
  });
  it("joins arrays with a comma-space (multi-select)", () => {
    expect(coerce(["a", "b", "c"])).toBe("a, b, c");
    expect(coerce([])).toBe("");
  });
  it("stringifies booleans and numbers", () => {
    expect(coerce(true)).toBe("true");
    expect(coerce(false)).toBe("false");
    expect(coerce(0)).toBe("0");
    expect(coerce(42)).toBe("42");
  });
});

describe("collectInputs", () => {
  it("seeds text/date/time inputs from value, defaulting to empty", () => {
    const { initial } = collectInputs({
      type: "column",
      children: [
        { type: "text_input", id: "name", value: "김" },
        { type: "date_input", id: "when" },
        { type: "time_input", id: "at", value: "09:00" },
      ],
    });
    expect(initial).toEqual({ name: "김", when: "", at: "09:00" });
  });
  it("seeds select/radio from selected and checkbox/switch as booleans", () => {
    const { initial } = collectInputs({
      type: "column",
      children: [
        { type: "select", id: "s", selected: "b" },
        { type: "radio_group", id: "r" },
        { type: "checkbox", id: "c", checked: true },
        { type: "switch", id: "w" },
      ],
    });
    expect(initial).toEqual({ s: "b", r: "", c: true, w: false });
  });
  it("seeds slider from value, then min, then 0", () => {
    const { initial } = collectInputs({
      type: "column",
      children: [
        { type: "slider", id: "a", value: 5, min: 1 },
        { type: "slider", id: "b", min: 2 },
        { type: "slider", id: "c" },
      ],
    });
    expect(initial).toEqual({ a: 5, b: 2, c: 0 });
  });
  it("seeds chip_group as an array only when multi-select", () => {
    const { initial } = collectInputs({
      type: "column",
      children: [
        { type: "chip_group", id: "multi", selection: "multi" },
        { type: "chip_group", id: "single" },
      ],
    });
    expect(initial).toEqual({ multi: [], single: "" });
  });
  it("collects required ids across the tree", () => {
    const { required } = collectInputs({
      type: "column",
      children: [
        { type: "text_input", id: "a", required: true },
        { type: "text_input", id: "b" },
        { type: "select", id: "c", required: true },
      ],
    });
    expect([...required].sort()).toEqual(["a", "c"]);
  });
  it("descends into children, items, and tab bodies", () => {
    const { initial } = collectInputs({
      type: "card",
      children: [{ type: "list", items: [{ type: "text_input", id: "deep", value: "x" }] }],
      tabs: [{ children: [{ type: "checkbox", id: "tabbed", checked: true }] }],
    });
    expect(initial).toEqual({ deep: "x", tabbed: true });
  });
  it("ignores nodes without a string id and tolerates junk", () => {
    const { initial, required } = collectInputs({
      type: "column",
      children: [
        { type: "text_input" },
        { type: "text_input", id: "" },
        null,
        "text",
        { type: "text", value: "no-id" },
      ],
    });
    expect(initial).toEqual({});
    expect(required.size).toBe(0);
  });
});
