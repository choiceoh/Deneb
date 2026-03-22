// Stub: device pairing removed for solo-dev simplification.
export type PairedDevice = {
  deviceId: string;
  displayName?: string;
  platform?: string;
  roles?: string[];
  scopes?: string[];
  tokens?: unknown[];
  remoteIp?: string;
  createdAtMs?: number;
  approvedAtMs?: number;
};

export async function listDevicePairing(): Promise<{ pending: unknown[]; paired: PairedDevice[] }> {
  return { pending: [], paired: [] };
}

export async function approveDevicePairing(
  _requestId: string,
): Promise<{ device: PairedDevice } | null> {
  return null;
}

export async function rejectDevicePairing(_requestId: string): Promise<boolean> {
  return false;
}

export async function removePairedDevice(_deviceId: string): Promise<boolean> {
  return false;
}

export function summarizeDeviceTokens(_tokens?: unknown[]): unknown[] {
  return [];
}

export async function requestDevicePairing(
  _params?: unknown,
): Promise<{ requestId?: string } | null> {
  return null;
}

export async function rotateDeviceToken(): Promise<unknown> {
  return null;
}

export async function revokeDeviceToken(): Promise<unknown> {
  return null;
}

export function getPairedDevice(_deviceId: string): PairedDevice | undefined {
  return undefined;
}
