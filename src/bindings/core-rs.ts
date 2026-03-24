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
  };
}

let coreRs: CoreRsModule | null = null;
let loaded = false;

/**
 * Load core-rs functions from the unified native addon.
 * Returns null if the addon is unavailable. Result is cached.
 * Shape validation and smoke tests are handled by the shared loadRawAddon().
 */
export function loadCoreRs(): CoreRsModule | null {
  if (loaded) {
    return coreRs;
  }
  loaded = true;
  const raw = loadRawAddon();
  if (!raw) {
    coreRs = null;
    return coreRs;
  }
  coreRs = wrapModule(raw as unknown as CoreRsModuleRaw);
  return coreRs;
}
