import crypto from "node:crypto";
import { loadCoreRs, type CoreRsModule } from "../bindings/core-rs.js";

// Resolve once at module scope to avoid per-call overhead.
let _coreRs: CoreRsModule | null | undefined;
function coreRs(): CoreRsModule | null {
  if (_coreRs === undefined) {
    _coreRs = loadCoreRs();
  }
  return _coreRs;
}

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
  const native = coreRs();
  if (native) {
    try {
      return native.constantTimeEq(a, b);
    } catch {
      // Fall through to Node.js crypto implementation.
    }
  }
  return crypto.timingSafeEqual(a, b);
}
