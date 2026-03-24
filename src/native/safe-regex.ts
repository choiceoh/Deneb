/**
 * Native-accelerated safe regex utilities.
 *
 * Delegates to Rust (core-rs) when available, falls back to TypeScript.
 */

import { hasNestedRepetition as hasNestedRepetitionTS } from "../security/safe-regex.js";
import { getNative } from "./loader.js";

/**
 * Check whether a regex source pattern contains nested repetition (ReDoS risk).
 * Uses native Rust implementation when available for better performance.
 */
export function hasNestedRepetition(source: string): boolean {
  const native = getNative();
  if (native) {
    return native.hasNestedRepetition(source);
  }
  return hasNestedRepetitionTS(source);
}
