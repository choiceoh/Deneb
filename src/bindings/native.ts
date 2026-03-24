/**
 * Lazy loader for the optional @deneb/native Rust addon.
 * Falls back gracefully when the addon is not available.
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

let native: NativeModule | null = null;
let loaded = false;

/**
 * Attempt to load the native addon. Returns null if unavailable.
 * Result is cached after first call.
 */
export function loadNative(): NativeModule | null {
  if (loaded) {
    return native;
  }
  loaded = true;
  try {
    const require = createRequire(import.meta.url);
    native = require("@deneb/native") as NativeModule;
  } catch {
    native = null;
  }
  return native;
}
