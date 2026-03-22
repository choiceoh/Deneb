// Minimal stub: exec approvals removed for solo-dev simplification.
// Types and no-op functions retained for compilation compatibility.

export type ExecHost = "sandbox" | "gateway" | "node";
export type ExecSecurity = "deny" | "allowlist" | "full";
export type ExecAsk = "off" | "on-miss" | "always";

export function normalizeExecHost(value?: string | null): ExecHost | null {
  if (!value) {
    return null;
  }
  const v = value.trim().toLowerCase();
  if (v === "sandbox" || v === "gateway" || v === "node") {
    return v;
  }
  return null;
}

export function normalizeExecSecurity(value?: string | null): ExecSecurity | null {
  if (!value) {
    return null;
  }
  const v = value.trim().toLowerCase();
  if (v === "deny" || v === "allowlist" || v === "full") {
    return v;
  }
  return null;
}

export function normalizeExecAsk(value?: string | null): ExecAsk | null {
  if (!value) {
    return null;
  }
  const v = value.trim().toLowerCase();
  if (v === "off" || v === "on-miss" || v === "always") {
    return v;
  }
  return null;
}

export function minSecurity(a: ExecSecurity, b: ExecSecurity): ExecSecurity {
  const order: ExecSecurity[] = ["deny", "allowlist", "full"];
  return order[Math.min(order.indexOf(a), order.indexOf(b))] ?? "deny";
}

export function maxAsk(a: ExecAsk, b: ExecAsk): ExecAsk {
  const order: ExecAsk[] = ["off", "on-miss", "always"];
  return order[Math.max(order.indexOf(a), order.indexOf(b))] ?? "on-miss";
}

export type SystemRunApprovalBinding = { boundTo?: string };
export type SystemRunApprovalFileOperand = { path: string; action: string };
export type SystemRunApprovalPlan = { steps?: unknown[] };

export type ExecApprovalRequestPayload = {
  command: string;
  commandPreview?: string;
  commandArgv?: string[];
  envKeys?: string[];
  systemRunBinding?: SystemRunApprovalBinding;
  systemRunPlan?: SystemRunApprovalPlan;
  cwd?: string;
  nodeId?: string;
  host?: ExecHost;
  security?: ExecSecurity;
  ask?: ExecAsk;
  agentId?: string;
  resolvedPath?: string;
  sessionKey?: string;
  turnSourceChannel?: string;
  turnSourceTo?: string;
  turnSourceAccountId?: string;
  turnSourceThreadId?: string;
};

export type ExecApprovalRequest = ExecApprovalRequestPayload & { id: string; ts: number };

export type ExecApprovalDecision = "allow-once" | "allow-always" | "deny";

export type ExecApprovalResolved = {
  id: string;
  decision: ExecApprovalDecision;
  resolvedBy?: string;
  ts: number;
  request?: ExecApprovalRequestPayload;
};

export type ExecApprovalsDefaults = {
  security?: ExecSecurity;
  ask?: ExecAsk;
  askFallback?: ExecApprovalDecision;
  autoAllowSkills?: boolean;
};

export type ExecAllowlistEntry = {
  id?: string;
  pattern: string;
  lastUsedAt?: number;
  lastUsedCommand?: string;
  lastResolvedPath?: string;
};

export type ExecApprovalsAgent = ExecApprovalsDefaults & {
  allowlist?: ExecAllowlistEntry[];
};

export type ExecApprovalsFile = {
  version: number;
  socket?: { path?: string; token?: string };
  defaults?: ExecApprovalsDefaults;
  agents?: Record<string, ExecApprovalsAgent>;
};

export type ExecApprovalsSnapshot = { file: ExecApprovalsFile; hash: string };

export type ExecApprovalsResolved = ExecApprovalsDefaults & {
  allowlist: ExecAllowlistEntry[];
};

export type ExecApprovalsDefaultOverrides = Partial<ExecApprovalsDefaults>;

export const DEFAULT_EXEC_APPROVAL_TIMEOUT_MS = 120_000;

export function resolveExecApprovalsPath(): string {
  return "";
}

export function resolveExecApprovalsSocketPath(): string {
  return "";
}

export function normalizeExecApprovals(file: ExecApprovalsFile): ExecApprovalsFile {
  return file;
}

export function mergeExecApprovalsSocketDefaults(_params: {
  file: ExecApprovalsFile;
  socketPath?: string;
}): ExecApprovalsFile {
  return _params.file;
}

export function readExecApprovalsSnapshot(): ExecApprovalsSnapshot {
  return { file: { version: 1 }, hash: "" };
}

export function loadExecApprovals(): ExecApprovalsFile {
  return { version: 1 };
}

export function saveExecApprovals(_file: ExecApprovalsFile) {}

export function ensureExecApprovals(): ExecApprovalsFile {
  return { version: 1 };
}

export function resolveExecApprovals(
  _agentId?: string,
  _overrides?: ExecApprovalsDefaultOverrides,
): ExecApprovalsResolved {
  return { security: "full", ask: "off", allowlist: [] };
}

export function resolveExecApprovalsFromFile(_params: {
  file: ExecApprovalsFile;
  agentId?: string;
  overrides?: ExecApprovalsDefaultOverrides;
}): ExecApprovalsResolved {
  return { security: "full", ask: "off", allowlist: [] };
}

export function requiresExecApproval(): boolean {
  return false;
}

export function addAllowlistEntry(): void {}

export function recordAllowlistUse(): void {}

export function matchAllowlist(): boolean {
  return true;
}
