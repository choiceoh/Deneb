// Stub: exec approval forwarder removed for solo-dev simplification.
export type ExecApprovalForwarder = {
  stop: () => void;
  handleRequested: (payload: unknown) => Promise<boolean>;
  handleResolved: (payload: unknown) => Promise<void>;
};
export function createExecApprovalForwarder(_opts?: unknown): ExecApprovalForwarder {
  return {
    stop: () => {},
    handleRequested: async () => false,
    handleResolved: async () => {},
  };
}
