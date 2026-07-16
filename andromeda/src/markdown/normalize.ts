// Pre-passes mirroring native MarkdownParser.parseMarkdown:
// footnotes omitted (rare on desktop approval/mail); html → box → pipe.
import { normalizeBoxTables } from "./normalizeBox";
import { normalizeHtmlBlocks } from "./normalizeHtml";
import { normalizePipeTables } from "./normalizePipe";

export function normalizeMarkdown(text: string): string {
  if (!text) return text;
  return normalizePipeTables(normalizeBoxTables(normalizeHtmlBlocks(text)));
}

export { normalizeBoxTables, normalizeHtmlBlocks, normalizePipeTables };
