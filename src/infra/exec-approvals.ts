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

export type SystemRunApprovalBinding = {
  boundTo?: string;
  argv: string[];
  cwd: string | null;
  agentId: string | null;
  sessionKey: string | null;
  envHash: string | null;
};
export type SystemRunApprovalFileOperand = {
  argvIndex: number;
  path: string;
  sha256: string;
  action?: string;
};
export type SystemRunApprovalPlan = {
  argv: string[];
  cwd?: string | null;
  commandText: string;
  commandPreview?: string | null;
  agentId?: string | null;
  sessionKey?: string | null;
  mutableFileOperand?: SystemRunApprovalFileOperand;
  steps?: unknown[];
};

export type ExecApprovalRequestPayload = {
  command: string;
  commandPreview?: string | null;
  commandArgv?: string[] | null;
  envKeys?: string[] | null;
  systemRunBinding?: SystemRunApprovalBinding | null;
  systemRunPlan?: SystemRunApprovalPlan | null;
  cwd?: string | null;
  nodeId?: string | null;
  host?: string | null;
  security?: string | null;
  ask?: string | null;
  agentId?: string | null;
  resolvedPath?: string | null;
  sessionKey?: string | null;
  turnSourceChannel?: string | null;
  turnSourceTo?: string | null;
  turnSourceAccountId?: string | null;
  turnSourceThreadId?: string | number | null;
};

export type ExecApprovalRequest = {
  id: string;
  ts: number;
  expiresAtMs: number;
  request: ExecApprovalRequestPayload;
};

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
  askFallback?: ExecApprovalDecision | "allowlist" | "full" | "deny";
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
  security: ExecSecurity;
  ask: ExecAsk;
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
): { file: ExecApprovalsFile; agent: ExecApprovalsResolved } {
  return { file: { version: 1 }, agent: { security: "full", ask: "off", allowlist: [] } };
}

export function resolveExecApprovalsFromFile(_params: {
  file: ExecApprovalsFile;
  agentId?: string;
  overrides?: ExecApprovalsDefaultOverrides;
}): { file: ExecApprovalsFile; agent: ExecApprovalsResolved } {
  return { file: _params.file, agent: { security: "full", ask: "off", allowlist: [] } };
}

export function requiresExecApproval(): boolean {
  return false;
}

export function addAllowlistEntry(): void {}

export function recordAllowlistUse(): void {}

export function matchAllowlist(): boolean {
  return true;
}

// Solo-dev stub types for exec approval reply/display/session-target.
export type ExecApprovalReplyDecision = "allow-once" | "allow-always" | "deny";

export type ExecApprovalPendingReplyParams = {
  approvalId: string;
  approvalSlug: string;
  approvalCommandId: string;
  command: string;
  cwd?: string;
  host: "gateway" | "node";
  nodeId?: string;
  expiresAtMs?: number;
  nowMs?: number;
};

export function resolveExecApprovalCommandDisplay(request: Record<string, unknown>): {
  commandText: string;
} {
  const command = typeof request.command === "string" ? request.command : "";
  const commandPreview = typeof request.commandPreview === "string" ? request.commandPreview : "";
  return { commandText: commandPreview || command };
}

export function buildExecApprovalPendingReplyPayload(params: ExecApprovalPendingReplyParams): {
  text: string;
  channelData?: Record<string, unknown>;
} {
  const text = `⏳ Exec approval pending [${params.approvalSlug}]: ${params.command}`;
  return {
    text,
    channelData: {
      execApproval: {
        approvalId: params.approvalId,
        command: params.command,
        cwd: params.cwd,
        host: params.host,
        nodeId: params.nodeId,
      },
    },
  };
}

export function getExecApprovalReplyMetadata(payload: unknown): { approvalId: string } | null {
  if (!payload || typeof payload !== "object") {
    return null;
  }
  const p = payload as Record<string, unknown>;
  const channelData = p.channelData as Record<string, unknown> | undefined;
  if (!channelData?.execApproval) {
    return null;
  }
  const ea = channelData.execApproval as Record<string, unknown>;
  return typeof ea.approvalId === "string" ? { approvalId: ea.approvalId } : null;
}

export type ExecApprovalInitiatingSurfaceState = {
  kind: "enabled" | "available" | "disabled" | "unsupported";
  channelLabel?: string;
};
