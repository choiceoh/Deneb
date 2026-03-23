/**
 * Exposure and multi-user security audit collectors.
 *
 * Checks: open groups with elevated tools, open groups with runtime/fs tools,
 * potential multi-user setup.
 */
import type { DenebConfig } from "../config/config.js";
import type { SecurityAuditFinding } from "./audit-extra-shared.js";
import {
  collectRiskyToolExposureContexts,
  listGroupPolicyOpen,
  listPotentialMultiUserSignals,
} from "./audit-extra.sync.helpers.js";

export function collectExposureMatrixFindings(cfg: DenebConfig): SecurityAuditFinding[] {
  const findings: SecurityAuditFinding[] = [];
  const openGroups = listGroupPolicyOpen(cfg);
  if (openGroups.length === 0) {
    return findings;
  }

  const elevatedEnabled = cfg.tools?.elevated?.enabled !== false;
  if (elevatedEnabled) {
    findings.push({
      checkId: "security.exposure.open_groups_with_elevated",
      severity: "critical",
      title: "Open groupPolicy with elevated tools enabled",
      detail:
        `Found groupPolicy="open" at:\n${openGroups.map((p) => `- ${p}`).join("\n")}\n` +
        "With tools.elevated enabled, a prompt injection in those rooms can become a high-impact incident.",
      remediation: `Set groupPolicy="allowlist" and keep elevated allowlists extremely tight.`,
    });
  }

  const { riskyContexts, hasRuntimeRisk } = collectRiskyToolExposureContexts(cfg);

  if (riskyContexts.length > 0) {
    findings.push({
      checkId: "security.exposure.open_groups_with_runtime_or_fs",
      severity: hasRuntimeRisk ? "critical" : "warn",
      title: "Open groupPolicy with runtime/filesystem tools exposed",
      detail:
        `Found groupPolicy="open" at:\n${openGroups.map((p) => `- ${p}`).join("\n")}\n` +
        `Risky tool exposure contexts:\n${riskyContexts.map((line) => `- ${line}`).join("\n")}\n` +
        "Prompt injection in open groups can trigger command/file actions in these contexts.",
      remediation:
        'For open groups, prefer tools.profile="messaging" (or deny group:runtime/group:fs), set tools.fs.workspaceOnly=true, and use agents.defaults.sandbox.mode="all" for exposed agents.',
    });
  }

  return findings;
}

export function collectLikelyMultiUserSetupFindings(cfg: DenebConfig): SecurityAuditFinding[] {
  const findings: SecurityAuditFinding[] = [];
  const signals = listPotentialMultiUserSignals(cfg);
  if (signals.length === 0) {
    return findings;
  }

  const { riskyContexts, hasRuntimeRisk } = collectRiskyToolExposureContexts(cfg);
  const impactLine = hasRuntimeRisk
    ? "Runtime/process tools are exposed without full sandboxing in at least one context."
    : "No unguarded runtime/process tools were detected by this heuristic.";
  const riskyContextsDetail =
    riskyContexts.length > 0
      ? `Potential high-impact tool exposure contexts:\n${riskyContexts.map((line) => `- ${line}`).join("\n")}`
      : "No unguarded runtime/filesystem contexts detected.";

  findings.push({
    checkId: "security.trust_model.multi_user_heuristic",
    severity: "warn",
    title: "Potential multi-user setup detected (personal-assistant model warning)",
    detail:
      "Heuristic signals indicate this gateway may be reachable by multiple users:\n" +
      signals.map((signal) => `- ${signal}`).join("\n") +
      `\n${impactLine}\n${riskyContextsDetail}\n` +
      "Deneb's default security model is personal-assistant (one trusted operator boundary), not hostile multi-tenant isolation on one shared gateway.",
    remediation:
      'If users may be mutually untrusted, split trust boundaries (separate gateways + credentials, ideally separate OS users/hosts). If you intentionally run shared-user access, set agents.defaults.sandbox.mode="all", keep tools.fs.workspaceOnly=true, deny runtime/fs/web tools unless required, and keep personal/private identities + credentials off that runtime.',
  });

  return findings;
}
