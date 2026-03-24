/**
 * Lazy loader for the optional @deneb/native Rust addon.
 * Falls back gracefully when the addon is not available or has an ABI mismatch.
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

/** Expected exports and their types for runtime shape validation. */
const EXPECTED_EXPORTS: Array<[string, string]> = [
  ["GitignoreMatcher", "function"],
  ["readJpegExifOrientation", "function"],
  ["crc32", "function"],
  ["encodePngRgba", "function"],
];

let native: NativeModule | null = null;
let loaded = false;

/**
 * Attempt to load the native addon. Returns null if unavailable.
 * Validates that the module exposes all expected functions and produces
 * correct output (smoke test) to guard against ABI mismatches.
 * Result is cached after first call.
 */
export function loadNative(): NativeModule | null {
  if (loaded) {
    return native;
  }
  loaded = true;
  try {
    const require = createRequire(import.meta.url);
    const mod = require("@deneb/native") as Record<string, unknown>;
    // Runtime shape validation.
    for (const [name, kind] of EXPECTED_EXPORTS) {
      if (typeof mod[name] !== kind) {
        native = null;
        return native;
      }
    }
    const candidate = mod as unknown as NativeModule;
    // Smoke test: crc32 of "IEND" is a known constant (PNG spec).
    if (candidate.crc32(Buffer.from("IEND")) !== 0xae42_6082) {
      native = null;
      return native;
    }
    native = candidate;
  } catch {
    native = null;
  }
  return native;
}
