/**
 * Lazy loader for the optional @deneb/core-rs Rust addon.
 * Exposes protocol validation, security primitives, and media detection.
 * Falls back gracefully when the addon is not available.
 */

import { createRequire } from "node:module";

export interface CoreRsModule {
  /** Validate a gateway protocol frame. Returns frame type ("req"/"res"/"event"). Throws on invalid. */
  validateFrame(json: string): string;
  /** Constant-time byte comparison (timing-attack safe). */
  constantTimeEq(a: Buffer, b: Buffer): boolean;
  /** Detect MIME type from file magic bytes. */
  detectMime(data: Buffer): string;
  /** Check if a string is free of injection patterns. */
  isSafeInput(input: string): boolean;
  /** Remove control characters (keeps newline/tab/CR). */
  sanitizeControlChars(input: string): string;
}

let coreRs: CoreRsModule | null = null;
let loaded = false;

/**
 * Attempt to load the core-rs native addon. Returns null if unavailable.
 * Result is cached after first call.
 */
export function loadCoreRs(): CoreRsModule | null {
  if (loaded) {
    return coreRs;
  }
  loaded = true;
  try {
    const require = createRequire(import.meta.url);
    coreRs = require("@deneb/core-rs") as CoreRsModule;
  } catch {
    coreRs = null;
  }
  return coreRs;
}
