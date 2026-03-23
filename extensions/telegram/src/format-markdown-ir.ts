// Markdown IR manipulation for Telegram chunking.
// Handles slicing, merging, splitting, and coalescing of MarkdownIR chunks
// to produce correctly sized Telegram messages.

import type { MarkdownIR } from "deneb/plugin-sdk/text-runtime";
import { chunkMarkdownIR } from "deneb/plugin-sdk/text-runtime";

export type TelegramFormattedChunk = {
  html: string;
  text: string;
};

function sliceStyleSpans(
  styles: MarkdownIR["styles"],
  start: number,
  end: number,
): MarkdownIR["styles"] {
  return styles.flatMap((span) => {
    if (span.end <= start || span.start >= end) {
      return [];
    }
    const nextStart = Math.max(span.start, start) - start;
    const nextEnd = Math.min(span.end, end) - start;
    if (nextEnd <= nextStart) {
      return [];
    }
    return [{ ...span, start: nextStart, end: nextEnd }];
  });
}

function sliceLinkSpans(
  links: MarkdownIR["links"],
  start: number,
  end: number,
): MarkdownIR["links"] {
  return links.flatMap((link) => {
    if (link.end <= start || link.start >= end) {
      return [];
    }
    const nextStart = Math.max(link.start, start) - start;
    const nextEnd = Math.min(link.end, end) - start;
    if (nextEnd <= nextStart) {
      return [];
    }
    return [{ ...link, start: nextStart, end: nextEnd }];
  });
}

function sliceMarkdownIR(ir: MarkdownIR, start: number, end: number): MarkdownIR {
  return {
    text: ir.text.slice(start, end),
    styles: sliceStyleSpans(ir.styles, start, end),
    links: sliceLinkSpans(ir.links, start, end),
  };
}

function mergeAdjacentStyleSpans(styles: MarkdownIR["styles"]): MarkdownIR["styles"] {
  const merged: MarkdownIR["styles"] = [];
  for (const span of styles) {
    const last = merged.at(-1);
    if (last && last.style === span.style && span.start <= last.end) {
      last.end = Math.max(last.end, span.end);
      continue;
    }
    merged.push({ ...span });
  }
  return merged;
}

function mergeAdjacentLinkSpans(links: MarkdownIR["links"]): MarkdownIR["links"] {
  const merged: MarkdownIR["links"] = [];
  for (const link of links) {
    const last = merged.at(-1);
    if (last && last.href === link.href && link.start <= last.end) {
      last.end = Math.max(last.end, link.end);
      continue;
    }
    merged.push({ ...link });
  }
  return merged;
}

function mergeMarkdownIRChunks(left: MarkdownIR, right: MarkdownIR): MarkdownIR {
  const offset = left.text.length;
  return {
    text: left.text + right.text,
    styles: mergeAdjacentStyleSpans([
      ...left.styles,
      ...right.styles.map((span) => ({
        ...span,
        start: span.start + offset,
        end: span.end + offset,
      })),
    ]),
    links: mergeAdjacentLinkSpans([
      ...left.links,
      ...right.links.map((link) => ({
        ...link,
        start: link.start + offset,
        end: link.end + offset,
      })),
    ]),
  };
}

function findMarkdownIRPreservedSplitIndex(text: string, start: number, limit: number): number {
  const maxEnd = Math.min(text.length, start + limit);
  if (maxEnd >= text.length) {
    return text.length;
  }

  let lastOutsideParenNewlineBreak = -1;
  let lastOutsideParenWhitespaceBreak = -1;
  let lastOutsideParenWhitespaceRunStart = -1;
  let lastAnyNewlineBreak = -1;
  let lastAnyWhitespaceBreak = -1;
  let lastAnyWhitespaceRunStart = -1;
  let parenDepth = 0;
  let sawNonWhitespace = false;

  for (let index = start; index < maxEnd; index += 1) {
    const char = text[index];
    if (char === "(") {
      sawNonWhitespace = true;
      parenDepth += 1;
      continue;
    }
    if (char === ")" && parenDepth > 0) {
      sawNonWhitespace = true;
      parenDepth -= 1;
      continue;
    }
    if (!/\s/.test(char)) {
      sawNonWhitespace = true;
      continue;
    }
    if (!sawNonWhitespace) {
      continue;
    }
    if (char === "\n") {
      lastAnyNewlineBreak = index + 1;
      if (parenDepth === 0) {
        lastOutsideParenNewlineBreak = index + 1;
      }
      continue;
    }
    const whitespaceRunStart =
      index === start || !/\s/.test(text[index - 1] ?? "") ? index : lastAnyWhitespaceRunStart;
    lastAnyWhitespaceBreak = index + 1;
    lastAnyWhitespaceRunStart = whitespaceRunStart;
    if (parenDepth === 0) {
      lastOutsideParenWhitespaceBreak = index + 1;
      lastOutsideParenWhitespaceRunStart = whitespaceRunStart;
    }
  }

  const resolveWhitespaceBreak = (breakIndex: number, runStart: number): number => {
    if (breakIndex <= start) {
      return breakIndex;
    }
    if (runStart <= start) {
      return breakIndex;
    }
    return /\s/.test(text[breakIndex] ?? "") ? runStart : breakIndex;
  };

  if (lastOutsideParenNewlineBreak > start) {
    return lastOutsideParenNewlineBreak;
  }
  if (lastOutsideParenWhitespaceBreak > start) {
    return resolveWhitespaceBreak(
      lastOutsideParenWhitespaceBreak,
      lastOutsideParenWhitespaceRunStart,
    );
  }
  if (lastAnyNewlineBreak > start) {
    return lastAnyNewlineBreak;
  }
  if (lastAnyWhitespaceBreak > start) {
    return resolveWhitespaceBreak(lastAnyWhitespaceBreak, lastAnyWhitespaceRunStart);
  }
  return maxEnd;
}

function splitMarkdownIRPreserveWhitespace(ir: MarkdownIR, limit: number): MarkdownIR[] {
  if (!ir.text) {
    return [];
  }
  const normalizedLimit = Math.max(1, Math.floor(limit));
  if (normalizedLimit <= 0 || ir.text.length <= normalizedLimit) {
    return [ir];
  }
  const chunks: MarkdownIR[] = [];
  let cursor = 0;
  while (cursor < ir.text.length) {
    const end = findMarkdownIRPreservedSplitIndex(ir.text, cursor, normalizedLimit);
    chunks.push({
      text: ir.text.slice(cursor, end),
      styles: sliceStyleSpans(ir.styles, cursor, end),
      links: sliceLinkSpans(ir.links, cursor, end),
    });
    cursor = end;
  }
  return chunks;
}

function splitTelegramChunkByHtmlLimit(
  chunk: MarkdownIR,
  htmlLimit: number,
  renderedHtmlLength: number,
): MarkdownIR[] {
  const currentTextLength = chunk.text.length;
  if (currentTextLength <= 1) {
    return [chunk];
  }
  const proportionalLimit = Math.floor(
    (currentTextLength * htmlLimit) / Math.max(renderedHtmlLength, 1),
  );
  const candidateLimit = Math.min(currentTextLength - 1, proportionalLimit);
  const splitLimit =
    Number.isFinite(candidateLimit) && candidateLimit > 0
      ? candidateLimit
      : Math.max(1, Math.floor(currentTextLength / 2));
  const split = splitMarkdownIRPreserveWhitespace(chunk, splitLimit);
  if (split.length > 1) {
    return split;
  }
  return splitMarkdownIRPreserveWhitespace(chunk, Math.max(1, Math.floor(currentTextLength / 2)));
}

function coalesceWhitespaceOnlyMarkdownIRChunks(
  chunks: MarkdownIR[],
  limit: number,
  renderChunkHtml: (chunk: MarkdownIR) => string,
): MarkdownIR[] {
  const coalesced: MarkdownIR[] = [];
  let index = 0;

  while (index < chunks.length) {
    const chunk = chunks[index];
    if (!chunk) {
      index += 1;
      continue;
    }
    if (chunk.text.trim().length > 0) {
      coalesced.push(chunk);
      index += 1;
      continue;
    }

    const prev = coalesced.at(-1);
    const next = chunks[index + 1];
    const chunkLength = chunk.text.length;

    const canMergePrev = (candidate: MarkdownIR) => renderChunkHtml(candidate).length <= limit;
    const canMergeNext = (candidate: MarkdownIR) => renderChunkHtml(candidate).length <= limit;

    if (prev) {
      const mergedPrev = mergeMarkdownIRChunks(prev, chunk);
      if (canMergePrev(mergedPrev)) {
        coalesced[coalesced.length - 1] = mergedPrev;
        index += 1;
        continue;
      }
    }

    if (next) {
      const mergedNext = mergeMarkdownIRChunks(chunk, next);
      if (canMergeNext(mergedNext)) {
        chunks[index + 1] = mergedNext;
        index += 1;
        continue;
      }
    }

    if (prev && next) {
      for (let prefixLength = chunkLength - 1; prefixLength >= 1; prefixLength -= 1) {
        const prefix = sliceMarkdownIR(chunk, 0, prefixLength);
        const suffix = sliceMarkdownIR(chunk, prefixLength, chunkLength);
        const mergedPrev = mergeMarkdownIRChunks(prev, prefix);
        const mergedNext = mergeMarkdownIRChunks(suffix, next);
        if (canMergePrev(mergedPrev) && canMergeNext(mergedNext)) {
          coalesced[coalesced.length - 1] = mergedPrev;
          chunks[index + 1] = mergedNext;
          break;
        }
      }
    }

    index += 1;
  }

  return coalesced;
}

export function renderTelegramChunksWithinHtmlLimit(
  ir: MarkdownIR,
  limit: number,
  renderChunkHtml: (chunk: MarkdownIR) => string,
): TelegramFormattedChunk[] {
  const normalizedLimit = Math.max(1, Math.floor(limit));
  const pending = chunkMarkdownIR(ir, normalizedLimit);
  const finalized: MarkdownIR[] = [];
  while (pending.length > 0) {
    const chunk = pending.shift();
    if (!chunk) {
      continue;
    }
    const html = renderChunkHtml(chunk);
    if (html.length <= normalizedLimit || chunk.text.length <= 1) {
      finalized.push(chunk);
      continue;
    }
    const split = splitTelegramChunkByHtmlLimit(chunk, normalizedLimit, html.length);
    if (split.length <= 1) {
      // Worst-case safety: avoid retry loops, deliver the chunk as-is.
      finalized.push(chunk);
      continue;
    }
    pending.unshift(...split);
  }
  return coalesceWhitespaceOnlyMarkdownIRChunks(finalized, normalizedLimit, renderChunkHtml).map(
    (chunk) => ({
      html: renderChunkHtml(chunk),
      text: chunk.text,
    }),
  );
}
