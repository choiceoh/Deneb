/**
 * Loader for the required @deneb/native Rust addon.
 * Throws if the addon is unavailable or has an ABI mismatch.
 *
 * This is the single entry point for loading the unified native addon.
 * Both loadNative() (gitignore/exif/png) and loadCoreRs() (protocol/security/media)
 * share the same underlying require() call via loadRawAddon().
 */

import { createRequire } from "node:module";

export interface NativeGitignoreMatcher {
  isIgnored(filePath: string, isDirectory: boolean): boolean;
  getPatterns(): Array<{
    raw: string;
    pattern: string;
    negated: boolean;
    directoryOnly: boolean;
  }>;
}

export interface NativeModule {
  GitignoreMatcher: new (content: string) => NativeGitignoreMatcher;
  readJpegExifOrientation(buffer: Buffer): number | null;
  crc32(buffer: Buffer): number;
  encodePngRgba(buffer: Buffer, width: number, height: number): Buffer;
}

/** All expected exports from the unified addon (native + core-rs combined). */
const ALL_EXPECTED_EXPORTS: Array<[string, string]> = [
  // native functions
  ["GitignoreMatcher", "function"],
  ["readJpegExifOrientation", "function"],
  ["crc32", "function"],
  ["encodePngRgba", "function"],
  // core-rs functions
  ["validateFrame", "function"],
  ["constantTimeEq", "function"],
  ["detectMime", "function"],
  // core-rs security/validation functions (Phase 2)
  ["validateSessionKey", "function"],
  ["sanitizeHtml", "function"],
  ["isSafeUrl", "function"],
  ["validateErrorCode", "function"],
  ["isRetryableErrorCode", "function"],
  // core-rs protocol schema validation (Phase 3)
  ["validateParams", "function"],
  // core-rs memory search functions
  ["memoryCosineSimilarity", "function"],
  ["memoryBm25RankToScore", "function"],
  ["memoryBuildFtsQuery", "function"],
  ["memoryTemporalDecayMultiplier", "function"],
  ["memoryApplyTemporalDecay", "function"],
  ["memoryParseMemoryDateFromPath", "function"],
  ["memoryIsEvergreenMemoryPath", "function"],
  ["memoryMmrRerank", "function"],
  ["memoryExtractKeywords", "function"],
  ["memoryIsQueryStopWord", "function"],
  ["memoryExpandQueryForFts", "function"],
  ["memoryMergeHybridResults", "function"],
  ["memoryTextSimilarity", "function"],
];

/** PNG magic bytes for the load-time smoke test. */
const PNG_MAGIC = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);

let rawAddon: Record<string, unknown> | null = null;
let rawLoaded = false;

/**
 * Load the raw @deneb/native addon once, with shape validation and smoke tests.
 * Shared by both loadNative() and loadCoreRs() to avoid duplicate require() calls.
 * Throws if the addon is unavailable or fails validation.
 * @internal Exported for core-rs.ts only — not part of the public API.
 */
export function loadRawAddon(): Record<string, unknown> {
  if (rawLoaded) {
    if (!rawAddon) {
      throw new Error("@deneb/native addon failed to load (cached failure)");
    }
    return rawAddon;
  }
  rawLoaded = true;
  const require = createRequire(import.meta.url);
  const mod = require("@deneb/native") as Record<string, unknown>;
  // Runtime shape validation: ensure all expected functions are present.
  for (const [name, kind] of ALL_EXPECTED_EXPORTS) {
    if (typeof mod[name] !== kind) {
      throw new Error(`@deneb/native addon missing export: ${name} (expected ${kind})`);
    }
  }
  // Smoke tests: verify known outputs to catch ABI mismatches from stale builds.
  const candidate = mod as unknown as NativeModule & { detectMime(data: Buffer): string };
  if (candidate.crc32(Buffer.from("IEND")) !== 0xae42_6082) {
    throw new Error("@deneb/native addon ABI mismatch: crc32 smoke test failed");
  }
  if (candidate.detectMime(PNG_MAGIC) !== "image/png") {
    throw new Error("@deneb/native addon ABI mismatch: detectMime smoke test failed");
  }
  rawAddon = mod;
  return rawAddon;
}

/**
 * Load native addon functions (gitignore, EXIF, PNG).
 * Throws if the addon is unavailable.
 */
export function loadNative(): NativeModule {
  return loadRawAddon() as unknown as NativeModule;
}
