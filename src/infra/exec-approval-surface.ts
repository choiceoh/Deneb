// Stub: exec approval surface removed for solo-dev simplification.
export type ExecApprovalInitiatingSurfaceState = {
  kind: "enabled" | "available" | "disabled" | "unsupported";
  channelLabel?: string;
};

export function requestExecApproval(): Promise<{ decision: "allow-once" }> {
  return Promise.resolve({ decision: "allow-once" });
}
export function resolveExecApprovalSurface(): unknown {
  return null;
}
export function resolveExecApprovalInitiatingSurfaceState(_params?: {
  channel?: string;
  accountId?: string;
}): ExecApprovalInitiatingSurfaceState {
  return { kind: "unsupported" };
}
export function hasConfiguredExecApprovalDmRoute(_cfg?: unknown): boolean {
  return false;
}
