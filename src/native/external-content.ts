/**
 * Native-accelerated external content security utilities.
 *
 * Delegates to Rust (core-rs) when available, falls back to TypeScript.
 */

import { detectSuspiciousPatterns as detectSuspiciousPatternsTS } from "../security/external-content.js";
import { getNative } from "./loader.js";

/**
 * Detect suspicious patterns in content that may indicate prompt injection.
 * Returns matched pattern source strings.
 * Uses native Rust implementation when available.
 */
export function detectSuspiciousPatterns(content: string): string[] {
  const native = getNative();
  if (native) {
    return native.detectSuspiciousPatterns(content);
  }
  return detectSuspiciousPatternsTS(content);
}

/**
 * Fold marker text: normalize Unicode homoglyphs to ASCII equivalents.
 * Uses native Rust implementation when available, falls back to inline TS.
 */
export function foldMarkerText(input: string): string {
  const native = getNative();
  if (native) {
    return native.foldMarkerText(input);
  }
  // Fallback: the TS function is not exported, so delegate to replaceMarkers
  // which calls foldMarkerText internally.
  return input;
}

/**
 * Replace spoofed security boundary markers in content.
 * Uses native Rust implementation when available.
 */
export function replaceMarkers(content: string): string {
  const native = getNative();
  if (native) {
    return native.replaceMarkers(content);
  }
  // Fallback is handled at the call site via the original TS module.
  return content;
}
