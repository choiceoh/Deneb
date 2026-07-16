// Pure, dependency-free Markdown block parser — text → a Block AST. No React and no
// KaTeX here: math is carried through as a `mathBlock` / inline `$…$` text node for
// the renderer to turn into KaTeX. Kept apart from the renderer so the GFM grammar
// is unit-testable directly. See components/Markdown.tsx for the AST → React pass.

export type Align = "left" | "center" | "right" | undefined;

export interface ListItem {
  task?: boolean; // a GFM task-list item ("- [ ] …")
  checked?: boolean;
  children: Block[]; // item body as blocks → supports nested lists / multi-paragraph
}

export type Block =
  | { type: "heading"; level: number; text: string }
  | { type: "code"; lang: string; text: string }
  | { type: "list"; ordered: boolean; start?: number; items: ListItem[] }
  | { type: "quote"; children: Block[] }
  | { type: "table"; header: string[]; align: Align[]; rows: string[][]; caption?: string }
  | { type: "details"; summary: string; open: boolean; children: Block[] }
  | { type: "mathBlock"; text: string }
  | { type: "hr" }
  | { type: "para"; text: string };

const FENCE = /^ {0,3}(?:```|~~~)(.*)$/;
const HEADING = /^ {0,3}(#{1,6})\s+(.*?)\s*#*\s*$/;
const HR = /^ {0,3}([-*_])(?:[ \t]*\1){2,}[ \t]*$/;
const SETEXT_H1 = /^\s{0,3}=+\s*$/;
const SETEXT_H2 = /^\s{0,3}-+\s*$/;
const QUOTE = /^ {0,3}>\s?(.*)$/;
const LIST_ITEM = /^(\s*)([-*+]|\d{1,9}[.)])(\s+)(.*)$/;
const TABLE_SEP = /^\s*\|?(?:\s*:?-+:?\s*\|)+(?:\s*:?-+:?\s*)?\|?\s*$/;
const DETAILS_OPEN = /<details\b([^>]*)>/i;
const DETAILS_CLOSE = /<\/details>/i;
const SUMMARY = /<summary\b[^>]*>([\s\S]*?)<\/summary>/i;
const TOTAL_CAPTION = /^합\s*계/;

// Visual width of a line's leading whitespace (tabs → 4) for nesting decisions.
function indentWidth(line: string): number {
  const m = /^[ \t]*/.exec(line);
  return m ? m[0].replace(/\t/g, "    ").length : 0;
}

function isOrdered(marker: string): boolean {
  return /\d/.test(marker);
}

// Split a GFM table row into trimmed cells, dropping the outer pipes. Escaped
// pipes (\|) are kept literal so a cell can contain one.
function splitRow(line: string): string[] {
  return line
    .replace(/^\s*\|/, "")
    .replace(/\|\s*$/, "")
    .split(/(?<!\\)\|/)
    .map((c) => c.trim().replace(/\\\|/g, "|"));
}

function cellAlign(spec: string): Align {
  const s = spec.trim();
  const left = s.startsWith(":");
  const right = s.endsWith(":");
  if (left && right) return "center";
  if (right) return "right";
  if (left) return "left";
  return undefined;
}

// GFM tables may interrupt a paragraph (GitHub behavior — Amaranth 결재 본문 and
// chat/feed often put a table right under a caption with no blank line). Same
// check the native BlockScanner.looksLikeTableStart uses: pipe row + separator
// with matching cell counts.
function looksLikeTableStart(line: string, next: string): boolean {
  if (!line.includes("|") || !next.includes("-")) return false;
  if (!TABLE_SEP.test(next)) return false;
  return splitRow(line).length === splitRow(next).length;
}

function isBlockOpener(line: string, next?: string): boolean {
  if (FENCE.test(line) || HEADING.test(line) || HR.test(line) || LIST_ITEM.test(line) || QUOTE.test(line)) return true;
  if (DETAILS_OPEN.test(line)) return true;
  if (next !== undefined && looksLikeTableStart(line, next)) return true;
  return false;
}

// Consume one list (and its nested children) starting at lines[start]. Returns
// the parsed block plus the index of the first line past it. Items collect their
// own indented continuation/child lines, which are dedented and parsed as blocks
// — so a deeper-indented list becomes a nested <ul>/<ol> via recursion.
function parseList(lines: string[], start: number): { block: Block; next: number } {
  const m0 = LIST_ITEM.exec(lines[start]) as RegExpExecArray;
  const baseIndent = m0[1].length;
  const ordered = isOrdered(m0[2]);
  const startNum = ordered ? parseInt(m0[2], 10) : undefined;
  const items: ListItem[] = [];
  let i = start;

  while (i < lines.length) {
    if (lines[i].trim() === "") {
      // Blank line: keep the list open only if the next non-blank line is a
      // sibling item or an indented child; otherwise the list ends.
      let j = i + 1;
      while (j < lines.length && lines[j].trim() === "") j++;
      if (j >= lines.length) break;
      const mn = LIST_ITEM.exec(lines[j]);
      const sibling = mn && mn[1].length === baseIndent && isOrdered(mn[2]) === ordered;
      if (!sibling && indentWidth(lines[j]) <= baseIndent) break;
      i = j;
      continue;
    }
    const m = LIST_ITEM.exec(lines[i]);
    if (!m || m[1].length < baseIndent || m[1].length > baseIndent || isOrdered(m[2]) !== ordered) break;

    const contentCol = m[1].length + m[2].length + m[3].length;
    const itemLines: string[] = [m[4]];
    i++;
    // Gather continuation + child lines (indented to the item's content column).
    while (i < lines.length) {
      if (lines[i].trim() === "") {
        let j = i + 1;
        while (j < lines.length && lines[j].trim() === "") j++;
        if (j < lines.length && indentWidth(lines[j]) >= contentCol) {
          itemLines.push("");
          i = j;
          continue;
        }
        break;
      }
      if (indentWidth(lines[i]) >= contentCol) {
        itemLines.push(lines[i].slice(contentCol));
        i++;
      } else {
        break;
      }
    }
    while (itemLines.length && itemLines[itemLines.length - 1] === "") itemLines.pop();

    let task: boolean | undefined;
    let checked: boolean | undefined;
    const tm = /^\[([ xX])\]\s+(.*)$/.exec(itemLines[0] ?? "");
    if (tm) {
      task = true;
      checked = tm[1].toLowerCase() === "x";
      itemLines[0] = tm[2];
    }
    items.push({ task, checked, children: parseBlocks(itemLines.join("\n")) });
  }

  return { block: { type: "list", ordered, start: startNum, items }, next: i };
}

function parseDetails(lines: string[], start: number): { block: Block; next: number } {
  const open = DETAILS_OPEN.exec(lines[start]);
  if (!open) {
    return { block: { type: "para", text: lines[start] }, next: start + 1 };
  }
  const initiallyOpen = /\bopen\b/i.test(open[1] ?? "");
  const collected: string[] = [];
  let i = start;
  while (i < lines.length) {
    let ln = lines[i];
    if (i === start) ln = ln.slice(open.index! + open[0].length);
    const close = DETAILS_CLOSE.exec(ln);
    if (close) {
      collected.push(ln.slice(0, close.index));
      i++;
      break;
    }
    collected.push(ln);
    i++;
  }
  let body = collected.join("\n");
  let summary = "세부 내용";
  const sm = SUMMARY.exec(body);
  if (sm) {
    summary = sm[1].trim() || summary;
    body = body.slice(0, sm.index) + body.slice(sm.index! + sm[0].length);
  }
  const children = parseBlocks(body.replace(/^\n+|\n+$/g, ""));
  return { block: { type: "details", summary, open: initiallyOpen, children }, next: i };
}

/** Fold a following "합계 …" paragraph into the preceding table's caption. */
export function promoteTableCaptions(blocks: Block[]): Block[] {
  const out: Block[] = [];
  for (let i = 0; i < blocks.length; i++) {
    const b = blocks[i];
    const next = blocks[i + 1];
    if (b.type === "table" && next?.type === "para" && TOTAL_CAPTION.test(next.text.trim())) {
      out.push({ ...b, caption: next.text.trim() });
      i++;
      continue;
    }
    out.push(b);
  }
  return out;
}

// Line-based block parser. Blank lines separate blocks; fences, lists, quotes,
// and tables consume their own runs of consecutive lines.
export function parseBlocks(src: string): Block[] {
  const lines = src.replace(/\r\n?/g, "\n").split("\n");
  const blocks: Block[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (line.trim() === "") {
      i++;
      continue;
    }
    // Block math: $$ … $$ (single- or multi-line) → KaTeX displayMode.
    const t = line.trim();
    if (t.startsWith("$$")) {
      const inner = t.slice(2, -2).trim();
      if (t.length > 4 && t.endsWith("$$") && inner) {
        blocks.push({ type: "mathBlock", text: inner });
        i++;
        continue;
      }
      const body: string[] = [];
      const firstRest = t.slice(2);
      if (firstRest) body.push(firstRest);
      i++;
      while (i < lines.length && !lines[i].trim().endsWith("$$")) body.push(lines[i++]);
      if (i < lines.length) {
        const content = lines[i].trim().slice(0, -2);
        if (content) body.push(content);
        i++;
      }
      blocks.push({ type: "mathBlock", text: body.join("\n").trim() });
      continue;
    }
    const fence = FENCE.exec(line);
    if (fence) {
      const lang = fence[1].trim();
      const body: string[] = [];
      i++;
      while (i < lines.length && !/^ {0,3}(?:```|~~~)\s*$/.test(lines[i])) body.push(lines[i++]);
      i++; // skip closing fence (or run off the end)
      blocks.push({ type: "code", lang, text: body.join("\n") });
      continue;
    }
    const heading = HEADING.exec(line);
    if (heading) {
      blocks.push({ type: "heading", level: heading[1].length, text: heading[2] });
      i++;
      continue;
    }
    // Setext headings (native BlockScanner parity) — before bare HR so
    // "Title\n---" becomes h2, while a lone "---" stays a rule.
    if (
      i + 1 < lines.length &&
      !LIST_ITEM.test(line) &&
      !QUOTE.test(line) &&
      !HR.test(line) &&
      !DETAILS_OPEN.test(line)
    ) {
      const next = lines[i + 1];
      if (SETEXT_H1.test(next)) {
        blocks.push({ type: "heading", level: 1, text: line.trim() });
        i += 2;
        continue;
      }
      if (SETEXT_H2.test(next)) {
        blocks.push({ type: "heading", level: 2, text: line.trim() });
        i += 2;
        continue;
      }
    }
    if (HR.test(line)) {
      blocks.push({ type: "hr" });
      i++;
      continue;
    }
    if (DETAILS_OPEN.test(line)) {
      const { block, next } = parseDetails(lines, i);
      blocks.push(block);
      i = next;
      continue;
    }
    // Table: a pipe row immediately followed by a |:--|--:| separator.
    if (i + 1 < lines.length && looksLikeTableStart(line, lines[i + 1])) {
      const header = splitRow(line);
      const align = splitRow(lines[i + 1]).map(cellAlign);
      i += 2;
      const rows: string[][] = [];
      while (i < lines.length && lines[i].includes("|") && lines[i].trim() !== "") rows.push(splitRow(lines[i++]));
      blocks.push({ type: "table", header, align, rows });
      continue;
    }
    if (QUOTE.test(line)) {
      const inner: string[] = [];
      while (i < lines.length && /^ {0,3}>/.test(lines[i])) inner.push(lines[i++].replace(QUOTE, "$1"));
      blocks.push({ type: "quote", children: parseBlocks(inner.join("\n")) });
      continue;
    }
    if (LIST_ITEM.test(line)) {
      const { block, next } = parseList(lines, i);
      blocks.push(block);
      i = next;
      continue;
    }
    // Paragraph: consume until a blank line or the start of another block
    // (including a GFM table that interrupts mid-paragraph — native parity).
    const para: string[] = [];
    while (
      i < lines.length &&
      lines[i].trim() !== "" &&
      !isBlockOpener(lines[i], lines[i + 1]) &&
      // Setext underline ends a one-line paragraph as a heading instead.
      !(
        i + 1 < lines.length &&
        para.length === 0 &&
        !HR.test(lines[i]) &&
        (SETEXT_H1.test(lines[i + 1]) || SETEXT_H2.test(lines[i + 1]))
      )
    ) {
      para.push(lines[i++]);
    }
    if (para.length) blocks.push({ type: "para", text: para.join("\n") });
  }
  return promoteTableCaptions(blocks);
}
