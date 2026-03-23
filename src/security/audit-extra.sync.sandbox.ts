/**
 * Sandbox security audit collectors.
 *
 * Checks: docker noop config, dangerous docker/network/seccomp/apparmor settings.
 */
import { resolveSandboxConfigForAgent } from "../agents/sandbox/config.js";
import { isDangerousNetworkMode, normalizeNetworkMode } from "../agents/sandbox/network-mode.js";
import { getBlockedBindReason } from "../agents/sandbox/validate-sandbox-security.js";
import type { DenebConfig } from "../config/config.js";
import type { SecurityAuditFinding } from "./audit-extra-shared.js";
import { hasConfiguredDockerConfig } from "./audit-extra.sync.helpers.js";

export function collectSandboxDockerNoopFindings(cfg: DenebConfig): SecurityAuditFinding[] {
  const findings: SecurityAuditFinding[] = [];
  const configuredPaths: string[] = [];
  const agents = Array.isArray(cfg.agents?.list) ? cfg.agents.list : [];

  const defaultsSandbox = cfg.agents?.defaults?.sandbox;
  const hasDefaultDocker = hasConfiguredDockerConfig(
    defaultsSandbox?.docker as Record<string, unknown> | undefined,
  );
  const defaultMode = defaultsSandbox?.mode ?? "off";
  const hasAnySandboxEnabledAgent = agents.some((entry) => {
    if (!entry || typeof entry !== "object" || typeof entry.id !== "string") {
      return false;
    }
    return resolveSandboxConfigForAgent(cfg, entry.id).mode !== "off";
  });
  if (hasDefaultDocker && defaultMode === "off" && !hasAnySandboxEnabledAgent) {
    configuredPaths.push("agents.defaults.sandbox.docker");
  }

  for (const entry of agents) {
    if (!entry || typeof entry !== "object" || typeof entry.id !== "string") {
      continue;
    }
    if (!hasConfiguredDockerConfig(entry.sandbox?.docker as Record<string, unknown> | undefined)) {
      continue;
    }
    if (resolveSandboxConfigForAgent(cfg, entry.id).mode === "off") {
      configuredPaths.push(`agents.list.${entry.id}.sandbox.docker`);
    }
  }

  if (configuredPaths.length === 0) {
    return findings;
  }

  findings.push({
    checkId: "sandbox.docker_config_mode_off",
    severity: "warn",
    title: "Sandbox docker settings configured while sandbox mode is off",
    detail:
      "These docker settings will not take effect until sandbox mode is enabled:\n" +
      configuredPaths.map((entry) => `- ${entry}`).join("\n"),
    remediation:
      'Enable sandbox mode (`agents.defaults.sandbox.mode="non-main"` or `"all"`) where needed, or remove unused docker settings.',
  });

  return findings;
}

export function collectSandboxDangerousConfigFindings(cfg: DenebConfig): SecurityAuditFinding[] {
  const findings: SecurityAuditFinding[] = [];
  const agents = Array.isArray(cfg.agents?.list) ? cfg.agents.list : [];

  const configs: Array<{ source: string; docker: Record<string, unknown> }> = [];
  const defaultDocker = cfg.agents?.defaults?.sandbox?.docker;
  if (defaultDocker && typeof defaultDocker === "object") {
    configs.push({
      source: "agents.defaults.sandbox.docker",
      docker: defaultDocker as Record<string, unknown>,
    });
  }
  for (const entry of agents) {
    if (!entry || typeof entry !== "object" || typeof entry.id !== "string") {
      continue;
    }
    const agentDocker = entry.sandbox?.docker;
    if (agentDocker && typeof agentDocker === "object") {
      configs.push({
        source: `agents.list.${entry.id}.sandbox.docker`,
        docker: agentDocker as Record<string, unknown>,
      });
    }
  }

  for (const { source, docker } of configs) {
    const binds = Array.isArray(docker.binds) ? docker.binds : [];
    for (const bind of binds) {
      if (typeof bind !== "string") {
        continue;
      }
      const blocked = getBlockedBindReason(bind);
      if (!blocked) {
        continue;
      }
      if (blocked.kind === "non_absolute") {
        findings.push({
          checkId: "sandbox.bind_mount_non_absolute",
          severity: "warn",
          title: "Sandbox bind mount uses a non-absolute source path",
          detail:
            `${source}.binds contains "${bind}" which uses source path "${blocked.sourcePath}". ` +
            "Non-absolute bind sources are hard to validate safely and may resolve unexpectedly.",
          remediation: `Rewrite "${bind}" to use an absolute host path (for example: /home/user/project:/project:ro).`,
        });
        continue;
      }
      if (blocked.kind !== "covers" && blocked.kind !== "targets") {
        continue;
      }
      const verb = blocked.kind === "covers" ? "covers" : "targets";
      findings.push({
        checkId: "sandbox.dangerous_bind_mount",
        severity: "critical",
        title: "Dangerous bind mount in sandbox config",
        detail:
          `${source}.binds contains "${bind}" which ${verb} blocked path "${blocked.blockedPath}". ` +
          "This can expose host system directories or the Docker socket to sandbox containers.",
        remediation: `Remove "${bind}" from ${source}.binds. Use project-specific paths instead.`,
      });
    }

    const network = typeof docker.network === "string" ? docker.network : undefined;
    const normalizedNetwork = normalizeNetworkMode(network);
    if (isDangerousNetworkMode(network)) {
      const modeLabel = normalizedNetwork === "host" ? '"host"' : `"${network}"`;
      const detail =
        normalizedNetwork === "host"
          ? `${source}.network is "host" which bypasses container network isolation entirely.`
          : `${source}.network is ${modeLabel} which joins another container namespace and can bypass sandbox network isolation.`;
      findings.push({
        checkId: "sandbox.dangerous_network_mode",
        severity: "critical",
        title: "Dangerous network mode in sandbox config",
        detail,
        remediation:
          `Set ${source}.network to "bridge", "none", or a custom bridge network name.` +
          ` Use ${source}.dangerouslyAllowContainerNamespaceJoin=true only as a break-glass override when you fully trust this runtime.`,
      });
    }

    const seccompProfile =
      typeof docker.seccompProfile === "string" ? docker.seccompProfile : undefined;
    if (seccompProfile && seccompProfile.trim().toLowerCase() === "unconfined") {
      findings.push({
        checkId: "sandbox.dangerous_seccomp_profile",
        severity: "critical",
        title: "Seccomp unconfined in sandbox config",
        detail: `${source}.seccompProfile is "unconfined" which disables syscall filtering.`,
        remediation: `Remove ${source}.seccompProfile or use a custom seccomp profile file.`,
      });
    }

    const apparmorProfile =
      typeof docker.apparmorProfile === "string" ? docker.apparmorProfile : undefined;
    if (apparmorProfile && apparmorProfile.trim().toLowerCase() === "unconfined") {
      findings.push({
        checkId: "sandbox.dangerous_apparmor_profile",
        severity: "critical",
        title: "AppArmor unconfined in sandbox config",
        detail: `${source}.apparmorProfile is "unconfined" which disables AppArmor enforcement.`,
        remediation: `Remove ${source}.apparmorProfile or use a named AppArmor profile.`,
      });
    }
  }

  const browserExposurePaths: string[] = [];
  const defaultBrowser = resolveSandboxConfigForAgent(cfg).browser;
  if (
    defaultBrowser.enabled &&
    defaultBrowser.network.trim().toLowerCase() === "bridge" &&
    !defaultBrowser.cdpSourceRange?.trim()
  ) {
    browserExposurePaths.push("agents.defaults.sandbox.browser");
  }
  for (const entry of agents) {
    if (!entry || typeof entry !== "object" || typeof entry.id !== "string") {
      continue;
    }
    const browser = resolveSandboxConfigForAgent(cfg, entry.id).browser;
    if (!browser.enabled) {
      continue;
    }
    if (browser.network.trim().toLowerCase() !== "bridge") {
      continue;
    }
    if (browser.cdpSourceRange?.trim()) {
      continue;
    }
    browserExposurePaths.push(`agents.list.${entry.id}.sandbox.browser`);
  }
  if (browserExposurePaths.length > 0) {
    findings.push({
      checkId: "sandbox.browser_cdp_bridge_unrestricted",
      severity: "warn",
      title: "Sandbox browser CDP may be reachable by peer containers",
      detail:
        "These sandbox browser configs use Docker bridge networking with no CDP source restriction:\n" +
        browserExposurePaths.map((entry) => `- ${entry}`).join("\n"),
      remediation:
        "Set sandbox.browser.network to a dedicated bridge network (recommended default: deneb-sandbox-browser), " +
        "or set sandbox.browser.cdpSourceRange (for example 172.21.0.1/32) to restrict container-edge CDP ingress.",
    });
  }

  return findings;
}
