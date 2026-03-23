// Public API barrel for src/routing/

// Session key utilities
export {
  DEFAULT_AGENT_ID,
  DEFAULT_MAIN_KEY,
  buildAgentMainSessionKey,
  buildAgentPeerSessionKey,
  buildGroupHistoryKey,
  classifySessionKeyShape,
  isValidAgentId,
  normalizeAgentId,
  normalizeMainKey,
  resolveAgentIdFromSessionKey,
  resolveThreadSessionKeys,
  sanitizeAgentId,
  scopedHeartbeatWakeOptions,
  toAgentRequestSessionKey,
  toAgentStoreSessionKey,
} from "./session-key.js";
export type { SessionKeyShape } from "./session-key.js";

// Account ID
export {
  DEFAULT_ACCOUNT_ID,
  normalizeAccountId,
  normalizeOptionalAccountId,
} from "./account-id.js";

// Account lookup
export { resolveAccountEntry } from "./account-lookup.js";

// Route resolution
export {
  buildAgentSessionKey,
  deriveLastRoutePolicy,
  pickFirstExistingAgentId,
  resolveAgentRoute,
  resolveInboundLastRouteSessionKey,
} from "./resolve-route.js";
export type {
  ResolveAgentRouteInput,
  ResolvedAgentRoute,
  RoutePeer,
  RoutePeerKind,
} from "./resolve-route.js";

// Bindings
export {
  buildChannelAccountBindings,
  listBindings,
  listBoundAccountIds,
  resolveDefaultAgentBoundAccountId,
  resolvePreferredAccountId,
} from "./bindings.js";

// Default account warnings
export {
  formatChannelAccountsDefaultPath,
  formatChannelDefaultAccountPath,
  formatSetExplicitDefaultInstruction,
  formatSetExplicitDefaultToConfiguredInstruction,
} from "./default-account-warnings.js";

// Re-export session-key-utils re-exports
export {
  getSubagentDepth,
  isAcpSessionKey,
  isCronSessionKey,
  isSubagentSessionKey,
  parseAgentSessionKey,
} from "./session-key.js";
export type { ParsedAgentSessionKey } from "./session-key.js";
