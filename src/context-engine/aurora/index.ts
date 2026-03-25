/**
 * Aurora context engine module.
 *
 * Native context engine implementation providing DAG-based conversation
 * summarization with incremental compaction, full-text search, and
 * sub-agent expansion.
 */
export {
  registerAuroraContextEngine,
  createAuroraToolFactories,
  getOrCreateAuroraSingleton,
} from "./register.js";
export { createNativeAuroraDependencies } from "./native-bridge.js";
