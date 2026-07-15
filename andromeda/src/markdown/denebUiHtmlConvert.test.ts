import { describe, expect, it } from "vitest";

import { convert, newElem, type OpenElem, type Structural } from "./denebUiHtmlConvert";

function element(
  tag: string,
  attrs: Record<string, string> = {},
  text = "",
  children: Record<string, unknown>[] = [],
  structs: Structural[] = [],
): OpenElem {
  return { ...newElem(tag, attrs), text: [text], children, structs };
}

describe("convert layout and typography", () => {
  it.each([
    ["column", "column"],
    ["col", "column"],
    ["row", "row"],
    ["card", "card"],
  ])("maps %s to %s and preserves children", (tag, type) => {
    const children = [{ type: "text", value: "child" }];
    expect(convert(element(tag, { id: "root" }, "", children))).toEqual({ id: "root", type, children });
  });

  it("maps box alignment only when populated", () => {
    expect(convert(element("box", { align: "center" }))).toEqual({
      type: "box",
      contentAlignment: "center",
      children: [],
    });
    expect(convert(element("box"))).toEqual({ type: "box", children: [] });
  });

  it.each(["hr", "divider"])("when maps %s to a divider", (tag) => {
    expect(convert(element(tag, { id: "rule" }))).toEqual({ id: "rule", type: "divider" });
  });

  it("when maps text flags and canonical style", () => {
    expect(
      convert(element("text", { style: "heading", bold: "yes", italic: "0", color: "#123" }, "  hello  ")),
    ).toEqual({
      type: "text",
      value: "hello",
      style: "headline",
      bold: true,
      italic: false,
      color: "#123",
    });
  });

  it("when upgrades markdown-shaped text", () => {
    expect(convert(element("text", {}, "- one\n- two"))).toEqual({
      type: "markdown",
      value: "- one\n- two",
    });
  });

  it.each([
    ["h1", { style: "headline" }],
    ["h2", { style: "title" }],
    ["h3", { style: "title" }],
    ["h4", { bold: true }],
    ["p", {}],
  ])("when maps %s aliases", (tag, extra) => {
    expect(convert(element(tag, {}, "Title"))).toEqual({ type: "text", value: "Title", ...extra });
  });

  it("drops an empty paragraph but wraps mixed paragraph content", () => {
    expect(convert(element("p"))).toBeNull();
    expect(convert(element("p", {}, "prefix", [{ type: "badge", value: "new" }]))).toEqual({
      type: "column",
      children: [
        { type: "text", value: "prefix" },
        { type: "badge", value: "new" },
      ],
    });
  });
});

describe("convert media and display nodes", () => {
  it("maps images with both URL aliases and lenient dimensions", () => {
    expect(convert(element("img", { src: "a.png", alt: "preview", height: "240px", "aspect-ratio": "1.5" }))).toEqual({
      type: "image",
      url: "a.png",
      alt: "preview",
      height: 240,
      aspectRatio: 1.5,
    });
    expect(convert(element("image", { url: "b.png" }))).toEqual({ type: "image", url: "b.png" });
  });

  it("maps icon, code, quote, and markdown payloads", () => {
    expect(convert(element("icon", { name: "calendar", size: "16", color: "blue" }))).toEqual({
      type: "icon",
      name: "calendar",
      size: 16,
      color: "blue",
    });
    expect(convert(element("code", { lang: "go" }, "  fmt.Println()  "))).toEqual({
      type: "code",
      code: "fmt.Println()",
      language: "go",
    });
    expect(convert(element("quote", { source: "Ada" }, " insight "))).toEqual({
      type: "quote",
      text: "insight",
      source: "Ada",
    });
    expect(convert(element("markdown", {}, " # heading "))).toEqual({
      type: "markdown",
      value: "# heading",
    });
  });

  it("when maps badges, stats, and avatars", () => {
    expect(convert(element("badge", { color: "red" }, "late"))).toEqual({
      type: "badge",
      value: "late",
      color: "error",
    });
    expect(convert(element("stat", { label: "Total", value: "42", description: "items" }))).toEqual({
      type: "stat",
      label: "Total",
      value: "42",
      description: "items",
    });
    expect(convert(element("avatar", { name: "Ada", "image-url": "ada.png", size: "48" }))).toEqual({
      type: "avatar",
      name: "Ada",
      imageUrl: "ada.png",
      size: 48,
    });
  });

  it.each([
    ["68", 0.68],
    ["68%", 0.68],
    ["0.5", 0.5],
    ["-20", 0],
    ["400", 1],
    ["not-a-number", undefined],
  ])("normalizes progress value %j", (raw, expected) => {
    const node = convert(element("progress", { value: raw, label: "Load" })) as Record<string, unknown>;
    expect(node).toMatchObject({ type: "progress", label: "Load" });
    expect(node.value).toBe(expected);
  });

  it("when maps alerts and count-down defaults", () => {
    expect(convert(element("alert", { title: "Careful", severity: "critical" }, "message"))).toEqual({
      type: "alert",
      title: "Careful",
      severity: "error",
      message: "message",
    });
    expect(convert(element("countdown", {}, "Soon"))).toEqual({ type: "countdown", label: "Soon", seconds: 0 });
  });
});

describe("convert structural children", () => {
  it("builds a chart from point structs and ignores unrelated structs", () => {
    expect(
      convert(
        element(
          "chart",
          { type: "trend", label: "Revenue" },
          "",
          [],
          [
            { kind: "point", label: "Q1", value: 1 },
            { kind: "cell", text: "ignored", header: false },
            { kind: "point", label: "Q2", value: 2 },
          ],
        ),
      ),
    ).toEqual({ type: "chart", chartType: "line", label: "Revenue", labels: ["Q1", "Q2"], values: [1, 2] });
    expect(convert(element("point", { label: "Q3", value: "3.5kg" }))).toEqual({
      kind: "point",
      label: "Q3",
      value: 3.5,
    });
  });

  it("when promotes the first header row and retains subsequent rows", () => {
    const header: Structural = {
      kind: "tr",
      cells: [
        { text: "Name", header: true },
        { text: "Value", header: true },
      ],
    };
    const row: Structural = {
      kind: "tr",
      cells: [
        { text: "A", header: false },
        { text: "1", header: false },
      ],
    };
    expect(convert(element("table", {}, "", [], [header, row]))).toEqual({
      type: "table",
      headers: ["Name", "Value"],
      rows: [["A", "1"]],
    });
  });

  it("when maps table row and cell structural records", () => {
    const cells: Structural[] = [
      { kind: "cell", text: "A", header: true },
      { kind: "cell", text: "B", header: false },
      { kind: "point", label: "ignored", value: 1 },
    ];
    expect(convert(element("tr", {}, "", [], cells))).toEqual({ kind: "tr", cells: cells.slice(0, 2) });
    expect(convert(element("th", {}, " Header "))).toEqual({ kind: "cell", text: "Header", header: true });
    expect(convert(element("td", {}, " Value "))).toEqual({ kind: "cell", text: "Value", header: false });
  });

  it("when builds ordered and unordered lists from list items", () => {
    const items: Structural[] = [
      { kind: "li", text: "plain", children: [] },
      { kind: "li", text: "ignored", children: [{ type: "badge", value: "one" }] },
      {
        kind: "li",
        text: "ignored",
        children: [
          { type: "text", value: "a" },
          { type: "text", value: "b" },
        ],
      },
    ];
    expect(convert(element("ol", {}, "", [], items))).toEqual({
      type: "list",
      ordered: true,
      items: [
        { type: "text", value: "plain" },
        { type: "badge", value: "one" },
        {
          type: "column",
          children: [
            { type: "text", value: "a" },
            { type: "text", value: "b" },
          ],
        },
      ],
    });
    expect(convert(element("ul", {}, "", [], items))).not.toHaveProperty("ordered");
  });

  it("when maps tabs and accordions", () => {
    const tabs: Structural[] = [
      { kind: "tab", label: "One", children: [{ type: "text", value: "1" }] },
      { kind: "tab", label: "Two", children: [] },
    ];
    expect(convert(element("tabs", { "selected-index": "1" }, "", [], tabs))).toEqual({
      type: "tabs",
      selectedIndex: 1,
      tabs: [
        { label: "One", children: [{ type: "text", value: "1" }] },
        { label: "Two", children: [] },
      ],
    });
    expect(convert(element("accordion", { title: "Details", expanded: "false" }, "", []))).toEqual({
      type: "accordion",
      title: "Details",
      expanded: false,
      children: [],
    });
  });
});

describe("convert controls", () => {
  it("builds callback actions with data and collected inputs", () => {
    const button = convert(
      element(
        "button",
        {
          label: "Send",
          variant: "primary",
          enabled: "false",
          event: "submit",
          "data-kind": "rsvp",
          "data-empty": "",
          collect: " name, date, , ",
          href: "https://ignored.test",
        },
        "ignored",
      ),
    );
    expect(button).toEqual({
      type: "button",
      label: "Send",
      variant: "primary",
      enabled: false,
      action: {
        type: "callback",
        event: "submit",
        data: { kind: "rsvp", empty: "" },
        collectFrom: ["name", "date"],
      },
    });
  });

  it.each([
    [{ href: "https://example.test" }, { type: "open_url", url: "https://example.test" }],
    [{ toggle: "details" }, { type: "toggle", targetId: "details" }],
    [{ copy: "text" }, { type: "copy_to_clipboard", text: "text" }],
    [{}, undefined],
  ])("when maps non-callback action attributes", (attrs, expected) => {
    expect(convert(element("button", attrs, "Act"))).toMatchObject({ type: "button", label: "Act" });
    expect((convert(element("button", attrs, "Act")) as Record<string, unknown>).action).toEqual(expected);
  });

  it("when maps text, date, time, and textarea inputs", () => {
    expect(
      convert(
        element("input", {
          id: "name",
          label: "Name",
          value: "Ada",
          placeholder: "Full name",
          keyboard: "text",
          required: "yes",
          multiline: "1",
        }),
      ),
    ).toEqual({
      type: "text_input",
      id: "name",
      label: "Name",
      value: "Ada",
      placeholder: "Full name",
      keyboard: "text",
      required: true,
      multiline: true,
    });
    expect(convert(element("input", { type: "date", id: "day", placeholder: "ignored" }))).toEqual({
      type: "date_input",
      id: "day",
    });
    expect(convert(element("input", { type: "time", id: "at" }))).toEqual({ type: "time_input", id: "at" });
    expect(convert(element("textarea", { id: "memo" }, "default"))).toEqual({
      type: "text_input",
      id: "memo",
      value: "default",
      multiline: true,
    });
  });

  it("when maps checkbox and switch labels from attributes or text", () => {
    expect(convert(element("input", { type: "checkbox", id: "a", label: "A", checked: "off" }))).toEqual({
      type: "checkbox",
      id: "a",
      label: "A",
      checked: false,
    });
    expect(convert(element("checkbox", { id: "b", checked: "yes" }, " B "))).toEqual({
      type: "checkbox",
      id: "b",
      label: "B",
      checked: true,
    });
    expect(convert(element("switch", { id: "c" }, " C "))).toEqual({ type: "switch", id: "c", label: "C" });
  });

  it("maps select and radio options with explicit selection precedence", () => {
    const options: Structural[] = [
      { kind: "option", text: "A", selected: false },
      { kind: "option", text: "B", selected: true },
    ];
    expect(convert(element("select", { id: "s", selected: "A", placeholder: "Pick" }, "", [], options))).toEqual({
      type: "select",
      id: "s",
      options: ["A", "B"],
      selected: "A",
      placeholder: "Pick",
    });
    expect(convert(element("radio-group", { id: "r", label: "Choice", required: "true" }, "", [], options))).toEqual({
      type: "radio_group",
      id: "r",
      label: "Choice",
      required: true,
      options: ["A", "B"],
      selected: "B",
    });
    expect(convert(element("option", { selected: "false" }, " A "))).toEqual({
      kind: "option",
      text: "A",
      selected: false,
    });
  });

  it("when maps sliders and chip groups", () => {
    expect(
      convert(element("slider", { id: "volume", label: "Volume", value: "5", min: "0", max: "10", step: "2" })),
    ).toEqual({
      type: "slider",
      id: "volume",
      label: "Volume",
      value: 5,
      min: 0,
      max: 10,
      step: 2,
    });
    const chips: Structural[] = [
      { kind: "chip", label: "Alpha", value: "a" },
      { kind: "chip", label: "Beta", value: "b" },
    ];
    expect(convert(element("chip-group", { id: "tags", selection: "multi", required: "1" }, "", [], chips))).toEqual({
      type: "chip_group",
      id: "tags",
      selection: "multi",
      required: true,
      chips: [
        { label: "Alpha", value: "a" },
        { label: "Beta", value: "b" },
      ],
    });
    expect(convert(element("chip", { value: "a" }, " Alpha "))).toEqual({
      kind: "chip",
      label: "Alpha",
      value: "a",
    });
  });

  it("drops line breaks and unknown tags", () => {
    expect(convert(element("br"))).toBeNull();
    expect(convert(element("unknown", {}, "payload"))).toBeNull();
  });
});
