/**
 * Lazy loader for core-rs functions from the unified @deneb/native addon.
 * Exposes protocol validation, security primitives, and media detection.
 * Falls back gracefully when the addon is not available.
 */

import { loadRawAddon } from "./native.js";

/** Frame type IDs returned by native validateFrame (matches Rust enum order). */
const FRAME_TYPES = ["req", "res", "event"] as const;

/** Raw native module interface (internal — use CoreRsModule wrapper). */
interface CoreRsModuleRaw {
  validateFrame(json: string): number;
  constantTimeEq(a: Buffer, b: Buffer): boolean;
  detectMime(data: Buffer): string;
  validateSessionKey(key: string): boolean;
  sanitizeHtml(input: string): string;
  isSafeUrl(url: string): boolean;
  validateErrorCode(code: string): boolean;
  isRetryableErrorCode(code: string): boolean;
  validateParams(method: string, json: string): string;
  // Security: input validation / sanitization
  isSafeInput(input: string): boolean;
  sanitizeControlChars(input: string): string;
  stripInvisibleUnicode(input: string): string;
  // Parsing functions
  parsingHtmlToMarkdown(html: string): string;
  parsingExtractLinks(text: string, configJson: string): string;
  parsingSplitMediaFromOutput(raw: string): string;
  parsingEstimateBase64DecodedBytes(input: string): number;
  parsingCanonicalizeBase64(input: string): string | null;
  // Media helper functions
  mediaExtensionForMime(mime: string): string;
  mediaCategoryForMime(mime: string): string;
  mediaDetectMimeWithInfo(data: Buffer): string;
  mediaIsImage(mime: string): boolean;
  mediaIsAudio(mime: string): boolean;
  mediaIsVideo(mime: string): boolean;
  // Memory search functions
  memoryCosineSimilarity(a: number[], b: number[]): number;
  memoryBm25RankToScore(rank: number): number;
  memoryBuildFtsQuery(raw: string): string | null;
  memoryTemporalDecayMultiplier(ageInDays: number, halfLifeDays: number): number;
  memoryApplyTemporalDecay(score: number, ageInDays: number, halfLifeDays: number): number;
  memoryParseMemoryDateFromPath(filePath: string): string | null;
  memoryIsEvergreenMemoryPath(filePath: string): boolean;
  memoryMmrRerank(itemsJson: string, configJson: string): string;
  memoryExtractKeywords(query: string): string[];
  memoryIsQueryStopWord(token: string): boolean;
  memoryExpandQueryForFts(query: string): string;
  memoryMergeHybridResults(paramsJson: string): string;
  memoryTextSimilarity(a: string, b: string): number;
}

/** Validation error returned from native param validation. */
export interface NativeValidationError {
  path: string;
  message: string;
  keyword: string;
}

/** Result of native param validation. */
export interface NativeValidationResult {
  valid: boolean;
  errors?: NativeValidationError[];
}

/** Result of parsing HTML to markdown. */
export interface HtmlToMarkdownResult {
  text: string;
  title?: string;
}

/** Result of extracting media tokens from output. */
export interface MediaParseResult {
  text: string;
  media_urls?: string[];
  media_url?: string;
  audio_as_voice?: boolean;
}

/** MIME detection result with extension and category. */
export interface MimeDetectInfo {
  mime: string;
  extension: string;
  category: string;
}

export interface CoreRsModule {
  /** Validate a gateway protocol frame. Returns frame type ("req"/"res"/"event"). Throws on invalid. */
  validateFrame(json: string): string;
  /** Constant-time byte comparison (timing-attack safe). */
  constantTimeEq(a: Buffer, b: Buffer): boolean;
  /** Detect MIME type from file magic bytes. */
  detectMime(data: Buffer): string;
  /** Validate a session key (non-empty, max 512 chars, no control chars). */
  validateSessionKey(key: string): boolean;
  /** Escape HTML-significant characters to prevent XSS. */
  sanitizeHtml(input: string): string;
  /** Check if a URL is safe for outbound requests (SSRF protection). */
  isSafeUrl(url: string): boolean;
  /** Check if an error code string is a known gateway error code. */
  validateErrorCode(code: string): boolean;
  /** Check if an error code is retryable by default. */
  isRetryableErrorCode(code: string): boolean;
  /** Validate RPC parameters for a method. Returns validation result with errors. */
  validateParams(method: string, json: string): NativeValidationResult;
  /** Check if input contains potential injection patterns (null bytes, XSS). */
  isSafeInput(input: string): boolean;
  /** Remove control characters except newline/tab/CR. */
  sanitizeControlChars(input: string): string;
  /** Remove invisible Unicode characters (zero-width, bidi marks, etc.). */
  stripInvisibleUnicode(input: string): string;
  /** Convert HTML to markdown. Returns JSON {text, title?}. */
  parsingHtmlToMarkdown(html: string): HtmlToMarkdownResult;
  /** Extract safe links from text. */
  parsingExtractLinks(text: string, maxLinks?: number): string[];
  /** Parse MEDIA: tokens from command output. */
  parsingSplitMediaFromOutput(raw: string): MediaParseResult;
  /** Estimate decoded byte size from base64 string length. */
  parsingEstimateBase64DecodedBytes(input: string): number;
  /** Normalize and validate a base64 string. Returns canonical form or null. */
  parsingCanonicalizeBase64(input: string): string | null;
  /** Get file extension for a MIME type. */
  mediaExtensionForMime(mime: string): string;
  /** Get media category for a MIME type ("image"/"audio"/"video"/"document"/"archive"/"text"/"unknown"). */
  mediaCategoryForMime(mime: string): string;
  /** Detect MIME with full info (mime, extension, category). */
  mediaDetectMimeWithInfo(data: Buffer): MimeDetectInfo;
  /** Check if MIME type is an image. */
  mediaIsImage(mime: string): boolean;
  /** Check if MIME type is audio. */
  mediaIsAudio(mime: string): boolean;
  /** Check if MIME type is video. */
  mediaIsVideo(mime: string): boolean;
  /** Memory search: cosine similarity between two vectors. */
  memoryCosineSimilarity(a: number[], b: number[]): number;
  /** Memory search: BM25 rank to [0,1] score. */
  memoryBm25RankToScore(rank: number): number;
  /** Memory search: build FTS5 query. */
  memoryBuildFtsQuery(raw: string): string | null;
  /** Memory search: temporal decay multiplier. */
  memoryTemporalDecayMultiplier(ageInDays: number, halfLifeDays: number): number;
  /** Memory search: apply temporal decay to score. */
  memoryApplyTemporalDecay(score: number, ageInDays: number, halfLifeDays: number): number;
  /** Memory search: parse date from memory path. Returns ISO date or null. */
  memoryParseMemoryDateFromPath(filePath: string): string | null;
  /** Memory search: check if path is evergreen memory. */
  memoryIsEvergreenMemoryPath(filePath: string): boolean;
  /** Memory search: MMR re-rank (JSON in/out). */
  memoryMmrRerank(itemsJson: string, configJson: string): string;
  /** Memory search: extract keywords from query. */
  memoryExtractKeywords(query: string): string[];
  /** Memory search: check if token is a stop word. */
  memoryIsQueryStopWord(token: string): boolean;
  /** Memory search: expand query for FTS (JSON). */
  memoryExpandQueryForFts(query: string): string;
  /** Memory search: merge hybrid results (JSON in/out). */
  memoryMergeHybridResults(paramsJson: string): string;
  /** Memory search: Jaccard text similarity. */
  memoryTextSimilarity(a: string, b: string): number;
}

/** Wraps the raw native module, mapping numeric frame type IDs to strings. */
function wrapModule(raw: CoreRsModuleRaw): CoreRsModule {
  return {
    validateFrame(json: string): string {
      const id = raw.validateFrame(json);
      const ft = FRAME_TYPES[id];
      if (!ft) {
        throw new Error(`unknown frame type id: ${id}`);
      }
      return ft;
    },
    constantTimeEq: (a: Buffer, b: Buffer) => raw.constantTimeEq(a, b),
    detectMime: (data: Buffer) => raw.detectMime(data),
    validateSessionKey: (key: string) => raw.validateSessionKey(key),
    sanitizeHtml: (input: string) => raw.sanitizeHtml(input),
    isSafeUrl: (url: string) => raw.isSafeUrl(url),
    validateErrorCode: (code: string) => raw.validateErrorCode(code),
    isRetryableErrorCode: (code: string) => raw.isRetryableErrorCode(code),
    validateParams(method: string, json: string): NativeValidationResult {
      const resultJson = raw.validateParams(method, json);
      return JSON.parse(resultJson) as NativeValidationResult;
    },
    // Security: input validation / sanitization
    isSafeInput: (input: string) => raw.isSafeInput(input),
    sanitizeControlChars: (input: string) => raw.sanitizeControlChars(input),
    stripInvisibleUnicode: (input: string) => raw.stripInvisibleUnicode(input),
    // Parsing functions
    parsingHtmlToMarkdown(html: string): HtmlToMarkdownResult {
      const json = raw.parsingHtmlToMarkdown(html);
      return JSON.parse(json) as HtmlToMarkdownResult;
    },
    parsingExtractLinks(text: string, maxLinks?: number): string[] {
      const configJson = JSON.stringify({ max_links: maxLinks ?? 5 });
      const json = raw.parsingExtractLinks(text, configJson);
      return JSON.parse(json) as string[];
    },
    parsingSplitMediaFromOutput(rawText: string): MediaParseResult {
      const json = raw.parsingSplitMediaFromOutput(rawText);
      return JSON.parse(json) as MediaParseResult;
    },
    parsingEstimateBase64DecodedBytes: (input: string) =>
      raw.parsingEstimateBase64DecodedBytes(input),
    parsingCanonicalizeBase64: (input: string) => raw.parsingCanonicalizeBase64(input),
    // Media helper functions
    mediaExtensionForMime: (mime: string) => raw.mediaExtensionForMime(mime),
    mediaCategoryForMime: (mime: string) => raw.mediaCategoryForMime(mime),
    mediaDetectMimeWithInfo(data: Buffer): MimeDetectInfo {
      const json = raw.mediaDetectMimeWithInfo(data);
      return JSON.parse(json) as MimeDetectInfo;
    },
    mediaIsImage: (mime: string) => raw.mediaIsImage(mime),
    mediaIsAudio: (mime: string) => raw.mediaIsAudio(mime),
    mediaIsVideo: (mime: string) => raw.mediaIsVideo(mime),
    // Memory search passthrough
    memoryCosineSimilarity: (a: number[], b: number[]) => raw.memoryCosineSimilarity(a, b),
    memoryBm25RankToScore: (rank: number) => raw.memoryBm25RankToScore(rank),
    memoryBuildFtsQuery: (r: string) => raw.memoryBuildFtsQuery(r),
    memoryTemporalDecayMultiplier: (age: number, half: number) =>
      raw.memoryTemporalDecayMultiplier(age, half),
    memoryApplyTemporalDecay: (score: number, age: number, half: number) =>
      raw.memoryApplyTemporalDecay(score, age, half),
    memoryParseMemoryDateFromPath: (fp: string) => raw.memoryParseMemoryDateFromPath(fp),
    memoryIsEvergreenMemoryPath: (fp: string) => raw.memoryIsEvergreenMemoryPath(fp),
    memoryMmrRerank: (items: string, config: string) => raw.memoryMmrRerank(items, config),
    memoryExtractKeywords: (q: string) => raw.memoryExtractKeywords(q),
    memoryIsQueryStopWord: (t: string) => raw.memoryIsQueryStopWord(t),
    memoryExpandQueryForFts: (q: string) => raw.memoryExpandQueryForFts(q),
    memoryMergeHybridResults: (p: string) => raw.memoryMergeHybridResults(p),
    memoryTextSimilarity: (a: string, b: string) => raw.memoryTextSimilarity(a, b),
  };
}

let coreRs: CoreRsModule | null = null;

/**
 * Load core-rs functions from the unified native addon.
 * Throws if the addon is unavailable. Result is cached.
 * Shape validation and smoke tests are handled by the shared loadRawAddon().
 */
export function loadCoreRs(): CoreRsModule {
  if (coreRs) {
    return coreRs;
  }
  const raw = loadRawAddon();
  coreRs = wrapModule(raw as unknown as CoreRsModuleRaw);
  return coreRs;
}
