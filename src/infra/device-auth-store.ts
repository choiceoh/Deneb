// In-memory device auth token store for solo-dev mode.
// Provides the same API surface that client.test.ts expects to mock.

const tokenStore = new Map<string, { token: string; scopes: string[] }>();

function storeKey(deviceId: string, role: string): string {
  return `${deviceId}:${role}`;
}

export function loadDeviceAuthToken(params: {
  deviceId: string;
  role: string;
}): { token: string } | null {
  const entry = tokenStore.get(storeKey(params.deviceId, params.role));
  return entry ? { token: entry.token } : null;
}

export function storeDeviceAuthToken(params: {
  deviceId: string;
  role: string;
  token: string;
  scopes: string[];
}): void {
  tokenStore.set(storeKey(params.deviceId, params.role), {
    token: params.token,
    scopes: params.scopes,
  });
}

export function clearDeviceAuthToken(params: { deviceId: string; role: string }): void {
  tokenStore.delete(storeKey(params.deviceId, params.role));
}
