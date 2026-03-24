/**
 * Native-accelerated MIME utilities.
 *
 * Delegates to Rust (core-rs) when available, falls back to TypeScript.
 */

import { normalizeMimeType as normalizeMimeTypeTS } from "../media/mime.js";
import { getNative } from "./loader.js";

/**
 * Normalize a MIME type: extract base type, trim, lowercase.
 * Returns undefined for empty input.
 * Uses native Rust implementation when available.
 */
export function normalizeMimeType(mime?: string | null): string | undefined {
  if (!mime) {
    return undefined;
  }
  const native = getNative();
  if (native) {
    return native.normalizeMimeType(mime) ?? undefined;
  }
  return normalizeMimeTypeTS(mime);
}

/**
 * Check if a MIME type is a generic container (octet-stream or zip).
 * Uses native Rust implementation when available.
 */
export function isGenericMime(mime?: string): boolean {
  if (!mime) {
    return true;
  }
  const native = getNative();
  if (native) {
    return native.isGenericMime(mime);
  }
  const m = mime.toLowerCase();
  return m === "application/octet-stream" || m === "application/zip";
}
