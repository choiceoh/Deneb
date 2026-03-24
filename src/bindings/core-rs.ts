/**
 * Lazy loader for the optional @deneb/core-rs Rust addon.
 * Exposes protocol validation, security primitives, and media detection.
 * Falls back gracefully when the addon is not available or has an ABI mismatch.
 */

import { createRequire } from "node:module";

/** Frame type IDs returned by native validateFrame (matches Rust enum order). */
const FRAME_TYPES = ["req", "res", "event"] as const;

/** Raw native module interface (internal — use CoreRsModule wrapper). */
interface CoreRsModuleRaw {
  validateFrame(json: string): number;
  constantTimeEq(a: Buffer, b: Buffer): boolean;
  detectMime(data: Buffer): string;
  isSafeInput(input: string): boolean;
  sanitizeControlChars(input: string): string;
}

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
    constantTimeEq: raw.constantTimeEq.bind(raw),
    detectMime: raw.detectMime.bind(raw),
    isSafeInput: raw.isSafeInput.bind(raw),
    sanitizeControlChars: raw.sanitizeControlChars.bind(raw),
  };
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
    const mod = require("@deneb/native") as Record<string, unknown>;
    // Runtime shape validation: ensure all expected functions are present.
    for (const [name, kind] of EXPECTED_EXPORTS) {
      if (typeof mod[name] !== kind) {
        coreRs = null;
        return coreRs;
      }
    }
    const raw = mod as unknown as CoreRsModuleRaw;
    // Smoke test: verify detectMime returns correct result for a known input.
    if (raw.detectMime(PNG_MAGIC) !== "image/png") {
      coreRs = null;
      return coreRs;
    }
    // Wrap raw module to map numeric frame type IDs to strings.
    coreRs = wrapModule(raw);
  } catch {
    coreRs = null;
  }
  return coreRs;
}
