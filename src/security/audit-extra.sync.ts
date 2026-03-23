/**
 * Synchronous security audit collector functions.
 *
 * These functions analyze config-based security properties without I/O.
 *
 * Implementation is split into focused sub-modules:
 * - audit-extra.sync.config.ts   — attack surface summary, synced folder, secrets in config
 * - audit-extra.sync.gateway.ts  — hooks hardening, HTTP session-key override, HTTP no-auth
 * - audit-extra.sync.sandbox.ts  — docker noop, dangerous docker/network/seccomp/apparmor
 * - audit-extra.sync.nodes.ts    — node deny-command patterns, dangerous allow-commands
 * - audit-extra.sync.models.ts   — minimal profile override, legacy/weak models, small model risk
 * - audit-extra.sync.exposure.ts — open group exposure matrix, multi-user heuristic
 * - audit-extra.sync.helpers.ts  — private helpers shared across the above modules
 */

export type { SecurityAuditFinding } from "./audit-extra-shared.js";

export {
  collectAttackSurfaceSummaryFindings,
  collectSecretsInConfigFindings,
  collectSyncedFolderFindings,
} from "./audit-extra.sync.config.js";

export {
  collectGatewayHttpNoAuthFindings,
  collectGatewayHttpSessionKeyOverrideFindings,
  collectHooksHardeningFindings,
} from "./audit-extra.sync.gateway.js";

export {
  collectSandboxDangerousConfigFindings,
  collectSandboxDockerNoopFindings,
} from "./audit-extra.sync.sandbox.js";

export {
  collectNodeDangerousAllowCommandFindings,
  collectNodeDenyCommandPatternFindings,
} from "./audit-extra.sync.nodes.js";

export {
  collectMinimalProfileOverrideFindings,
  collectModelHygieneFindings,
  collectSmallModelRiskFindings,
} from "./audit-extra.sync.models.js";

export {
  collectExposureMatrixFindings,
  collectLikelyMultiUserSetupFindings,
} from "./audit-extra.sync.exposure.js";
