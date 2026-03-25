import { registerAuroraContextEngine } from "./aurora/index.js";

/**
 * Ensures all built-in context engines are registered exactly once.
 *
 * The Aurora (Lossless Context Management) engine is registered as a native
 * core engine, replacing the former lossless-claw plugin.
 */
let initialized = false;

export function ensureContextEnginesInitialized(): void {
  if (initialized) {
    return;
  }

  // Native Aurora engine (DAG-based summarization, FTS, sub-agent expansion).
  // registerAuroraContextEngine handles its own errors — won't throw here.
  registerAuroraContextEngine();

  initialized = true;
}
