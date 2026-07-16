// Pre-passes mirroring native MarkdownParser.parseMarkdown:
// footnotes → html → box → pipe.
import { normalizeBoxTables } from "./normalizeBox";
import { normalizeFootnotes } from "./normalizeFootnotes";
import { normalizeHtmlBlocks } from "./normalizeHtml";
import { normalizePipeTables } from "./normalizePipe";

export function normalizeMarkdown(text: string): string {
  if (!text) return text;
  return normalizePipeTables(normalizeBoxTables(normalizeHtmlBlocks(normalizeFootnotes(text))));
}

export { normalizeBoxTables, normalizeFootnotes, normalizeHtmlBlocks, normalizePipeTables };
