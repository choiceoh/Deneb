// Element-to-node conversion for the deneb-ui labeled-HTML parser.

import type { Node } from "./denebUiParse";
import {
  canonBadgeColor,
  canonChartType,
  canonSeverity,
  canonTextStyle,
  lenientFloat,
  looksLikeMarkdownBlock,
  truthy,
} from "./denebUiHtmlHelpers";

export type Structural =
  | { kind: "option"; text: string; selected: boolean }
  | { kind: "chip"; label: string; value: string }
  | { kind: "tab"; label: string; children: Node[] }
  | { kind: "tr"; cells: { text: string; header: boolean }[] }
  | { kind: "cell"; text: string; header: boolean }
  | { kind: "point"; label: string; value: number }
  | { kind: "li"; text: string; children: Node[] };

export interface OpenElem {
  tag: string;
  attrs: Record<string, string>;
  children: Node[];
  structs: Structural[];
  text: string[];
  /** Buffered implicit-text runs, flushed as one merged node. */
  pending: string[];
  /**
   * A whitespace-only run arrived after existing text; the next run keeps one
   * separating space so inline merges don't glue ("**A** **B**").
   */
  pendingSpace: boolean;
}

export function newElem(tag: string, attrs: Record<string, string>): OpenElem {
  return { tag, attrs, children: [], structs: [], text: [], pending: [], pendingSpace: false };
}

export function convert(el: OpenElem): Node | Structural | null {
  const a = el.attrs;
  const inner = el.text.join("").trim();
  const node: Node = {};
  if (a.id) node.id = a.id;
  const num = lenientFloat;
  const bool = (key: string): boolean | undefined => (key in a ? truthy(a[key]) : undefined);
  const set = (key: string, v: unknown) => {
    if (v !== undefined && v !== "" && v !== null) node[key] = v;
  };

  switch (el.tag) {
    case "column":
    case "col":
      return { ...node, type: "column", children: el.children };
    case "row":
      set("longPressAction", longPressFromAttrs(a));
      return { ...node, type: "row", children: el.children };
    case "card":
      return { ...node, type: "card", children: el.children };
    case "box":
      set("contentAlignment", a.align);
      return { ...node, type: "box", children: el.children };
    // spacer: invented-but-frequent alias — breathing room ≈ divider.
    case "hr":
    case "divider":
    case "spacer":
      return { ...node, type: "divider" };
    case "text":
      // Whole markdown blocks stuffed into <text> upgrade to a markdown node
      // so they render structured.
      if (looksLikeMarkdownBlock(inner)) return { ...node, type: "markdown", value: inner };
      set("style", canonTextStyle(a.style));
      set("bold", bool("bold"));
      set("italic", bool("italic"));
      set("color", a.color);
      set("longPressAction", longPressFromAttrs(a));
      return { ...node, type: "text", value: inner };
    // HTML fluency aliases: paragraphs and headings map onto text nodes.
    // title/label/kv are invented-but-frequent model habits promoted to
    // proper typography (2026-07-18 reject telemetry; gateway parity).
    case "p":
    case "h1":
    case "h2":
    case "h3":
    case "h4":
    case "h5":
    case "h6":
    case "title":
    case "label":
    case "kv": {
      let value = inner;
      // <kv label="발신">양도현</kv> → "발신 — 양도현" (key/value convention).
      if (el.tag === "kv" && a.label && value) value = `${a.label} — ${value}`;
      if (!value && el.children.length === 0) return null;
      const textNode: Node = { ...node, type: "text", value };
      if (el.tag === "h1") textNode.style = "headline";
      else if (el.tag === "h2" || el.tag === "h3" || el.tag === "title") textNode.style = "title";
      else if (el.tag === "label") textNode.style = "caption";
      else if (el.tag !== "p" && el.tag !== "kv") textNode.bold = true;
      if (el.children.length === 0) return textNode;
      // Block children inside a paragraph: keep both, text first.
      const kids = value ? [textNode, ...el.children] : el.children;
      return { type: "column", children: kids };
    }
    case "markdown":
      return { ...node, type: "markdown", value: inner };
    case "img":
    case "image":
      set("alt", a.alt);
      set("height", num(a.height));
      set("aspectRatio", num(a["aspect-ratio"]));
      return { ...node, type: "image", url: a.src ?? a.url ?? "" };
    case "icon":
      set("size", num(a.size));
      set("color", a.color);
      return { ...node, type: "icon", name: a.name ?? "" };
    case "code":
      set("language", a.language ?? a.lang);
      return { ...node, type: "code", code: el.text.join("").trim() };
    case "blockquote":
    case "quote":
      set("source", a.source);
      return { ...node, type: "quote", text: inner };
    case "badge":
      set("color", canonBadgeColor(a.color));
      return { ...node, type: "badge", value: a.value || inner };
    case "stat":
      set("description", a.description);
      return { ...node, type: "stat", value: a.value || inner, label: a.label ?? "" };
    case "avatar":
      set("name", a.name);
      set("imageUrl", a.src ?? a["image-url"]);
      set("size", num(a.size));
      return { ...node, type: "avatar" };
    case "progress": {
      // Percent tolerance: "68" / "68%" mean 68% — the 0..1 contract only
      // applies to values already in range.
      let pv = num(a.value);
      if (pv != null && pv > 1) pv /= 100;
      if (pv != null) set("value", Math.min(Math.max(pv, 0), 1));
      set("label", a.label);
      return { ...node, type: "progress" };
    }
    case "alert":
      set("title", a.title);
      set("severity", canonSeverity(a.severity));
      return { ...node, type: "alert", message: a.message || inner };
    case "countdown":
      set("label", a.label || inner || undefined);
      set("action", actionFromAttrs(a));
      return { ...node, type: "countdown", seconds: num(a.seconds) ?? 0 };
    case "chart": {
      const points = el.structs.filter((s): s is Extract<Structural, { kind: "point" }> => s.kind === "point");
      set("chartType", canonChartType(a.type));
      set("label", a.label);
      return {
        ...node,
        type: "chart",
        labels: points.map((p) => p.label),
        values: points.map((p) => p.value),
      };
    }
    case "point":
      return { kind: "point", label: a.label ?? "", value: num(a.value) ?? 0 };
    case "table": {
      let headers: string[] = [];
      const rows: string[][] = [];
      for (const s of el.structs) {
        if (s.kind !== "tr") continue;
        const texts = s.cells.map((c) => c.text);
        if (s.cells.some((c) => c.header) && headers.length === 0) headers = texts;
        else rows.push(texts);
      }
      return { ...node, type: "table", headers, rows };
    }
    case "tr":
      return {
        kind: "tr",
        cells: el.structs.filter((s): s is Extract<Structural, { kind: "cell" }> => s.kind === "cell"),
      };
    case "td":
      return { kind: "cell", text: inner, header: false };
    case "th":
      return { kind: "cell", text: inner, header: true };
    case "ul":
    case "ol":
    case "list": {
      const items = el.structs
        .filter((s): s is Extract<Structural, { kind: "li" }> => s.kind === "li")
        .map((li) => {
          if (li.children.length === 0) return { type: "text", value: li.text };
          if (li.children.length === 1) return li.children[0];
          return { type: "column", children: li.children };
        });
      const ordered = el.tag === "ol" || ("ordered" in a && truthy(a.ordered));
      if (ordered) node.ordered = true;
      return { ...node, type: "list", items };
    }
    case "li":
      return { kind: "li", text: inner, children: el.children };
    case "tabs":
      set("selectedIndex", num(a["selected-index"]));
      return {
        ...node,
        type: "tabs",
        tabs: el.structs
          .filter((s): s is Extract<Structural, { kind: "tab" }> => s.kind === "tab")
          .map((t) => ({ label: t.label, children: t.children })),
      };
    case "tab":
      return { kind: "tab", label: a.label ?? "", children: el.children };
    case "accordion":
      set("expanded", bool("expanded"));
      return { ...node, type: "accordion", title: a.title ?? "", children: el.children };
    case "button":
      set("variant", a.variant);
      set("enabled", bool("enabled"));
      set("action", actionFromAttrs(a));
      return { ...node, type: "button", label: a.label || inner };
    case "input":
      switch ((a.type ?? "").toLowerCase()) {
        case "date":
          return fillInput({ ...node, type: "date_input" }, a, false);
        case "time":
          return fillInput({ ...node, type: "time_input" }, a, false);
        case "checkbox":
          set("checked", bool("checked"));
          return { ...node, type: "checkbox", id: a.id ?? "", label: a.label ?? "" };
        default:
          return fillInput({ ...node, type: "text_input" }, a, true);
      }
    case "textarea": {
      const n = fillInput({ ...node, type: "text_input" }, a, true);
      n.multiline = true;
      if (inner) n.value = n.value ?? inner;
      return n;
    }
    case "checkbox":
      set("checked", bool("checked"));
      return { ...node, type: "checkbox", id: a.id ?? "", label: a.label || inner };
    case "switch":
      set("checked", bool("checked"));
      return { ...node, type: "switch", id: a.id ?? "", label: a.label || inner };
    case "select":
    case "radio-group":
    case "radiogroup": {
      const opts = el.structs.filter((s): s is Extract<Structural, { kind: "option" }> => s.kind === "option");
      set("label", a.label);
      set("required", bool("required"));
      const selected =
        a.selected ||
        opts
          .filter((o) => o.selected)
          .map((o) => o.text)
          .pop();
      if (selected) node.selected = selected;
      if (el.tag === "select") {
        set("placeholder", a.placeholder);
        return { ...node, type: "select", id: a.id ?? "", options: opts.map((o) => o.text) };
      }
      return { ...node, type: "radio_group", id: a.id ?? "", options: opts.map((o) => o.text) };
    }
    case "option":
      return { kind: "option", text: inner, selected: "selected" in a && truthy(a.selected) };
    case "slider":
      set("label", a.label);
      set("value", num(a.value));
      set("min", num(a.min));
      set("max", num(a.max));
      set("step", num(a.step));
      return { ...node, type: "slider", id: a.id ?? "" };
    case "chips":
    case "chip-group":
      set("required", bool("required"));
      return {
        ...node,
        type: "chip_group",
        id: a.id ?? "",
        selection: a.selection || "single",
        chips: el.structs
          .filter((s): s is Extract<Structural, { kind: "chip" }> => s.kind === "chip")
          .map((c) => ({ label: c.label, value: c.value })),
      };
    case "chip":
      return { kind: "chip", label: inner, value: a.value || inner };
    case "br":
      return null;
    default:
      return null; // unknown tag: skip subtree (server validation reports it)
  }
}

function fillInput(node: Node, a: Record<string, string>, textKind: boolean): Node {
  node.id = a.id ?? "";
  if (a.label) node.label = a.label;
  if (a.value) node.value = a.value;
  if ("required" in a && truthy(a.required)) node.required = true;
  if (textKind) {
    if (a.placeholder) node.placeholder = a.placeholder;
    if (a.keyboard) node.keyboard = a.keyboard;
    if ("multiline" in a && truthy(a.multiline)) node.multiline = true;
  }
  return node;
}

// Action attributes on button/countdown. Precedence: event > href > toggle > copy.
function actionFromAttrs(a: Record<string, string>): Node | undefined {
  if (a.event) {
    const action: Node = { type: "callback", event: a.event };
    const data: Record<string, string> = {};
    for (const [k, v] of Object.entries(a)) {
      if (k.startsWith("data-") && k.length > 5) data[k.slice(5)] = v;
    }
    if (Object.keys(data).length > 0) action.data = data;
    const collect = (a.collect ?? "")
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
    if (collect.length > 0) action.collectFrom = collect;
    return action;
  }
  if (a.href) return { type: "open_url", url: a.href };
  if (a.toggle) return { type: "toggle", targetId: a.toggle };
  if (a.copy) return { type: "copy_to_clipboard", text: a.copy };
  return undefined;
}

// longpress="event" (+ data-*) → a press-hold callback, distinct from event=
// (tap). Attached to any node by the convert wrapper; the renderer binds it.
function longPressFromAttrs(a: Record<string, string>): Node | undefined {
  const ev = (a.longpress ?? "").trim();
  if (!ev) return undefined;
  const action: Node = { type: "callback", event: ev };
  const data: Record<string, string> = {};
  for (const [k, v] of Object.entries(a)) {
    if (k.startsWith("data-") && k.length > 5) data[k.slice(5)] = v;
  }
  if (Object.keys(data).length > 0) action.data = data;
  return action;
}
