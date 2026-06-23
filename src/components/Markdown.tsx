// Dependency-free, XSS-safe Markdown → React renderer for Deneb's chat replies,
// mail bodies, and wiki/doc previews. Covers GFM at the level the native client
// renders: ATX headings, bold/italic/strikethrough, inline + fenced code (with a
// language label + copy), links/images/autolinks (http/https/mailto only),
// nested ordered/unordered lists, task-list checkboxes, blockquotes (with block
// content), aligned GFM tables, hard line breaks, backslash escapes, and rules.
//
// Output is React elements (never dangerouslySetInnerHTML) so every text node is
// auto-escaped — no HTML sanitizer needed. Matches the repo's "dependency-free
// Icon set" idiom rather than pulling react-markdown + remark and its
// build-script supply-chain gate. (KaTeX math is the one deliberate gap — true
// math layout needs a dependency; see DESIGN follow-up.)
import { type CSSProperties, type ReactNode, useState } from "react";
import { Icon } from "./Icon";

type Align = "left" | "center" | "right" | undefined;

interface ListItem {
  task?: boolean; // a GFM task-list item ("- [ ] …")
  checked?: boolean;
  children: Block[]; // item body as blocks → supports nested lists / multi-paragraph
}

type Block =
  | { type: "heading"; level: number; text: string }
  | { type: "code"; lang: string; text: string }
  | { type: "list"; ordered: boolean; start?: number; items: ListItem[] }
  | { type: "quote"; children: Block[] }
  | { type: "table"; header: string[]; align: Align[]; rows: string[][] }
  | { type: "hr" }
  | { type: "para"; text: string };

const FENCE = /^ {0,3}(?:```|~~~)(.*)$/;
const HEADING = /^ {0,3}(#{1,6})\s+(.*?)\s*#*\s*$/;
const HR = /^ {0,3}([-*_])(?:[ \t]*\1){2,}[ \t]*$/;
const QUOTE = /^ {0,3}>\s?(.*)$/;
const LIST_ITEM = /^(\s*)([-*+]|\d{1,9}[.)])(\s+)(.*)$/;
const TABLE_SEP = /^\s*\|?(?:\s*:?-+:?\s*\|)+(?:\s*:?-+:?\s*)?\|?\s*$/;

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

// Line-based block parser. Blank lines separate blocks; fences, lists, quotes,
// and tables consume their own runs of consecutive lines.
function parseBlocks(src: string): Block[] {
  const lines = src.replace(/\r\n?/g, "\n").split("\n");
  const blocks: Block[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (line.trim() === "") {
      i++;
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
    if (HR.test(line)) {
      blocks.push({ type: "hr" });
      i++;
      continue;
    }
    // Table: a pipe row immediately followed by a |:--|--:| separator.
    if (line.includes("|") && i + 1 < lines.length && TABLE_SEP.test(lines[i + 1]) && lines[i + 1].includes("-")) {
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
    // Paragraph: consume until a blank line or the start of another block.
    const para: string[] = [];
    while (
      i < lines.length &&
      lines[i].trim() !== "" &&
      !FENCE.test(lines[i]) &&
      !HEADING.test(lines[i]) &&
      !HR.test(lines[i]) &&
      !LIST_ITEM.test(lines[i]) &&
      !QUOTE.test(lines[i])
    ) {
      para.push(lines[i++]);
    }
    blocks.push({ type: "para", text: para.join("\n") });
  }
  return blocks;
}

// Only these schemes render as live links/images; anything else stays plain text
// so a `javascript:`/`data:` URL can never become a clickable or fetchable node.
function safeHref(url: string): string | null {
  const u = url.trim();
  return /^(https?:\/\/|mailto:)/i.test(u) ? u : null;
}

// Earliest-match inline tokenizer. At each step the pattern that OPENS first wins
// (ties broken by array order), so escapes and images beat the weaker emphasis
// markers. Each alternative captures its inner text for recursive rendering.
const INLINE: { kind: string; re: RegExp }[] = [
  { kind: "esc", re: /\\([\\`*_{}[\]()#+\-.!~>|"'$])/ },
  { kind: "br", re: /\n/ },
  { kind: "image", re: /!\[([^\]]*)\]\(([^)\s]+)\)/ },
  { kind: "code", re: /`([^`]+)`/ },
  { kind: "autolinkAngle", re: /<((?:https?:\/\/|mailto:)[^>\s]+)>/ },
  { kind: "link", re: /\[([^\]]+)\]\(([^)\s]+)\)/ },
  { kind: "strike", re: /~~([^~]+)~~/ },
  { kind: "bold", re: /\*\*([^*]+)\*\*|__([^_]+)__/ },
  { kind: "italic", re: /\*([^*\n]+)\*|\b_([^_\n]+)_\b/ },
  { kind: "autolink", re: /(https?:\/\/[^\s<>)\]]+)/ },
];

function renderInline(text: string, key: string): ReactNode[] {
  const out: ReactNode[] = [];
  let rest = text;
  let n = 0;
  while (rest.length > 0) {
    let best: { idx: number; len: number; kind: string; m: RegExpExecArray } | null = null;
    for (const { kind, re } of INLINE) {
      const m = re.exec(rest);
      if (m && (best === null || m.index < best.idx)) best = { idx: m.index, len: m[0].length, kind, m };
    }
    if (!best) {
      out.push(rest);
      break;
    }
    const k = `${key}-${n++}`;
    const { kind, m } = best;
    // Text before the token. For a line break, strip the trailing hard-break
    // marker (2+ spaces or a backslash) so it doesn't render as literal space.
    let before = rest.slice(0, best.idx);
    if (kind === "br") before = before.replace(/[ \t]+$|\\$/, "");
    if (before) out.push(before);

    if (kind === "esc") {
      out.push(m[1]);
    } else if (kind === "br") {
      out.push(<br key={k} />);
    } else if (kind === "image") {
      const src = safeHref(m[2]);
      out.push(src ? <img key={k} className="md-img" src={src} alt={m[1]} loading="lazy" /> : m[1]);
    } else if (kind === "code") {
      out.push(
        <code key={k} className="md-code">
          {m[1]}
        </code>,
      );
    } else if (kind === "autolinkAngle" || kind === "autolink") {
      let url = m[1];
      let trail = "";
      if (kind === "autolink") {
        const t = /[.,;:!?)\]}'"]+$/.exec(url);
        if (t) {
          trail = t[0];
          url = url.slice(0, -trail.length);
        }
      }
      const href = safeHref(url);
      out.push(
        href ? (
          <a key={k} href={href} target="_blank" rel="noreferrer noopener">
            {url}
          </a>
        ) : (
          url
        ),
      );
      if (trail) out.push(trail);
    } else if (kind === "link") {
      const href = safeHref(m[2]);
      out.push(
        href ? (
          <a key={k} href={href} target="_blank" rel="noreferrer noopener">
            {renderInline(m[1], k)}
          </a>
        ) : (
          renderInline(m[1], k)
        ),
      );
    } else if (kind === "strike") {
      out.push(<del key={k}>{renderInline(m[1], k)}</del>);
    } else if (kind === "bold") {
      out.push(<strong key={k}>{renderInline(m[1] ?? m[2], k)}</strong>);
    } else {
      out.push(<em key={k}>{renderInline(m[1] ?? m[2], k)}</em>);
    }
    rest = rest.slice(best.idx + best.len);
  }
  return out;
}

// A fenced code block: language label + copy affordance over a monospaced pre.
function CodeBlock({ lang, text }: { lang: string; text: string }) {
  const [copied, setCopied] = useState(false);
  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    } catch {
      /* clipboard blocked — no-op */
    }
  }
  return (
    <div className="md-codeblock">
      <div className="md-codebar">
        {lang ? <span className="md-codelang">{lang}</span> : <span />}
        <button type="button" className="md-copy" onClick={copy} aria-label="코드 복사">
          <Icon name="copy" size={12} />
          {copied ? "복사됨" : "복사"}
        </button>
      </div>
      <pre>
        <code data-lang={lang || undefined}>{text}</code>
      </pre>
    </div>
  );
}

function renderItemBody(item: ListItem, key: string): ReactNode {
  // Tight item (single paragraph) renders inline so list rows stay compact;
  // a multi-block item (nested list, multiple paragraphs) renders its blocks.
  if (item.children.length === 1 && item.children[0].type === "para") {
    return renderInline(item.children[0].text, key);
  }
  return item.children.map((b, j) => renderBlock(b, `${key}-${j}`));
}

function renderList(b: Extract<Block, { type: "list" }>, key: string): ReactNode {
  const hasTask = b.items.some((it) => it.task);
  const children = b.items.map((it, j) => {
    const body = renderItemBody(it, `${key}-${j}`);
    return it.task ? (
      <li key={j} className="md-task">
        <input type="checkbox" checked={!!it.checked} disabled readOnly />
        <span>{body}</span>
      </li>
    ) : (
      <li key={j}>{body}</li>
    );
  });
  const cls = hasTask ? "md-tasklist" : undefined;
  return b.ordered ? (
    <ol key={key} className={cls} start={b.start && b.start !== 1 ? b.start : undefined}>
      {children}
    </ol>
  ) : (
    <ul key={key} className={cls}>
      {children}
    </ul>
  );
}

function renderBlock(b: Block, key: string): ReactNode {
  switch (b.type) {
    case "heading": {
      const Tag = `h${Math.min(b.level + 2, 6)}` as "h3" | "h4" | "h5" | "h6";
      return <Tag key={key}>{renderInline(b.text, key)}</Tag>;
    }
    case "code":
      return <CodeBlock key={key} lang={b.lang} text={b.text} />;
    case "hr":
      return <hr key={key} />;
    case "quote":
      return <blockquote key={key}>{b.children.map((c, j) => renderBlock(c, `${key}-${j}`))}</blockquote>;
    case "list":
      return renderList(b, key);
    case "table":
      return (
        <table key={key} className="md-table">
          <thead>
            <tr>
              {b.header.map((h, j) => (
                <th key={j} style={alignStyle(b.align[j])}>
                  {renderInline(h, `${key}-h${j}`)}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {b.rows.map((row, r) => (
              <tr key={r}>
                {row.map((c, j) => (
                  <td key={j} style={alignStyle(b.align[j])}>
                    {renderInline(c, `${key}-${r}-${j}`)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      );
    case "para":
      return <p key={key}>{renderInline(b.text, key)}</p>;
  }
}

function alignStyle(a: Align): CSSProperties | undefined {
  return a ? { textAlign: a } : undefined;
}

// Render Markdown text as safe React nodes. Plain text with no Markdown renders
// as a single <p>, so a bare reply stays a clean paragraph.
export function Markdown({ text }: { text: string }) {
  const blocks = parseBlocks(text);
  return <div className="md">{blocks.map((b, i) => renderBlock(b, `b${i}`))}</div>;
}
