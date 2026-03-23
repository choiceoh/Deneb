import type { DenebConfig } from "../config/config.js";
import { hasConfiguredSecretInput } from "../config/types.secrets.js";
import type { SecurityAuditFinding, SecurityAuditSummary } from "./audit.types.js";

export function countBySeverity(findings: SecurityAuditFinding[]): SecurityAuditSummary {
  let critical = 0;
  let warn = 0;
  let info = 0;
  for (const f of findings) {
    if (f.severity === "critical") {
      critical += 1;
    } else if (f.severity === "warn") {
      warn += 1;
    } else {
      info += 1;
    }
  }
  return { critical, warn, info };
}

export function normalizeAllowFromList(list: Array<string | number> | undefined | null): string[] {
  if (!Array.isArray(list)) {
    return [];
  }
  return list.map((v) => String(v).trim()).filter(Boolean);
}

export function asRecord(value: unknown): Record<string, unknown> | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return undefined;
  }
  return value as Record<string, unknown>;
}

export function hasNonEmptyString(value: unknown): boolean {
  return typeof value === "string" && value.trim().length > 0;
}

export function isFeishuDocToolEnabled(cfg: DenebConfig): boolean {
  const channels = asRecord(cfg.channels);
  const feishu = asRecord(channels?.feishu);
  if (!feishu || feishu.enabled === false) {
    return false;
  }

  const baseTools = asRecord(feishu.tools);
  const baseDocEnabled = baseTools?.doc !== false;
  const baseAppId = hasNonEmptyString(feishu.appId);
  const baseAppSecret = hasConfiguredSecretInput(feishu.appSecret, cfg.secrets?.defaults);
  const baseConfigured = baseAppId && baseAppSecret;

  const accounts = asRecord(feishu.accounts);
  if (!accounts || Object.keys(accounts).length === 0) {
    return baseDocEnabled && baseConfigured;
  }

  for (const accountValue of Object.values(accounts)) {
    const account = asRecord(accountValue) ?? {};
    if (account.enabled === false) {
      continue;
    }
    const accountTools = asRecord(account.tools);
    const effectiveTools = accountTools ?? baseTools;
    const docEnabled = effectiveTools?.doc !== false;
    if (!docEnabled) {
      continue;
    }
    const accountConfigured =
      (hasNonEmptyString(account.appId) || baseAppId) &&
      (hasConfiguredSecretInput(account.appSecret, cfg.secrets?.defaults) || baseAppSecret);
    if (accountConfigured) {
      return true;
    }
  }

  return false;
}
