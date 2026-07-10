import type { ReactNode } from "react";

/** Inline emphasis for text values: **bold** / *italic* / `code` / [text](url).
 * Links are load-bearing, not cosmetic: the HTML parser merges <a href> into
 * `[label](url)` markdown (denebUiHtml.ts), and the native InlineTokenizer
 * renders it as a real link — desktop must not leak the literal brackets. */
export function renderInline(text: string, keyBase: string): ReactNode {
  if (!/[*`[]/.test(text)) return text;
  const out: ReactNode[] = [];
  const re = /(\*\*([^*]+)\*\*|`([^`]+)`|\*([^*]+)\*|\[([^\]]+)\]\(([^)]+)\))/g;
  let last = 0;
  let m: RegExpExecArray | null;
  let i = 0;
  while ((m = re.exec(text))) {
    if (m.index > last) out.push(text.slice(last, m.index));
    if (m[2] !== undefined) out.push(<strong key={`${keyBase}b${i}`}>{m[2]}</strong>);
    else if (m[3] !== undefined)
      out.push(
        <code key={`${keyBase}c${i}`} className="dui-inline-code">
          {m[3]}
        </code>,
      );
    else if (m[4] !== undefined) out.push(<em key={`${keyBase}i${i}`}>{m[4]}</em>);
    else if (m[5] !== undefined)
      out.push(
        <a key={`${keyBase}a${i}`} href={m[6]} target="_blank" rel="noopener noreferrer">
          {m[5]}
        </a>,
      );
    last = m.index + m[0].length;
    i += 1;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}
