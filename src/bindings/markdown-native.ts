/**
 * Optional loader for markdown native functions from the unified @deneb/native addon.
 * These functions are loaded opportunistically — the addon works fine without them.
 */

import { loadRawAddon } from "./native.js";

export type JsStyleSpan = {
  start: number;
  end: number;
  style: string;
};

export type JsLinkSpan = {
  start: number;
  end: number;
  href: string;
};

export type JsFenceSpan = {
  start: number;
  end: number;
  openLine: string;
  marker: string;
  indent: string;
};

export type JsMarkdownIR = {
  text: string;
  styles: JsStyleSpan[];
  links: JsLinkSpan[];
};

export type JsParseOptions = {
  linkify?: boolean;
  enableSpoilers?: boolean;
  headingStyle?: string;
  blockquotePrefix?: string;
  autolink?: boolean;
  tableMode?: string;
};

export type JsMarkdownIRWithMeta = {
  ir: JsMarkdownIR;
  hasTables: boolean;
};

export interface MarkdownNativeModule {
  markdownMergeStyleSpans(spans: JsStyleSpan[]): JsStyleSpan[];
  markdownClampStyleSpans(spans: JsStyleSpan[], maxLength: number): JsStyleSpan[];
  markdownClampLinkSpans(spans: JsLinkSpan[], maxLength: number): JsLinkSpan[];
  markdownSliceStyleSpans(spans: JsStyleSpan[], start: number, end: number): JsStyleSpan[];
  markdownSliceLinkSpans(spans: JsLinkSpan[], start: number, end: number): JsLinkSpan[];
  markdownParseFenceSpans(buffer: string): JsFenceSpan[];
  markdownIsSafeFenceBreak(spans: JsFenceSpan[], index: number): boolean;
  markdownIsInsideCode(text: string, position: number): boolean;
  markdownBuildCodeSpanState(
    text: string,
    initialOpen?: boolean | null,
    initialTicks?: number | null,
  ): { open: boolean; ticks: number };
  markdownToIr(markdown: string, options?: JsParseOptions | null): JsMarkdownIR;
  markdownToIrWithMeta(markdown: string, options?: JsParseOptions | null): JsMarkdownIRWithMeta;
}

let markdownNative: MarkdownNativeModule | null = null;
let loaded = false;

/**
 * Load markdown native functions from the addon.
 * Returns null if the addon is unavailable or doesn't have markdown functions.
 * Result is cached.
 */
export function loadMarkdownNative(): MarkdownNativeModule | null {
  if (loaded) {
    return markdownNative;
  }
  loaded = true;
  const raw = loadRawAddon();
  if (!raw || typeof raw.markdownMergeStyleSpans !== "function") {
    markdownNative = null;
    return markdownNative;
  }
  markdownNative = raw as unknown as MarkdownNativeModule;
  return markdownNative;
}
