import crypto from "node:crypto";
import { loadCoreRs } from "../bindings/core-rs.js";

// Circuit breaker: permanently disable native path for this security-critical
// function if it ever throws, to prevent timing information leaking from
// the time spent in a failed native call.
let nativeDisabled = false;

export function safeEqualSecret(
  provided: string | undefined | null,
  expected: string | undefined | null,
): boolean {
  if (typeof provided !== "string" || typeof expected !== "string") {
    return false;
  }
  if (provided.length !== expected.length) {
    return false;
  }
  const a = Buffer.from(provided);
  const b = Buffer.from(expected);
  // Fast path: use native Rust constant-time comparison.
  if (!nativeDisabled) {
    const native = loadCoreRs();
    if (native) {
      try {
        return native.constantTimeEq(a, b);
      } catch {
        // Permanently disable native for this function to avoid timing leaks.
        nativeDisabled = true;
      }
    }
  }
  return crypto.timingSafeEqual(a, b);
}
