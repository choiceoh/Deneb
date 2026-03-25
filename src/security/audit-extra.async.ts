/**
 * Asynchronous security audit collector functions.
 *
 * These functions perform I/O (filesystem, config reads) to detect security issues.
 * Implementation split by domain:
 * - audit-extra.async.plugins.ts: Plugin trust, install integrity, code safety
 * - audit-extra.async.skills.ts: Workspace skill symlink escape and code safety
 * - audit-extra.async.filesystem.ts: Config include and state directory permission checks
 * - audit-extra.async.helpers.ts: Shared helpers (plugin dirs, code scanning, etc.)
 */
export type { SecurityAuditFinding } from "./audit-extra-shared.js";

export {
  collectPluginsTrustFindings,
  collectPluginsCodeSafetyFindings,
} from "./audit-extra.async.plugins.js";

export {
  collectWorkspaceSkillSymlinkEscapeFindings,
  collectInstalledSkillsCodeSafetyFindings,
} from "./audit-extra.async.skills.js";

export {
  collectIncludeFilePermFindings,
  collectStateDeepFilesystemFindings,
  readConfigSnapshotForAudit,
} from "./audit-extra.async.filesystem.js";
