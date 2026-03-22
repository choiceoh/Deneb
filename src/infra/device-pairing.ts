// Device pairing (disabled in solo-dev mode).
export type PairedDevice = {
  deviceId: string;
  publicKey?: string;
  displayName?: string;
  platform?: string;
  deviceFamily?: string;
  role?: string;
  roles?: string[];
  scopes?: string[];
  approvedScopes?: string[];
  tokens?: Record<string, { token?: string; scopes?: string[] }>;
  remoteIp?: string;
  createdAtMs?: number;
  approvedAtMs?: number;
};

type PendingPairingEntry = {
  requestId: string;
  deviceId: string;
  role?: string;
  roles?: string[];
  scopes?: string[];
};

export async function listDevicePairing(): Promise<{
  pending: PendingPairingEntry[];
  paired: PairedDevice[];
}> {
  return { pending: [], paired: [] };
}

export type PairingRequest = {
  requestId: string;
  silent?: boolean;
};

export type PairingResult = {
  request: PairingRequest;
  created: boolean;
};

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

export async function requestDevicePairing(_params?: unknown): Promise<PairingResult> {
  return { request: { requestId: "", silent: true }, created: false };
}

export async function rotateDeviceToken(): Promise<unknown> {
  return null;
}

export async function revokeDeviceToken(_params?: {
  deviceId: string;
  role: string;
}): Promise<unknown> {
  return null;
}

export async function getPairedDevice(_deviceId: string): Promise<PairedDevice | undefined> {
  return undefined;
}

export type DeviceTokenRecord = {
  token: string;
  role: string;
  scopes: string[];
  createdAtMs?: number;
  rotatedAtMs?: number;
};

export async function ensureDeviceToken(_params: {
  deviceId: string;
  role: string;
  scopes: string[];
}): Promise<DeviceTokenRecord | null> {
  return null;
}

export async function updatePairedDeviceMetadata(
  _deviceId: string,
  _metadata: unknown,
): Promise<void> {}

export async function verifyDeviceToken(_params: {
  deviceId: string;
  token: string;
  role: string;
  scopes: string[];
}): Promise<{ ok: boolean }> {
  return { ok: false };
}
