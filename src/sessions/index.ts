// Public API barrel for src/sessions/

// Session key utilities
export {
  deriveSessionChatType,
  getSubagentDepth,
  isAcpSessionKey,
  isCronRunSessionKey,
  isCronSessionKey,
  isSubagentSessionKey,
  parseAgentSessionKey,
  resolveThreadParentSessionKey,
} from "./session-key-utils.js";
export type { ParsedAgentSessionKey, SessionKeyChatType } from "./session-key-utils.js";

// Input provenance
export {
  INPUT_PROVENANCE_KIND_VALUES,
  applyInputProvenanceToUserMessage,
  hasInterSessionUserProvenance,
  isInterSessionInputProvenance,
  normalizeInputProvenance,
} from "./input-provenance.js";
export type { InputProvenance, InputProvenanceKind } from "./input-provenance.js";

// Transcript events
export { emitSessionTranscriptUpdate, onSessionTranscriptUpdate } from "./transcript-events.js";
export type { SessionTranscriptUpdate } from "./transcript-events.js";

// Model overrides
export { applyModelOverrideToSessionEntry } from "./model-overrides.js";
export type { ModelOverrideSelection } from "./model-overrides.js";

// Send policy
export { normalizeSendPolicy, resolveSendPolicy } from "./send-policy.js";
export type { SessionSendPolicyDecision } from "./send-policy.js";

// Session label
export { SESSION_LABEL_MAX_LENGTH, parseSessionLabel } from "./session-label.js";
export type { ParsedSessionLabel } from "./session-label.js";

// Level overrides
export { applyVerboseOverride, parseVerboseOverride } from "./level-overrides.js";

// Session ID
export { SESSION_ID_RE, looksLikeSessionId } from "./session-id.js";

// Session ID resolution
export { resolvePreferredSessionKeyForSessionIdMatches } from "./session-id-resolution.js";

// Session lifecycle events
export { emitSessionLifecycleEvent, onSessionLifecycleEvent } from "./session-lifecycle-events.js";
export type { SessionLifecycleEvent } from "./session-lifecycle-events.js";
