import crypto from "node:crypto";
import { loadCoreRs } from "../bindings/core-rs.js";

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
  // Fast path: use native Rust constant-time comparison.
  const native = loadCoreRs();
  if (native) {
    try {
      return native.constantTimeEq(Buffer.from(provided), Buffer.from(expected));
    } catch {
      // Fall through to Node.js crypto implementation.
    }
  }
  return crypto.timingSafeEqual(Buffer.from(provided), Buffer.from(expected));
}
