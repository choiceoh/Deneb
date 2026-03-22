// Stub: device bootstrap removed for solo-dev simplification.
export async function issueDeviceBootstrapToken(): Promise<string> {
  return "";
}

export async function verifyDeviceBootstrapToken(_token: string): Promise<{ valid: boolean }> {
  return { valid: false };
}
