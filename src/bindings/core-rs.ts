/**
 * Lazy loader for the optional @deneb/core-rs Rust addon.
 * Exposes protocol validation, security primitives, and media detection.
 * Falls back gracefully when the addon is not available or has an ABI mismatch.
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
  /** Remove control characters (keeps newline/tab/CR). Throws if input exceeds size limit. */
  sanitizeControlChars(input: string): string;
}

/** Expected function exports and their types, used for runtime shape validation. */
const EXPECTED_EXPORTS: Array<[string, string]> = [
  ["validateFrame", "function"],
  ["constantTimeEq", "function"],
  ["detectMime", "function"],
  ["isSafeInput", "function"],
  ["sanitizeControlChars", "function"],
];

/** PNG magic bytes for the load-time smoke test. */
const PNG_MAGIC = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);

let coreRs: CoreRsModule | null = null;
let loaded = false;

/**
 * Attempt to load the core-rs native addon. Returns null if unavailable.
 * Validates that the loaded module exposes all expected functions and produces
 * correct output (smoke test) to guard against ABI mismatches from stale builds.
 * Result is cached after first call.
 */
export function loadCoreRs(): CoreRsModule | null {
  if (loaded) {
    return coreRs;
  }
  loaded = true;
  try {
    const require = createRequire(import.meta.url);
    const mod = require("@deneb/core-rs") as Record<string, unknown>;
    // Runtime shape validation: ensure all expected functions are present.
    for (const [name, kind] of EXPECTED_EXPORTS) {
      if (typeof mod[name] !== kind) {
        coreRs = null;
        return coreRs;
      }
    }
    const candidate = mod as unknown as CoreRsModule;
    // Smoke test: verify detectMime returns correct result for a known input.
    // Catches stale/corrupt .node binaries that pass shape checks but return garbage.
    if (candidate.detectMime(PNG_MAGIC) !== "image/png") {
      coreRs = null;
      return coreRs;
    }
    coreRs = candidate;
  } catch {
    coreRs = null;
  }
  return coreRs;
}
