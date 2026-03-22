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

export async function approveNodePairing(_requestId: string): Promise<{ node: PairedNode } | null> {
  return null;
}

export async function rejectNodePairing(_requestId: string): Promise<{ nodeId: string } | null> {
  return null;
}

export async function updatePairedNodeMetadata(
  _nodeId: string,
  _metadata: Record<string, unknown>,
): Promise<void> {}

export async function renamePairedNode(_nodeId: string, _name: string): Promise<void> {}

export async function requestNodePairing(
  _params: unknown,
): Promise<{ requestId: string; node?: unknown }> {
  return { requestId: "" };
}

export function verifyNodeToken(_token: string): { valid: boolean; nodeId?: string } {
  return { valid: false };
}
