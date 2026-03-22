/**
 * LCM (Lossless Context Management) module.
 *
 * Native context engine implementation providing DAG-based conversation
 * summarization with incremental compaction, full-text search, and
 * sub-agent expansion.
 */
export {
  registerLcmContextEngine,
  createLcmToolFactories,
  getOrCreateLcmSingleton,
} from "./register.js";
export { createNativeLcmDependencies } from "./native-bridge.js";
