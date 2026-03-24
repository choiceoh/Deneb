/**
 * Async security audit: filesystem permission checks for config includes, credentials, and logs.
 */
import path from "node:path";
import { resolveDefaultAgentId } from "../agents/agent-scope.js";
import type { DenebConfig, ConfigFileSnapshot } from "../config/config.js";
import { collectIncludePathsRecursive } from "../config/includes-scan.js";
import { resolveOAuthDir } from "../config/paths.js";
import { normalizeAgentId } from "../routing/session-key.js";
import type { SecurityAuditFinding } from "./audit-extra-shared.js";
import { expandTilde, loadConfigModule } from "./audit-extra.async.helpers.js";
import {
  formatPermissionDetail,
  formatPermissionRemediation,
  inspectPathPermissions,
} from "./audit-fs.js";

export async function collectIncludeFilePermFindings(params: {
  configSnapshot: ConfigFileSnapshot;
  env?: NodeJS.ProcessEnv;
  platform?: NodeJS.Platform;
}): Promise<SecurityAuditFinding[]> {
  const findings: SecurityAuditFinding[] = [];
  if (!params.configSnapshot.exists) {
    return findings;
  }

  const configPath = params.configSnapshot.path;
  const includePaths = await collectIncludePathsRecursive({
    configPath,
    parsed: params.configSnapshot.parsed,
  });
  if (includePaths.length === 0) {
    return findings;
  }

  for (const p of includePaths) {
    const perms = await inspectPathPermissions(p);
    if (!perms.ok) {
      continue;
    }
    if (perms.worldWritable || perms.groupWritable) {
      findings.push({
        checkId: "fs.config_include.perms_writable",
        severity: "critical",
        title: "Config include file is writable by others",
        detail: `${formatPermissionDetail(p, perms)}; another user could influence your effective config.`,
        remediation: formatPermissionRemediation({
          targetPath: p,
          isDir: false,
          posixMode: 0o600,
        }),
      });
    } else if (perms.worldReadable) {
      findings.push({
        checkId: "fs.config_include.perms_world_readable",
        severity: "critical",
        title: "Config include file is world-readable",
        detail: `${formatPermissionDetail(p, perms)}; include files can contain tokens and private settings.`,
        remediation: formatPermissionRemediation({
          targetPath: p,
          isDir: false,
          posixMode: 0o600,
        }),
      });
    } else if (perms.groupReadable) {
      findings.push({
        checkId: "fs.config_include.perms_group_readable",
        severity: "warn",
        title: "Config include file is group-readable",
        detail: `${formatPermissionDetail(p, perms)}; include files can contain tokens and private settings.`,
        remediation: formatPermissionRemediation({
          targetPath: p,
          isDir: false,
          posixMode: 0o600,
        }),
      });
    }
  }

  return findings;
}

export async function collectStateDeepFilesystemFindings(params: {
  cfg: DenebConfig;
  env: NodeJS.ProcessEnv;
  stateDir: string;
}): Promise<SecurityAuditFinding[]> {
  const findings: SecurityAuditFinding[] = [];
  const oauthDir = resolveOAuthDir(params.env, params.stateDir);

  const oauthPerms = await inspectPathPermissions(oauthDir);
  if (oauthPerms.ok && oauthPerms.isDir) {
    if (oauthPerms.worldWritable || oauthPerms.groupWritable) {
      findings.push({
        checkId: "fs.credentials_dir.perms_writable",
        severity: "critical",
        title: "Credentials dir is writable by others",
        detail: `${formatPermissionDetail(oauthDir, oauthPerms)}; another user could drop/modify credential files.`,
        remediation: formatPermissionRemediation({
          targetPath: oauthDir,
          isDir: true,
          posixMode: 0o700,
        }),
      });
    } else if (oauthPerms.groupReadable || oauthPerms.worldReadable) {
      findings.push({
        checkId: "fs.credentials_dir.perms_readable",
        severity: "warn",
        title: "Credentials dir is readable by others",
        detail: `${formatPermissionDetail(oauthDir, oauthPerms)}; credentials and allowlists can be sensitive.`,
        remediation: formatPermissionRemediation({
          targetPath: oauthDir,
          isDir: true,
          posixMode: 0o700,
        }),
      });
    }
  }

  const agentIds = Array.isArray(params.cfg.agents?.list)
    ? params.cfg.agents?.list
        .map((a) => (a && typeof a === "object" && typeof a.id === "string" ? a.id.trim() : ""))
        .filter(Boolean)
    : [];
  const defaultAgentId = resolveDefaultAgentId(params.cfg);
  const ids = Array.from(new Set([defaultAgentId, ...agentIds])).map((id) => normalizeAgentId(id));

  for (const agentId of ids) {
    const agentDir = path.join(params.stateDir, "agents", agentId, "agent");
    const authPath = path.join(agentDir, "auth-profiles.json");

    const authPerms = await inspectPathPermissions(authPath);
    if (authPerms.ok) {
      if (authPerms.worldWritable || authPerms.groupWritable) {
        findings.push({
          checkId: "fs.auth_profiles.perms_writable",
          severity: "critical",
          title: "auth-profiles.json is writable by others",
          detail: `${formatPermissionDetail(authPath, authPerms)}; another user could inject credentials.`,
          remediation: formatPermissionRemediation({
            targetPath: authPath,
            isDir: false,
            posixMode: 0o600,
          }),
        });
      } else if (authPerms.worldReadable || authPerms.groupReadable) {
        findings.push({
          checkId: "fs.auth_profiles.perms_readable",
          severity: "warn",
          title: "auth-profiles.json is readable by others",
          detail: `${formatPermissionDetail(authPath, authPerms)}; auth-profiles.json contains API keys and OAuth tokens.`,
          remediation: formatPermissionRemediation({
            targetPath: authPath,
            isDir: false,
            posixMode: 0o600,
          }),
        });
      }
    }

    const storePath = path.join(params.stateDir, "agents", agentId, "sessions", "sessions.json");

    const storePerms = await inspectPathPermissions(storePath);
    if (storePerms.ok) {
      if (storePerms.worldReadable || storePerms.groupReadable) {
        findings.push({
          checkId: "fs.sessions_store.perms_readable",
          severity: "warn",
          title: "sessions.json is readable by others",
          detail: `${formatPermissionDetail(storePath, storePerms)}; routing and transcript metadata can be sensitive.`,
          remediation: formatPermissionRemediation({
            targetPath: storePath,
            isDir: false,
            posixMode: 0o600,
          }),
        });
      }
    }
  }

  const logFile =
    typeof params.cfg.logging?.file === "string" ? params.cfg.logging.file.trim() : "";
  if (logFile) {
    const expanded = logFile.startsWith("~") ? expandTilde(logFile, params.env) : logFile;
    if (expanded) {
      const logPath = path.resolve(expanded);
      const logPerms = await inspectPathPermissions(logPath);
      if (logPerms.ok) {
        if (logPerms.worldReadable || logPerms.groupReadable) {
          findings.push({
            checkId: "fs.log_file.perms_readable",
            severity: "warn",
            title: "Log file is readable by others",
            detail: `${formatPermissionDetail(logPath, logPerms)}; logs can contain private messages and tool output.`,
            remediation: formatPermissionRemediation({
              targetPath: logPath,
              isDir: false,
              posixMode: 0o600,
            }),
          });
        }
      }
    }
  }

  return findings;
}

export async function readConfigSnapshotForAudit(params: {
  env: NodeJS.ProcessEnv;
  configPath: string;
}): Promise<ConfigFileSnapshot> {
  const { createConfigIO } = await loadConfigModule();
  return await createConfigIO({
    env: params.env,
    configPath: params.configPath,
  }).readConfigFileSnapshot();
}
