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
import { convert, newElem, type OpenElem, type Structural } from "./denebUiHtmlConvert";
import { decodeEntities, indexOfCloseTag, isNameChar, isNameStart, isSpace, textBlockNode } from "./denebUiHtmlHelpers";

const VOID_TAGS = new Set(["hr", "img", "input", "icon", "slider", "progress", "avatar", "point", "br", "spacer"]);
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
  "thead",
  "tbody",
  "tfoot",
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
  "title",
  "label",
  "spacer",
  "kv",
]);

// Whether bare text inside a tag surfaces as implicit child nodes
// (containers, generic wrappers, unknown tags) rather than feeding the
// element's own value slot (text/badge/li/… and inline tags).
function treatsTextAsChildren(tag: string): boolean {
  if (CONTAINER_TAGS.has(tag) || GENERIC_TAGS.has(tag)) return true;
  if (tag in INLINE_TAGS) return false;
  return !KNOWN_TAGS.has(tag);
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
  private rootPendingSpace = false;

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
      // implicit text) hoist to the parent in source order. Structural
      // intermediates hoist too, so <thead>/<tbody> table rows reach the
      // enclosing <table> instead of vanishing with the wrapper.
      this.flushPending(el);
      for (const c of el.children) this.attach(c);
      for (const s of el.structs) this.attach(s);
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
    if (top) top.pendingSpace = false; // whitespace before a child is layout, not a separator
    if (isStruct) {
      top?.structs.push(v as Structural); // floating at root: drop
      return;
    }
    if (top) {
      this.flushPending(top);
      top.children.push(v);
    } else {
      this.flushRootPending();
      this.rootPendingSpace = false;
      this.roots.push(v);
    }
  }

  private emitText(t: string) {
    if (!t.trim()) {
      this.markPendingSpace();
      return;
    }
    this.emitRun(decodeEntities(t));
  }

  // Remembers that a whitespace-only run arrived after existing text, so the
  // next run in the same flow keeps a single separating space — dropping it
  // glues inline markers ("**A****B**").
  private markPendingSpace() {
    const top = this.stack[this.stack.length - 1];
    if (!top) {
      if (this.rootPending.length > 0) this.rootPendingSpace = true;
      return;
    }
    if (top.text.length > 0) top.pendingSpace = true;
  }

  // Adds an already-decoded text run (entity decoding must not repeat on
  // inline re-emits).
  private emitRun(t: string) {
    if (!t.trim()) return;
    const top = this.stack[this.stack.length - 1];
    if (!top) {
      if (this.rootPendingSpace) {
        t = " " + t;
        this.rootPendingSpace = false;
      }
      this.rootPending.push(t);
      return;
    }
    if (top.pendingSpace) {
      t = " " + t;
      top.pendingSpace = false;
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
