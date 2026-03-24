// Public API barrel for src/infra/

// Environment utilities
export { isTruthyEnvValue, logAcceptedEnvOption, normalizeEnv, normalizeZaiEnv } from "./env.js";

// Boundary file reading
export {
  canUseBoundaryFileOpen,
  openBoundaryFile,
  openBoundaryFileSync,
} from "./boundary-file-read.js";
export type {
  BoundaryFileOpenFailureReason,
  BoundaryFileOpenResult,
  OpenBoundaryFileParams,
  OpenBoundaryFileSyncParams,
} from "./boundary-file-read.js";

// Agent events
export {
  clearAgentRunContext,
  emitAgentEvent,
  getAgentRunContext,
  onAgentEvent,
  registerAgentRunContext,
  resetAgentRunContextForTest,
} from "./agent-events.js";
export type { AgentEventPayload, AgentEventStream, AgentRunContext } from "./agent-events.js";

// Heartbeat runner
export { startHeartbeatRunner } from "./heartbeat-runner.js";
export type { HeartbeatDeps, HeartbeatRunner } from "./heartbeat-runner.js";

// System events
export {
  drainSystemEventEntries,
  drainSystemEvents,
  enqueueSystemEvent,
  hasSystemEvents,
  isSystemEventContextChanged,
  peekSystemEventEntries,
  peekSystemEvents,
  resetSystemEventsForTest,
} from "./system-events.js";
export type { SystemEvent } from "./system-events.js";

// Device identity
export {
  deriveDeviceIdFromPublicKey,
  getDeviceId,
  loadOrCreateDeviceIdentity,
  normalizeDevicePublicKeyBase64Url,
  publicKeyRawBase64UrlFromPem,
  signDevicePayload,
  verifyDeviceSignature,
} from "./device-identity.js";
export type { DeviceIdentity } from "./device-identity.js";

// System presence
export { listSystemPresence, upsertPresence } from "./system-presence.js";
export type { SystemPresence, SystemPresenceUpdate } from "./system-presence.js";

// Tailscale
export { findTailscaleBinary, getTailnetHostname, getTailscaleBinary } from "./tailscale.js";
export type { TailscaleWhoisIdentity } from "./tailscale.js";

// Path environment
export { ensureDenebCliOnPath } from "./path-env.js";

// Home directory
export {
  expandHomePrefix,
  resolveEffectiveHomeDir,
  resolveHomeRelativePath,
  resolveRequiredHomeDir,
} from "./home-dir.js";

// JSON UTF-8 byte counting
export { jsonUtf8Bytes } from "./json-utf8-bytes.js";

// HTTP body parsing
export {
  DEFAULT_WEBHOOK_BODY_TIMEOUT_MS,
  DEFAULT_WEBHOOK_MAX_BODY_BYTES,
  RequestBodyLimitError,
  isRequestBodyLimitError,
  readRequestBodyWithLimit,
  requestBodyErrorToText,
} from "./http-body.js";
export type {
  ReadJsonBodyOptions,
  ReadJsonBodyResult,
  ReadRequestBodyOptions,
  RequestBodyLimitErrorCode,
} from "./http-body.js";

// WebSocket utilities
export { rawDataToString } from "./ws.js";

// Gateway lock
export { GatewayLockError, acquireGatewayLock } from "./gateway-lock.js";
export type { GatewayLockHandle, GatewayLockOptions } from "./gateway-lock.js";

// Heartbeat wake
export {
  areHeartbeatsEnabled,
  hasHeartbeatWakeHandler,
  hasPendingHeartbeatWake,
  requestHeartbeatNow,
  setHeartbeatWakeHandler,
  setHeartbeatsEnabled,
} from "./heartbeat-wake.js";
export type { HeartbeatRunResult, HeartbeatWakeHandler } from "./heartbeat-wake.js";

// Update check
export {
  getUpdateAvailable,
  resetUpdateAvailableStateForTest,
  runGatewayUpdateCheck,
  scheduleGatewayUpdateCheck,
} from "./update-startup.js";
export type { UpdateAvailable } from "./update-startup.js";
