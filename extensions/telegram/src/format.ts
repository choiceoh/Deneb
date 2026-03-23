// Telegram message formatting: markdown → HTML conversion and chunking.
// Core rendering API. Sub-modules:
//   format-file-refs.ts     — file reference detection and wrapping
//   format-html-chunking.ts — HTML tag-aware chunking
//   format-markdown-ir.ts   — markdown IR manipulation and splitting

import type { MarkdownTableMode } from "deneb/plugin-sdk/config-runtime";
import { markdownToIR, type MarkdownIR } from "deneb/plugin-sdk/text-runtime";
import { renderMarkdownWithMarkers } from "deneb/plugin-sdk/text-runtime";
import { buildTelegramLink, escapeHtml, wrapFileReferencesInHtml } from "./format-file-refs.js";
import {
  renderTelegramChunksWithinHtmlLimit,
  type TelegramFormattedChunk,
} from "./format-markdown-ir.js";

export type { TelegramFormattedChunk } from "./format-markdown-ir.js";

function renderTelegramHtml(ir: MarkdownIR): string {
  return renderMarkdownWithMarkers(ir, {
    styleMarkers: {
      bold: { open: "<b>", close: "</b>" },
      italic: { open: "<i>", close: "</i>" },
      strikethrough: { open: "<s>", close: "</s>" },
      code: { open: "<code>", close: "</code>" },
      code_block: { open: "<pre><code>", close: "</code></pre>" },
      spoiler: { open: "<tg-spoiler>", close: "</tg-spoiler>" },
      blockquote: { open: "<blockquote>", close: "</blockquote>" },
    },
    escapeText: escapeHtml,
    buildLink: buildTelegramLink,
  });
}

/** Render a single MarkdownIR chunk to Telegram HTML with file reference wrapping. */
export function renderTelegramChunkHtml(ir: MarkdownIR): string {
  return wrapFileReferencesInHtml(renderTelegramHtml(ir));
}

export function markdownToTelegramHtml(
  markdown: string,
  options: { tableMode?: MarkdownTableMode; wrapFileRefs?: boolean } = {},
): string {
  const ir = markdownToIR(markdown ?? "", {
    linkify: true,
    enableSpoilers: true,
    headingStyle: "none",
    blockquotePrefix: "",
    tableMode: options.tableMode,
  });
  const html = renderTelegramHtml(ir);
  // Apply file reference wrapping if requested (for chunked rendering)
  if (options.wrapFileRefs !== false) {
    return wrapFileReferencesInHtml(html);
  }
  return html;
}

export function renderTelegramHtmlText(
  text: string,
  options: { textMode?: "markdown" | "html"; tableMode?: MarkdownTableMode } = {},
): string {
  const textMode = options.textMode ?? "markdown";
  if (textMode === "html") {
    // For HTML mode, trust caller markup - don't modify
    return text;
  }
  // markdownToTelegramHtml already wraps file references by default
  return markdownToTelegramHtml(text, { tableMode: options.tableMode });
}

export function markdownToTelegramChunks(
  markdown: string,
  limit: number,
  options: { tableMode?: MarkdownTableMode } = {},
): TelegramFormattedChunk[] {
  const ir = markdownToIR(markdown ?? "", {
    linkify: true,
    enableSpoilers: true,
    headingStyle: "none",
    blockquotePrefix: "",
    tableMode: options.tableMode,
  });
  return renderTelegramChunksWithinHtmlLimit(ir, limit, renderTelegramChunkHtml);
}

export function markdownToTelegramHtmlChunks(markdown: string, limit: number): string[] {
  return markdownToTelegramChunks(markdown, limit).map((chunk) => chunk.html);
}

// Re-export sub-module public APIs for backward compatibility
export { escapeHtml, wrapFileReferencesInHtml } from "./format-file-refs.js";
export { splitTelegramHtmlChunks } from "./format-html-chunking.js";
