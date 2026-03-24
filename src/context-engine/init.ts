import { registerLcmContextEngine } from "./lcm/index.js";

/**
 * Ensures all built-in context engines are registered exactly once.
 *
 * The LCM (Lossless Context Management) engine is registered as a native
 * core engine, replacing the former lossless-claw plugin.
 */
let initialized = false;

export function ensureContextEnginesInitialized(): void {
  if (initialized) {
    return;
  }

  // Native LCM engine (DAG-based summarization, FTS, sub-agent expansion).
  // registerLcmContextEngine handles its own errors — won't throw here.
  registerLcmContextEngine();

  initialized = true;
}
