import { registerLcmContextEngine } from "./lcm/index.js";
import { registerLegacyContextEngine } from "./legacy.js";

/**
 * Ensures all built-in context engines are registered exactly once.
 *
 * The legacy engine is always registered as a safe fallback so that
 * `resolveContextEngine()` can resolve the default "legacy" slot without
 * callers needing to remember manual registration.
 *
 * The LCM (Lossless Context Management) engine is registered as a native
 * core engine, replacing the former lossless-claw plugin.
 */
let initialized = false;

export function ensureContextEnginesInitialized(): void {
  if (initialized) {
    return;
  }

  // Always available – safe fallback for the "legacy" slot default.
  registerLegacyContextEngine();

  // Native LCM engine (DAG-based summarization, FTS, sub-agent expansion).
  // registerLcmContextEngine handles its own errors — won't throw here.
  registerLcmContextEngine();

  initialized = true;
}
