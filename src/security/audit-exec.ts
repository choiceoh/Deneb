/**
 * Exec-runtime and elevated-access security audit findings.
 *
 * Covers sandbox-mode mismatches, safeBins interpreter risks, risky
 * safeBinTrustedDirs, elevated-exec allowlists, and logging redaction.
 */
import path from "node:path";
import { resolveSandboxConfigForAgent } from "../agents/sandbox.js";
import type { DenebConfig } from "../config/config.js";
import {
  listInterpreterLikeSafeBins,
  resolveMergedSafeBinProfileFixtures,
} from "../infra/exec-safe-bin-runtime-policy.js";
import { normalizeTrustedSafeBinDirs } from "../infra/exec-safe-bin-trust.js";
import { normalizeAllowFromList } from "./audit.helpers.js";
import type { SecurityAuditFinding } from "./audit.types.js";

export function collectLoggingFindings(cfg: DenebConfig): SecurityAuditFinding[] {
  const redact = cfg.logging?.redactSensitive;
  if (redact !== "off") {
    return [];
  }
  return [
    {
      checkId: "logging.redact_off",
      severity: "warn",
      title: "Tool summary redaction is disabled",
      detail: `logging.redactSensitive="off" can leak secrets into logs and status output.`,
      remediation: `Set logging.redactSensitive="tools".`,
    },
  ];
}

export function collectElevatedFindings(cfg: DenebConfig): SecurityAuditFinding[] {
  const findings: SecurityAuditFinding[] = [];
  const enabled = cfg.tools?.elevated?.enabled;
  const allowFrom = cfg.tools?.elevated?.allowFrom ?? {};
  const anyAllowFromKeys = Object.keys(allowFrom).length > 0;

  if (enabled === false) {
    return findings;
  }
  if (!anyAllowFromKeys) {
    return findings;
  }

  for (const [provider, list] of Object.entries(allowFrom)) {
    const normalized = normalizeAllowFromList(list);
    if (normalized.includes("*")) {
      findings.push({
        checkId: `tools.elevated.allowFrom.${provider}.wildcard`,
        severity: "critical",
        title: "Elevated exec allowlist contains wildcard",
        detail: `tools.elevated.allowFrom.${provider} includes "*" which effectively approves everyone on that channel for elevated mode.`,
      });
    } else if (normalized.length > 25) {
      findings.push({
        checkId: `tools.elevated.allowFrom.${provider}.large`,
        severity: "warn",
        title: "Elevated exec allowlist is large",
        detail: `tools.elevated.allowFrom.${provider} has ${normalized.length} entries; consider tightening elevated access.`,
      });
    }
  }

  return findings;
}

function normalizeConfiguredSafeBins(entries: unknown): string[] {
  if (!Array.isArray(entries)) {
    return [];
  }
  return Array.from(
    new Set(
      entries
        .map((entry) => (typeof entry === "string" ? entry.trim().toLowerCase() : ""))
        .filter((entry) => entry.length > 0),
    ),
  ).toSorted();
}

function normalizeConfiguredTrustedDirs(entries: unknown): string[] {
  if (!Array.isArray(entries)) {
    return [];
  }
  return normalizeTrustedSafeBinDirs(
    entries.filter((entry): entry is string => typeof entry === "string"),
  );
}

function classifyRiskySafeBinTrustedDir(entry: string): string | null {
  const raw = entry.trim();
  if (!raw) {
    return null;
  }
  if (!path.isAbsolute(raw)) {
    return "relative path (trust boundary depends on process cwd)";
  }
  const normalized = path.resolve(raw).replace(/\\/g, "/").toLowerCase();
  if (
    normalized === "/tmp" ||
    normalized.startsWith("/tmp/") ||
    normalized === "/var/tmp" ||
    normalized.startsWith("/var/tmp/") ||
    normalized === "/private/tmp" ||
    normalized.startsWith("/private/tmp/")
  ) {
    return "temporary directory is mutable and easy to poison";
  }
  if (
    normalized === "/usr/local/bin" ||
    normalized === "/opt/homebrew/bin" ||
    normalized === "/opt/local/bin" ||
    normalized === "/home/linuxbrew/.linuxbrew/bin"
  ) {
    return "package-manager bin directory (often user-writable)";
  }
  if (
    normalized.startsWith("/users/") ||
    normalized.startsWith("/home/") ||
    normalized.includes("/.local/bin")
  ) {
    return "home-scoped bin directory (typically user-writable)";
  }
  if (/^[a-z]:\/users\//.test(normalized)) {
    return "home-scoped bin directory (typically user-writable)";
  }
  return null;
}

export function collectExecRuntimeFindings(cfg: DenebConfig): SecurityAuditFinding[] {
  const findings: SecurityAuditFinding[] = [];
  const globalExecHost = cfg.tools?.exec?.host;
  const defaultSandboxMode = resolveSandboxConfigForAgent(cfg).mode;
  const defaultHostIsExplicitSandbox = globalExecHost === "sandbox";

  if (defaultHostIsExplicitSandbox && defaultSandboxMode === "off") {
    findings.push({
      checkId: "tools.exec.host_sandbox_no_sandbox_defaults",
      severity: "warn",
      title: "Exec host is sandbox but sandbox mode is off",
      detail:
        "tools.exec.host is explicitly set to sandbox while agents.defaults.sandbox.mode=off. " +
        "In this mode, exec runs directly on the gateway host.",
      remediation:
        'Enable sandbox mode (`agents.defaults.sandbox.mode="non-main"` or `"all"`) or set tools.exec.host to "gateway" with approvals.',
    });
  }

  const agents = Array.isArray(cfg.agents?.list) ? cfg.agents.list : [];
  const riskyAgents = agents
    .filter(
      (entry) =>
        entry &&
        typeof entry === "object" &&
        typeof entry.id === "string" &&
        entry.tools?.exec?.host === "sandbox" &&
        resolveSandboxConfigForAgent(cfg, entry.id).mode === "off",
    )
    .map((entry) => entry.id)
    .slice(0, 5);

  if (riskyAgents.length > 0) {
    findings.push({
      checkId: "tools.exec.host_sandbox_no_sandbox_agents",
      severity: "warn",
      title: "Agent exec host uses sandbox while sandbox mode is off",
      detail:
        `agents.list.*.tools.exec.host is set to sandbox for: ${riskyAgents.join(", ")}. ` +
        "With sandbox mode off, exec runs directly on the gateway host.",
      remediation:
        'Enable sandbox mode for these agents (`agents.list[].sandbox.mode`) or set their tools.exec.host to "gateway".',
    });
  }

  const globalExec = cfg.tools?.exec;
  const riskyTrustedDirHits: string[] = [];
  const collectRiskyTrustedDirHits = (scopePath: string, entries: unknown): void => {
    for (const entry of normalizeConfiguredTrustedDirs(entries)) {
      const reason = classifyRiskySafeBinTrustedDir(entry);
      if (!reason) {
        continue;
      }
      riskyTrustedDirHits.push(`- ${scopePath}.safeBinTrustedDirs: ${entry} (${reason})`);
    }
  };
  collectRiskyTrustedDirHits("tools.exec", globalExec?.safeBinTrustedDirs);
  for (const entry of agents) {
    if (!entry || typeof entry !== "object" || typeof entry.id !== "string") {
      continue;
    }
    collectRiskyTrustedDirHits(
      `agents.list.${entry.id}.tools.exec`,
      entry.tools?.exec?.safeBinTrustedDirs,
    );
  }

  const interpreterHits: string[] = [];
  const globalSafeBins = normalizeConfiguredSafeBins(globalExec?.safeBins);
  if (globalSafeBins.length > 0) {
    const merged = resolveMergedSafeBinProfileFixtures({ global: globalExec }) ?? {};
    const interpreters = listInterpreterLikeSafeBins(globalSafeBins).filter((bin) => !merged[bin]);
    if (interpreters.length > 0) {
      interpreterHits.push(`- tools.exec.safeBins: ${interpreters.join(", ")}`);
    }
  }

  for (const entry of agents) {
    if (!entry || typeof entry !== "object" || typeof entry.id !== "string") {
      continue;
    }
    const agentExec = entry.tools?.exec;
    const agentSafeBins = normalizeConfiguredSafeBins(agentExec?.safeBins);
    if (agentSafeBins.length === 0) {
      continue;
    }
    const merged =
      resolveMergedSafeBinProfileFixtures({
        global: globalExec,
        local: agentExec,
      }) ?? {};
    const interpreters = listInterpreterLikeSafeBins(agentSafeBins).filter((bin) => !merged[bin]);
    if (interpreters.length === 0) {
      continue;
    }
    interpreterHits.push(
      `- agents.list.${entry.id}.tools.exec.safeBins: ${interpreters.join(", ")}`,
    );
  }

  if (interpreterHits.length > 0) {
    findings.push({
      checkId: "tools.exec.safe_bins_interpreter_unprofiled",
      severity: "warn",
      title: "safeBins includes interpreter/runtime binaries without explicit profiles",
      detail:
        `Detected interpreter-like safeBins entries missing explicit profiles:\n${interpreterHits.join("\n")}\n` +
        "These entries can turn safeBins into a broad execution surface when used with permissive argv profiles.",
      remediation:
        "Remove interpreter/runtime bins from safeBins (prefer allowlist entries) or define hardened tools.exec.safeBinProfiles.<bin> rules.",
    });
  }

  if (riskyTrustedDirHits.length > 0) {
    findings.push({
      checkId: "tools.exec.safe_bin_trusted_dirs_risky",
      severity: "warn",
      title: "safeBinTrustedDirs includes risky mutable directories",
      detail:
        `Detected risky safeBinTrustedDirs entries:\n${riskyTrustedDirHits.slice(0, 10).join("\n")}` +
        (riskyTrustedDirHits.length > 10
          ? `\n- +${riskyTrustedDirHits.length - 10} more entries.`
          : ""),
      remediation:
        "Prefer root-owned immutable bins, keep default trust dirs (/bin, /usr/bin), and avoid trusting temporary/home/package-manager paths unless tightly controlled.",
    });
  }

  return findings;
}
