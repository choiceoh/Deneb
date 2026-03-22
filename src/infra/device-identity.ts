// Stub: device identity removed for solo-dev simplification.
export type DeviceIdentity = {
  deviceId: string;
  publicKey?: string;
  publicKeyPem?: string;
  privateKeyPem?: string;
};

export function loadOrCreateDeviceIdentity(_opts?: unknown): DeviceIdentity | undefined {
  return undefined;
}

export function getDeviceId(): string | undefined {
  return undefined;
}

export function publicKeyRawBase64UrlFromPem(_pem: string): string {
  return "";
}

export function signDevicePayload(_payload: unknown): string {
  return "";
}
