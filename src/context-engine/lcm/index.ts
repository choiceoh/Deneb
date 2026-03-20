/**
 * LCM (Lossless Context Management) module.
 *
 * Native context engine implementation providing DAG-based conversation
 * summarization with incremental compaction, full-text search, and
 * sub-agent expansion.
 *
 * This replaces the @martian-engineering/lossless-claw plugin.
 */
export { registerLcmContextEngine, createLcmToolFactories } from "./register.js";
export { createNativeLcmDependencies } from "./native-bridge.js";
