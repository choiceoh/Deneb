// Labeled-HTML wire format for deneb-ui blocks (v2, 2026-07) — parses fence
// bodies starting with `<` into the same loose node objects the legacy JSON
// path produced, so DenebUi.tsx renders both formats unchanged.
//
// Deliberately a small XML-lite tokenizer, NOT DOMParser: the browser's HTML5
// algorithms foster-parent custom tags out of <table> and would rearrange the
// tree. Grammar single source of truth: docs/research/deneb-ui-html.md (repo
// root); the gateway (denebui/html.go) and native client (DenebUiHtml.kt)
// port the same rules — keep the shared test vectors in sync.

import type { Node } from "./denebUiParse";

const VOID_TAGS = new Set(["hr", "img", "input", "icon", "slider", "progress", "avatar", "point", "br"]);
const RAW_TEXT_TAGS = new Set(["markdown", "code"]);
const AUTO_CLOSE: Record<string, Set<string>> = {
  li: new Set(["li"]),
  option: new Set(["option"]),
  td: new Set(["td", "th"]),
  th: new Set(["td", "th"]),
  tr: new Set(["td", "th", "tr"]),
  tab: new Set(["tab"]),
  chip: new Set(["chip"]),
  point: new Set(["point"]),
};
const CONTAINER_TAGS = new Set(["column", "col", "row", "card", "box", "accordion", "li", "tab"]);

type Structural =
  | { kind: "option"; text: string; selected: boolean }
  | { kind: "chip"; label: string; value: string }
  | { kind: "tab"; label: string; children: Node[] }
  | { kind: "tr"; cells: { text: string; header: boolean }[] }
  | { kind: "cell"; text: string; header: boolean }
  | { kind: "point"; label: string; value: number }
  | { kind: "li"; text: string; children: Node[] };

interface OpenElem {
  tag: string;
  attrs: Record<string, string>;
  children: Node[];
  structs: Structural[];
  text: string[];
}

// Parse a fence body into a node tree, or null when nothing usable parsed.
export function parseDenebUiHtml(body: string): Node | null {
  const roots = new Parser(body.trim()).parseNodes();
  if (roots.length === 0) return null;
  if (roots.length === 1) return roots[0];
  return { type: "column", children: roots };
}

class Parser {
  private pos = 0;
  private stack: OpenElem[] = [];
  private roots: Node[] = [];

  constructor(private src: string) {}

  parseNodes(): Node[] {
    while (this.pos < this.src.length) {
      const lt = this.src.indexOf("<", this.pos);
      if (lt < 0) {
        this.emitText(this.src.slice(this.pos));
        break;
      }
      if (lt > this.pos) {
        this.emitText(this.src.slice(this.pos, lt));
        this.pos = lt;
      }
      if (!this.parseTag()) {
        this.emitText("<");
        this.pos++;
      }
    }
    while (this.stack.length > 0) this.closeTop();
    return this.roots;
  }

  private parseTag(): boolean {
    const s = this.src;
    const i = this.pos;
    if (i + 1 >= s.length) return false;
    if (s.startsWith("<!--", i)) {
      const end = s.indexOf("-->", i + 4);
      this.pos = end < 0 ? s.length : end + 3;
      return true;
    }
    if (s[i + 1] === "!") {
      const end = s.indexOf(">", i);
      this.pos = end < 0 ? s.length : end + 1;
      return true;
    }
    if (s[i + 1] === "/") {
      const end = s.indexOf(">", i);
      if (end < 0) return false;
      const name = s
        .slice(i + 2, end)
        .trim()
        .toLowerCase();
      this.pos = end + 1;
      this.handleClose(name);
      return true;
    }
    if (!isNameStart(s[i + 1])) return false;
    let j = i + 1;
    while (j < s.length && isNameChar(s[j])) j++;
    const name = s.slice(i + 1, j).toLowerCase();
    const [attrs, end, selfClose] = this.parseAttrs(j);
    this.pos = end;
    this.handleOpen(name, attrs, selfClose);
    return true;
  }

  private parseAttrs(start: number): [Record<string, string>, number, boolean] {
    const s = this.src;
    const attrs: Record<string, string> = {};
    let j = start;
    let selfClose = false;
    for (;;) {
      while (j < s.length && isSpace(s[j])) j++;
      if (j >= s.length) return [attrs, j, selfClose]; // truncated tag: swallow
      if (s[j] === ">") return [attrs, j + 1, selfClose];
      if (s[j] === "/") {
        selfClose = true;
        j++;
        continue;
      }
      if (!isNameStart(s[j])) {
        j++;
        continue;
      }
      let k = j;
      while (k < s.length && isNameChar(s[k])) k++;
      const key = s.slice(j, k).toLowerCase();
      j = k;
      while (j < s.length && isSpace(s[j])) j++;
      let value = "true"; // boolean attribute default
      if (j < s.length && s[j] === "=") {
        j++;
        while (j < s.length && isSpace(s[j])) j++;
        if (j < s.length && (s[j] === '"' || s[j] === "'")) {
          const q = s[j];
          j++;
          k = j;
          while (k < s.length && s[k] !== q) k++;
          value = s.slice(j, k);
          j = Math.min(k + 1, s.length);
        } else {
          k = j;
          while (k < s.length && !isSpace(s[k]) && s[k] !== ">" && s[k] !== "/") k++;
          value = s.slice(j, k);
          j = k;
        }
      }
      attrs[key] = decodeEntities(value);
    }
  }

  private handleOpen(name: string, attrs: Record<string, string>, selfClose: boolean) {
    const closers = AUTO_CLOSE[name];
    if (closers) {
      while (this.stack.length > 0 && closers.has(this.stack[this.stack.length - 1].tag)) this.closeTop();
    }
    if (RAW_TEXT_TAGS.has(name) && !selfClose) {
      this.captureRawText(name, attrs);
      return;
    }
    const el: OpenElem = { tag: name, attrs, children: [], structs: [], text: [] };
    if (VOID_TAGS.has(name) || selfClose) {
      this.attach(convert(el));
      return;
    }
    this.stack.push(el);
  }

  private captureRawText(name: string, attrs: Record<string, string>) {
    const lower = this.src.toLowerCase();
    const end = lower.indexOf("</" + name, this.pos);
    let raw: string;
    if (end < 0) {
      raw = this.src.slice(this.pos);
      this.pos = this.src.length;
    } else {
      raw = this.src.slice(this.pos, end);
      const gt = this.src.indexOf(">", end);
      this.pos = gt < 0 ? this.src.length : gt + 1;
    }
    const el: OpenElem = { tag: name, attrs, children: [], structs: [], text: [decodeEntities(raw)] };
    this.attach(convert(el));
  }

  private handleClose(name: string) {
    if (VOID_TAGS.has(name)) return;
    let idx = -1;
    for (let i = this.stack.length - 1; i >= 0; i--) {
      if (this.stack[i].tag === name) {
        idx = i;
        break;
      }
    }
    if (idx < 0) return; // stray close: ignore
    while (this.stack.length > idx) this.closeTop();
  }

  private closeTop() {
    const el = this.stack.pop()!;
    this.attach(convert(el));
  }

  private attach(v: Node | Structural | null) {
    if (v == null) return;
    const isStruct = typeof v === "object" && "kind" in v;
    const top = this.stack[this.stack.length - 1];
    if (isStruct) {
      top?.structs.push(v as Structural); // floating at root: drop
      return;
    }
    if (top) top.children.push(v);
    else this.roots.push(v);
  }

  private emitText(t: string) {
    if (!t.trim()) return;
    const decoded = decodeEntities(t).trim();
    const top = this.stack[this.stack.length - 1];
    if (!top) {
      this.roots.push({ type: "text", value: decoded });
      return;
    }
    top.text.push(decodeEntities(t));
    if (CONTAINER_TAGS.has(top.tag)) top.children.push({ type: "text", value: decoded });
  }
}

// ---------------------------------------------------------------------------
// Element → node conversion
// ---------------------------------------------------------------------------

/* eslint-disable complexity */
function convert(el: OpenElem): Node | Structural | null {
  const a = el.attrs;
  const inner = el.text.join("").trim();
  const node: Node = {};
  if (a.id) node.id = a.id;
  const num = (v: string | undefined): number | undefined => {
    if (v == null || v.trim() === "") return undefined;
    const f = Number(v);
    return Number.isFinite(f) ? f : undefined;
  };
  const bool = (key: string): boolean | undefined => (key in a ? truthy(a[key]) : undefined);
  const set = (key: string, v: unknown) => {
    if (v !== undefined && v !== "" && v !== null) node[key] = v;
  };

  switch (el.tag) {
    case "column":
    case "col":
      return { ...node, type: "column", children: el.children };
    case "row":
      return { ...node, type: "row", children: el.children };
    case "card":
      return { ...node, type: "card", children: el.children };
    case "box":
      set("contentAlignment", a.align);
      return { ...node, type: "box", children: el.children };
    case "hr":
    case "divider":
      return { ...node, type: "divider" };
    case "text":
      set("style", a.style);
      set("bold", bool("bold"));
      set("italic", bool("italic"));
      set("color", a.color);
      return { ...node, type: "text", value: inner };
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
      set("color", a.color);
      return { ...node, type: "badge", value: a.value || inner };
    case "stat":
      set("description", a.description);
      return { ...node, type: "stat", value: a.value || inner, label: a.label ?? "" };
    case "avatar":
      set("name", a.name);
      set("imageUrl", a.src ?? a["image-url"]);
      set("size", num(a.size));
      return { ...node, type: "avatar" };
    case "progress":
      set("value", num(a.value));
      set("label", a.label);
      return { ...node, type: "progress" };
    case "alert":
      set("title", a.title);
      set("severity", a.severity);
      return { ...node, type: "alert", message: a.message || inner };
    case "countdown":
      set("label", a.label || inner || undefined);
      set("action", actionFromAttrs(a));
      return { ...node, type: "countdown", seconds: num(a.seconds) ?? 0 };
    case "chart": {
      const points = el.structs.filter((s): s is Extract<Structural, { kind: "point" }> => s.kind === "point");
      set("chartType", a.type);
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
/* eslint-enable complexity */

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

function truthy(v: string): boolean {
  return !["false", "0", "no", "off"].includes(v.trim().toLowerCase());
}

function isSpace(c: string): boolean {
  return c === " " || c === "\t" || c === "\n" || c === "\r";
}

function isNameStart(c: string): boolean {
  return (c >= "a" && c <= "z") || (c >= "A" && c <= "Z");
}

function isNameChar(c: string): boolean {
  return isNameStart(c) || (c >= "0" && c <= "9") || c === "-" || c === "_";
}

// Decodes the grammar's small entity set: named basics + numeric refs.
function decodeEntities(s: string): string {
  if (!s.includes("&")) return s;
  let out = "";
  for (let i = 0; i < s.length;) {
    if (s[i] !== "&") {
      out += s[i];
      i++;
      continue;
    }
    const semi = s.indexOf(";", i);
    if (semi < 0 || semi - i > 10) {
      out += s[i];
      i++;
      continue;
    }
    const ent = s.slice(i + 1, semi);
    let decoded: string | null = null;
    switch (ent.toLowerCase()) {
      case "lt":
        decoded = "<";
        break;
      case "gt":
        decoded = ">";
        break;
      case "amp":
        decoded = "&";
        break;
      case "quot":
        decoded = '"';
        break;
      case "apos":
        decoded = "'";
        break;
      case "nbsp":
        decoded = " ";
        break;
      default:
        if (ent.startsWith("#")) {
          const code = ent[1] === "x" || ent[1] === "X" ? parseInt(ent.slice(2), 16) : parseInt(ent.slice(1), 10);
          if (Number.isFinite(code) && code > 0) decoded = String.fromCodePoint(code);
        }
    }
    if (decoded == null) {
      out += s[i];
      i++;
    } else {
      out += decoded;
      i = semi + 1;
    }
  }
  return out;
}
