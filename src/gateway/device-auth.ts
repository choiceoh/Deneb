// Stub: device auth removed for solo-dev simplification.
export function buildDeviceAuthPayload(_params: {
  deviceId: string;
  clientId: string;
  clientMode: string;
  role: string;
  scopes: string[];
  signedAtMs: number;
  token: string | null;
  nonce: string;
}): string {
  return "";
}

export function buildDeviceAuthPayloadV3(_params: {
  deviceId: string;
  clientId: string;
  clientMode: string;
  role: string;
  scopes: string[];
  signedAtMs: number;
  token: string | null;
  nonce: string;
  platform?: string;
  deviceFamily?: string;
}): string {
  return "";
}

export function normalizeDeviceMetadataForAuth(_value?: string): string {
  return "";
}
