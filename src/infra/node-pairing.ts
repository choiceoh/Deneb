// Stub: node pairing removed for solo-dev simplification.
export type PairedNode = {
  nodeId: string;
  displayName?: string;
  platform?: string;
  deviceFamily?: string;
  commands?: string[];
  remoteIp?: string;
  bins?: string[];
};

export async function listNodePairing(): Promise<{ pending: unknown[]; paired: PairedNode[] }> {
  return { pending: [], paired: [] };
}

export async function approveNodePairing(_requestId: string): Promise<unknown> {
  return null;
}

export async function rejectNodePairing(_requestId: string): Promise<boolean> {
  return false;
}

export async function updatePairedNodeMetadata(
  _nodeId: string,
  _metadata: Record<string, unknown>,
): Promise<void> {}
