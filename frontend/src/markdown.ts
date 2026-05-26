// markdown.ts — minimal Markdown → HTML renderer for analysis output.
//
// The Gmail analysis prompt produces predictable markdown: a small set
// of `##` headers, `**bold**` emphasis, `-` / `*` bullet lists, and
// numbered lists. Pulling in a full Markdown library (~30 KB) would
// dwarf the Mini App bundle, so we render just the patterns we know to
// expect. Anything we don't recognize comes through as plain text.
//
// Security: we escape all input first, then re-introduce a very small
// set of tags. No raw HTML from the analysis ever lands in the DOM.

const escapeMap: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
};

function escapeHTML(s: string): string {
  return s.replace(/[&<>"']/g, (c) => escapeMap[c]);
}

function inlineFormat(s: string): string {
  // Bold first (greedy avoidance via lazy match).
  s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  // Italic (single asterisk) — be careful to not eat the inner bold we
  // just emitted; the closing tag had asterisks stripped already.
  s = s.replace(/(^|[^*])\*([^*]+)\*(?!\*)/g, '$1<em>$2</em>');
  // Inline code.
  s = s.replace(/`([^`]+)`/g, '<code>$1</code>');
  return s;
}

/**
 * renderMarkdown produces an HTML string suitable for setting via
 * innerHTML on a styled container. Headers (##/###/####), bullet lists
 * (- or *), numbered lists, blockquotes (>), horizontal rules (---),
 * and inline bold/italic/code are supported.
 */
export function renderMarkdown(src: string): string {
  const escaped = escapeHTML(src);
  const lines = escaped.split('\n');
  const out: string[] = [];
  let inUl = false;
  let inOl = false;
  let inQuote = false;

  const closeLists = () => {
    if (inUl) {
      out.push('</ul>');
      inUl = false;
    }
    if (inOl) {
      out.push('</ol>');
      inOl = false;
    }
  };
  const closeQuote = () => {
    if (inQuote) {
      out.push('</blockquote>');
      inQuote = false;
    }
  };

  for (const rawLine of lines) {
    const line = rawLine.replace(/\s+$/, '');

    if (line === '') {
      closeLists();
      closeQuote();
      continue;
    }

    // Horizontal rule.
    if (/^---+$/.test(line)) {
      closeLists();
      closeQuote();
      out.push('<hr />');
      continue;
    }

    // Headers.
    const h = /^(#{2,4})\s+(.*)$/.exec(line);
    if (h) {
      closeLists();
      closeQuote();
      const level = h[1].length;
      out.push(`<h${level}>${inlineFormat(h[2])}</h${level}>`);
      continue;
    }

    // Bullet list.
    const ul = /^[-*]\s+(.*)$/.exec(line);
    if (ul) {
      closeQuote();
      if (inOl) {
        out.push('</ol>');
        inOl = false;
      }
      if (!inUl) {
        out.push('<ul>');
        inUl = true;
      }
      out.push(`<li>${inlineFormat(ul[1])}</li>`);
      continue;
    }

    // Numbered list.
    const ol = /^\d+\.\s+(.*)$/.exec(line);
    if (ol) {
      closeQuote();
      if (inUl) {
        out.push('</ul>');
        inUl = false;
      }
      if (!inOl) {
        out.push('<ol>');
        inOl = true;
      }
      out.push(`<li>${inlineFormat(ol[1])}</li>`);
      continue;
    }

    // Blockquote.
    const q = /^>\s?(.*)$/.exec(line);
    if (q) {
      closeLists();
      if (!inQuote) {
        out.push('<blockquote>');
        inQuote = true;
      }
      out.push(`<p>${inlineFormat(q[1])}</p>`);
      continue;
    }

    // Paragraph.
    closeLists();
    closeQuote();
    out.push(`<p>${inlineFormat(line)}</p>`);
  }
  closeLists();
  closeQuote();
  return out.join('\n');
}
