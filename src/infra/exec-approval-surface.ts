// Stub: exec approval surface removed for solo-dev simplification.
export function requestExecApproval(): Promise<{ decision: "allow-once" }> {
  return Promise.resolve({ decision: "allow-once" });
}
export function resolveExecApprovalSurface(): unknown {
  return null;
}
