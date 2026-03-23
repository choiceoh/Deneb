/**
 * Node command security audit collectors.
 *
 * Checks: deny command pattern issues, dangerous allow commands.
 */
import type { DenebConfig } from "../config/config.js";
import type { SecurityAuditFinding } from "./audit-extra-shared.js";
import {
  DEFAULT_DANGEROUS_NODE_COMMANDS,
  isGatewayRemotelyExposed,
  listKnownNodeCommands,
  looksLikeNodeCommandPattern,
  normalizeNodeCommand,
  suggestKnownNodeCommands,
} from "./audit-extra.sync.helpers.js";

export function collectNodeDenyCommandPatternFindings(cfg: DenebConfig): SecurityAuditFinding[] {
  const findings: SecurityAuditFinding[] = [];
  const denyListRaw = cfg.gateway?.nodes?.denyCommands;
  if (!Array.isArray(denyListRaw) || denyListRaw.length === 0) {
    return findings;
  }

  const denyList = denyListRaw.map(normalizeNodeCommand).filter(Boolean);
  if (denyList.length === 0) {
    return findings;
  }

  const knownCommands = listKnownNodeCommands(cfg);
  const patternLike = denyList.filter((entry) => looksLikeNodeCommandPattern(entry));
  const unknownExact = denyList.filter(
    (entry) => !looksLikeNodeCommandPattern(entry) && !knownCommands.has(entry),
  );
  if (patternLike.length === 0 && unknownExact.length === 0) {
    return findings;
  }

  const detailParts: string[] = [];
  if (patternLike.length > 0) {
    detailParts.push(
      `Pattern-like entries (not supported by exact matching): ${patternLike.join(", ")}`,
    );
  }
  if (unknownExact.length > 0) {
    const unknownDetails = unknownExact
      .map((entry) => {
        const suggestions = suggestKnownNodeCommands(entry, knownCommands);
        if (suggestions.length === 0) {
          return entry;
        }
        return `${entry} (did you mean: ${suggestions.join(", ")})`;
      })
      .join(", ");

    detailParts.push(`Unknown command names (not in defaults/allowCommands): ${unknownDetails}`);
  }
  const examples = Array.from(knownCommands).slice(0, 8);

  findings.push({
    checkId: "gateway.nodes.deny_commands_ineffective",
    severity: "warn",
    title: "Some gateway.nodes.denyCommands entries are ineffective",
    detail:
      "gateway.nodes.denyCommands uses exact node command-name matching only (for example `system.run`), not shell-text filtering inside a command payload.\n" +
      detailParts.map((entry) => `- ${entry}`).join("\n"),
    remediation:
      `Use exact command names (for example: ${examples.join(", ")}). ` +
      "If you need broader restrictions, remove risky command IDs from allowCommands/default workflows and tighten tools.exec policy.",
  });

  return findings;
}

export function collectNodeDangerousAllowCommandFindings(cfg: DenebConfig): SecurityAuditFinding[] {
  const findings: SecurityAuditFinding[] = [];
  const allowRaw = cfg.gateway?.nodes?.allowCommands;
  if (!Array.isArray(allowRaw) || allowRaw.length === 0) {
    return findings;
  }

  const allow = new Set(allowRaw.map(normalizeNodeCommand).filter(Boolean));
  if (allow.size === 0) {
    return findings;
  }

  const deny = new Set((cfg.gateway?.nodes?.denyCommands ?? []).map(normalizeNodeCommand));
  const dangerousAllowed = DEFAULT_DANGEROUS_NODE_COMMANDS.filter(
    (cmd) => allow.has(cmd) && !deny.has(cmd),
  );
  if (dangerousAllowed.length === 0) {
    return findings;
  }

  findings.push({
    checkId: "gateway.nodes.allow_commands_dangerous",
    severity: isGatewayRemotelyExposed(cfg) ? "critical" : "warn",
    title: "Dangerous node commands explicitly enabled",
    detail:
      `gateway.nodes.allowCommands includes: ${dangerousAllowed.join(", ")}. ` +
      "These commands can trigger high-impact device actions (camera/screen/contacts/calendar/reminders/SMS).",
    remediation:
      "Remove these entries from gateway.nodes.allowCommands (recommended). " +
      "If you keep them, treat gateway auth as full operator access and keep gateway exposure local/tailnet-only.",
  });

  return findings;
}
