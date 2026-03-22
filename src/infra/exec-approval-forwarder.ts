// Stub: exec approval forwarder removed for solo-dev simplification.
export type ExecApprovalForwarder = { stop: () => void };
export function createExecApprovalForwarder(_opts?: unknown): ExecApprovalForwarder {
  return { stop: () => {} };
}
