// Public API barrel for src/agents/

// Agent scope
export {
  listAgentEntries,
  listAgentIds,
  resolveAgentConfig,
  resolveAgentEffectiveModelPrimary,
  resolveAgentExplicitModelPrimary,
  resolveAgentSkillsFilter,
  resolveDefaultAgentId,
  resolveSessionAgentId,
  resolveSessionAgentIds,
} from "./agent-scope.js";

// Defaults
export { DEFAULT_CONTEXT_TOKENS, DEFAULT_MODEL, DEFAULT_PROVIDER } from "./defaults.js";

// Model selection
export {
  inferUniqueProviderFromConfiguredModels,
  isCliProvider,
  legacyModelKey,
  modelKey,
  normalizeModelRef,
  parseModelRef,
} from "./model-selection.js";
export type { ModelAliasIndex, ModelRef } from "./model-selection.js";

// Model catalog
export {
  findModelInCatalog,
  loadModelCatalog,
  modelSupportsDocument,
  modelSupportsVision,
  resetModelCatalogCacheForTest,
} from "./model-catalog.js";
export type { ModelCatalogEntry, ModelInputType } from "./model-catalog.js";

// Subagent registry
export {
  countActiveDescendantRuns,
  countActiveRunsForSession,
  countPendingDescendantRuns,
  countPendingDescendantRunsExcludingRun,
  getSubagentRunByChildSessionKey,
  initSubagentRegistry,
  isSubagentSessionRunActive,
  listDescendantRunsForRequester,
  listSubagentRunsForController,
  listSubagentRunsForRequester,
  markSubagentRunForSteerRestart,
  markSubagentRunTerminated,
  registerSubagentRun,
  releaseSubagentRun,
  resolveRequesterForChildSession,
  shouldIgnorePostCompletionAnnounceForSession,
} from "./subagent/subagent-registry.js";

// Identity / avatar
export { resolveAgentAvatar } from "./identity-avatar.js";
export type { AgentAvatarResolution } from "./identity-avatar.js";

// PI embedded runner
export {
  abortEmbeddedPiRun,
  compactEmbeddedPiSession,
  isEmbeddedPiRunActive,
  isEmbeddedPiRunStreaming,
  queueEmbeddedPiMessage,
  resolveEmbeddedSessionLane,
  runEmbeddedPiAgent,
  waitForEmbeddedPiRunEnd,
} from "./pi-embedded.js";
export type {
  EmbeddedPiAgentMeta,
  EmbeddedPiCompactResult,
  EmbeddedPiRunMeta,
  EmbeddedPiRunResult,
} from "./pi-embedded.js";

// Embedded run state
export {
  getActiveEmbeddedRunCount,
  getActiveEmbeddedRunSnapshot,
  isEmbeddedPiRunActive as isEmbeddedPiRunActiveBySessionId,
  waitForActiveEmbeddedRuns,
} from "./pi-embedded-runner/runs.js";
export type { ActiveEmbeddedRunSnapshot } from "./pi-embedded-runner/runs.js";

// Bootstrap cache
export {
  clearAllBootstrapSnapshots,
  clearBootstrapSnapshot,
  clearBootstrapSnapshotOnSessionRollover,
  getOrLoadBootstrapFiles,
} from "./bootstrap-cache.js";

// Deneb tools
export { createDenebTools } from "./deneb-tools.js";

// Session directories
export {
  resolveAgentSessionDirs,
  resolveAgentSessionDirsFromAgentsDir,
  resolveAgentSessionDirsFromAgentsDirSync,
} from "./session-dirs.js";

// Usage
export {
  derivePromptTokens,
  deriveSessionTotalTokens,
  hasNonzeroUsage,
  makeZeroUsageSnapshot,
  normalizeUsage,
} from "./usage.js";
export type { AssistantUsageSnapshot, NormalizedUsage, UsageLike } from "./usage.js";
