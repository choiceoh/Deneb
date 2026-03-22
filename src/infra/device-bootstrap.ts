// Stub: device bootstrap removed for solo-dev simplification.
export async function issueDeviceBootstrapToken(): Promise<string> {
  return "";
}

export async function verifyDeviceBootstrapToken(_params: {
  deviceId: string;
  publicKey: string;
  token: string;
  role: string;
  scopes: string[];
}): Promise<{ ok: boolean; reason?: string }> {
  return { ok: false, reason: "device_bootstrap_disabled" };
}
