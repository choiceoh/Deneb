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

// Inline HTML formatting habits with no node of their own: they merge back
// into the parent text flow as markdown-marked runs ("**"/"*") the inline
// renderer already draws — content survives instead of the subtree dropping.
// Empty marker = keep bare text.
const INLINE_TAGS: Record<string, string> = {
  b: "**",
  strong: "**",
  i: "*",
  em: "*",
  u: "",
  s: "",
  del: "",
  strike: "",
  mark: "",
  small: "",
  span: "",
  sub: "",
  sup: "",
  a: "",
};

// Structural HTML wrappers (div soup) models emit out of pre-trained habit.
// They produce no node: children hoist to the parent and bare text becomes
// implicit text nodes.
const GENERIC_TAGS = new Set([
  "div",
  "section",
  "article",
  "header",
  "footer",
  "main",
  "aside",
  "figure",
  "center",
  "nav",
]);

// Every tag convert() maps to a node or structural. Tags in none of the
// tables unwrap like GENERIC_TAGS (the gateway validator reports them), so
// content survives typos.
const KNOWN_TAGS = new Set([
  "column",
  "col",
  "row",
  "card",
  "box",
  "hr",
  "divider",
  "text",
  "markdown",
  "img",
  "image",
  "icon",
  "code",
  "blockquote",
  "quote",
  "badge",
  "stat",
  "avatar",
  "progress",
  "alert",
  "countdown",
  "chart",
  "point",
  "table",
  "tr",
  "td",
  "th",
  "ul",
  "ol",
  "list",
  "li",
  "tabs",
  "tab",
  "accordion",
  "button",
  "input",
  "textarea",
  "checkbox",
  "switch",
  "select",
  "radio-group",
  "radiogroup",
  "option",
  "slider",
  "chips",
  "chip-group",
  "chip",
  "br",
  "p",
  "h1",
  "h2",
  "h3",
  "h4",
  "h5",
  "h6",
]);

// Whether bare text inside a tag surfaces as implicit child nodes
// (containers, generic wrappers, unknown tags) rather than feeding the
// element's own value slot (text/badge/li/… and inline tags).
function treatsTextAsChildren(tag: string): boolean {
  if (CONTAINER_TAGS.has(tag) || GENERIC_TAGS.has(tag)) return true;
  if (tag in INLINE_TAGS) return false;
  return !KNOWN_TAGS.has(tag);
}

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
  /** Buffered implicit-text runs, flushed as one merged node. */
  pending: string[];
}

function newElem(tag: string, attrs: Record<string, string>): OpenElem {
  return { tag, attrs, children: [], structs: [], text: [], pending: [] };
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
  private rootPending: string[] = [];

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
    this.flushRootPending();
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
    const el = newElem(name, attrs);
    if (VOID_TAGS.has(name) || selfClose) {
      // Self-closed inline/generic/unknown tags carry no content.
      if (name in INLINE_TAGS || GENERIC_TAGS.has(name) || !KNOWN_TAGS.has(name)) return;
      this.attach(convert(el));
      return;
    }
    this.stack.push(el);
  }

  private captureRawText(name: string, attrs: Record<string, string>) {
    const end = indexOfCloseTag(this.src, this.pos, name);
    let raw: string;
    if (end < 0) {
      raw = this.src.slice(this.pos);
      this.pos = this.src.length;
    } else {
      raw = this.src.slice(this.pos, end);
      const gt = this.src.indexOf(">", end);
      this.pos = gt < 0 ? this.src.length : gt + 1;
    }
    const decoded = decodeEntities(raw);
    // Inline habit: <code> inside a text flow merges as a backtick run
    // instead of breaking the sentence into a block node.
    if (name === "code" && this.inlineCodeContext()) {
      const t = decoded.trim();
      if (t) this.emitRun("`" + t + "`");
      return;
    }
    const el = newElem(name, attrs);
    el.text.push(decoded);
    this.attach(convert(el));
  }

  // Raw <code> merges into text flow when the parent is a text node or inline tag.
  private inlineCodeContext(): boolean {
    const tag = this.stack[this.stack.length - 1]?.tag;
    if (tag == null) return false;
    return tag === "text" || tag in INLINE_TAGS;
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
    if (el.tag in INLINE_TAGS) {
      this.emitInline(el, INLINE_TAGS[el.tag]);
      return;
    }
    if (GENERIC_TAGS.has(el.tag) || !KNOWN_TAGS.has(el.tag)) {
      // Unwrap: the wrapper produces no node; its children (incl. flushed
      // implicit text) hoist to the parent in source order.
      this.flushPending(el);
      for (const c of el.children) this.attach(c);
      return;
    }
    this.flushPending(el);
    this.attach(convert(el));
  }

  // Merges an inline formatting element back into the parent text flow
  // (<b>중요</b> → "**중요**"). Plain-value slots (badge, button labels)
  // receive bare text — literal markers would render as noise there. Real
  // child nodes (rare) hoist to the parent afterwards.
  private emitInline(el: OpenElem, marker: string) {
    const inner = el.text.join("").trim();
    if (inner) {
      let run = inner;
      if (this.inlineMarkupAllowed()) {
        if (el.tag === "a") {
          const href = el.attrs.href;
          if (href) run = `[${inner}](${href})`;
        } else if (marker) {
          run = marker + inner + marker;
        }
      }
      this.emitRun(run);
    }
    for (const c of el.children) this.attach(c);
  }

  // Markdown markers only where inline markdown renders (text/containers/root).
  private inlineMarkupAllowed(): boolean {
    const tag = this.stack[this.stack.length - 1]?.tag;
    if (tag == null) return true;
    return tag === "text" || treatsTextAsChildren(tag);
  }

  private attach(v: Node | Structural | null) {
    if (v == null) return;
    const isStruct = typeof v === "object" && "kind" in v;
    const top = this.stack[this.stack.length - 1];
    if (isStruct) {
      top?.structs.push(v as Structural); // floating at root: drop
      return;
    }
    if (top) {
      this.flushPending(top);
      top.children.push(v);
    } else {
      this.flushRootPending();
      this.roots.push(v);
    }
  }

  private emitText(t: string) {
    if (!t.trim()) return;
    this.emitRun(decodeEntities(t));
  }

  // Adds an already-decoded text run (entity decoding must not repeat on
  // inline re-emits).
  private emitRun(t: string) {
    if (!t.trim()) return;
    const top = this.stack[this.stack.length - 1];
    if (!top) {
      this.rootPending.push(t);
      return;
    }
    top.text.push(t);
    if (treatsTextAsChildren(top.tag)) top.pending.push(t);
  }

  // Materializes buffered text runs as one merged implicit node. Merging
  // keeps sentences split by inline tags whole and lets markdown block
  // structure be recognized.
  private flushPending(el: OpenElem) {
    if (el.pending.length === 0) return;
    const node = textBlockNode(el.pending.join(""));
    el.pending = [];
    el.children.push(node);
  }

  private flushRootPending() {
    if (this.rootPending.length === 0) return;
    const node = textBlockNode(this.rootPending.join(""));
    this.rootPending = [];
    this.roots.push(node);
  }
}

// Wraps a merged text run as an implicit node — as markdown when the run
// carries markdown block structure (auto-correcting the "markdown table
// inside a card" habit: markdown nodes route through the full markdown
// renderer, tables included), else as plain text.
function textBlockNode(s: string): Node {
  const t = s.trim();
  if (looksLikeMarkdownBlock(t)) return { type: "markdown", value: t };
  return { type: "text", value: t };
}

// Whether text carries markdown block structure (table rows, headings, list
// runs, fences) that a plain text node would render broken. Conservative:
// single bullets or lone pipes stay text.
function looksLikeMarkdownBlock(s: string): boolean {
  if (s.includes("```")) return true;
  let pipeRows = 0;
  let bullets = 0;
  for (const line of s.split("\n")) {
    const t = line.trim();
    if (!t) continue;
    if (t.startsWith("|")) {
      if (++pipeRows >= 2) return true;
      continue;
    }
    if (isMarkdownHeading(t)) return true;
    if (isMarkdownBullet(t) && ++bullets >= 2) return true;
  }
  return false;
}

function isMarkdownHeading(t: string): boolean {
  let n = 0;
  while (n < t.length && t[n] === "#") n++;
  return n >= 1 && n <= 6 && n < t.length && t[n] === " ";
}

function isMarkdownBullet(t: string): boolean {
  if (t.length >= 2 && (t[0] === "-" || t[0] === "*") && t[1] === " ") return true;
  if (t.startsWith("• ")) return true;
  let i = 0;
  while (i < t.length && t[i] >= "0" && t[i] <= "9") i++;
  return i >= 1 && i + 1 < t.length && (t[i] === "." || t[i] === ")") && t[i + 1] === " ";
}

// ---------------------------------------------------------------------------
// Element → node conversion
// ---------------------------------------------------------------------------

function convert(el: OpenElem): Node | Structural | null {
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
      // Whole markdown blocks stuffed into <text> upgrade to a markdown node
      // so they render structured.
      if (looksLikeMarkdownBlock(inner)) return { ...node, type: "markdown", value: inner };
      set("style", canonTextStyle(a.style));
      set("bold", bool("bold"));
      set("italic", bool("italic"));
      set("color", a.color);
      return { ...node, type: "text", value: inner };
    // HTML fluency aliases: paragraphs and headings map onto text nodes.
    case "p":
    case "h1":
    case "h2":
    case "h3":
    case "h4":
    case "h5":
    case "h6": {
      if (!inner && el.children.length === 0) return null;
      const textNode: Node = { ...node, type: "text", value: inner };
      if (el.tag === "h1") textNode.style = "headline";
      else if (el.tag === "h2" || el.tag === "h3") textNode.style = "title";
      else if (el.tag !== "p") textNode.bold = true;
      if (el.children.length === 0) return textNode;
      // Block children inside a paragraph: keep both, text first.
      const kids = inner ? [textNode, ...el.children] : el.children;
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

// Absolute index of the first `</name` at or after `from`, comparing the
// (ASCII, whitelist-only) tag name case-insensitively via manual folding.
// Never lowercase the whole source for index math: Unicode case mapping can
// change string length (e.g. "İ".toLowerCase() === "i̇"), skewing indexes into
// the original — the fuzzer-found crash in the Go port of this parser.
function indexOfCloseTag(s: string, from: number, name: string): number {
  const n = name.length;
  for (let i = from; i + 2 + n <= s.length; i++) {
    if (s[i] !== "<" || s[i + 1] !== "/") continue;
    let match = true;
    for (let k = 0; k < n; k++) {
      let c = s.charCodeAt(i + 2 + k);
      if (c >= 65 && c <= 90) c += 32;
      if (c !== name.charCodeAt(k)) {
        match = false;
        break;
      }
    }
    if (match) return i;
  }
  return -1;
}

function truthy(v: string): boolean {
  return !["false", "0", "no", "off"].includes(v.trim().toLowerCase());
}

// Extracts a number from a lenient attribute value: exact floats parse as-is;
// otherwise units, thousands commas, and stray symbols are tolerated
// ("1,200톤" → 1200, "68%" → 68, "16px" → 16).
function lenientFloat(v: string | undefined): number | undefined {
  const t = v?.trim() ?? "";
  if (!t) return undefined;
  const exact = Number(t);
  if (Number.isFinite(exact)) return exact;
  let start = -1;
  for (let i = 0; i < t.length; i++) {
    if (t[i] >= "0" && t[i] <= "9") {
      start = i;
      break;
    }
  }
  if (start < 0) return undefined;
  let b = start > 0 && t[start - 1] === "-" ? "-" : "";
  let dot = false;
  for (let i = start; i < t.length; i++) {
    const c = t[i];
    if (c >= "0" && c <= "9") b += c;
    else if (c === ",")
      continue; // thousands separator: skip
    else if (c === "." && !dot) {
      dot = true;
      b += c;
    } else break;
  }
  const f = Number(b.replace(/\.$/, ""));
  return Number.isFinite(f) ? f : undefined;
}

// Folds common CSS color words onto the badge tint enum.
function canonBadgeColor(v: string | undefined): string | undefined {
  switch (v?.trim().toLowerCase()) {
    case "red":
      return "error";
    case "green":
      return "success";
    case "yellow":
    case "amber":
    case "orange":
      return "warning";
    case "blue":
      return "primary";
    case "gray":
    case "grey":
    case "neutral":
      return "secondary";
    default:
      return v;
  }
}

// Folds severity synonyms onto the alert enum.
function canonSeverity(v: string | undefined): string | undefined {
  switch (v?.trim().toLowerCase()) {
    case "warn":
    case "caution":
      return "warning";
    case "danger":
    case "critical":
    case "fatal":
      return "error";
    case "ok":
    case "done":
      return "success";
    case "note":
    case "notice":
    case "information":
      return "info";
    default:
      return v;
  }
}

// Folds chart-type synonyms onto bar/line.
function canonChartType(v: string | undefined): string | undefined {
  switch (v?.trim().toLowerCase()) {
    case "bars":
    case "column":
    case "columns":
      return "bar";
    case "lines":
    case "area":
    case "trend":
      return "line";
    default:
      return v;
  }
}

// Folds text-style synonyms onto the style enum.
function canonTextStyle(v: string | undefined): string | undefined {
  switch (v?.trim().toLowerCase()) {
    case "heading":
    case "header":
      return "headline";
    case "subtitle":
    case "subheading":
      return "title";
    default:
      return v;
  }
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
