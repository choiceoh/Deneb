/**
 * Build deterministic device auth payloads for signature verification.
 * Both client and server must produce identical payloads for the same inputs.
 */

export function buildDeviceAuthPayload(params: Record<string, unknown>): string {
  // V2 payload: canonical JSON of sorted keys (excludes platform/deviceFamily).
  const { deviceId, clientId, clientMode, role, scopes, signedAtMs, token, nonce } = params;
  return JSON.stringify({
    clientId,
    clientMode,
    deviceId,
    nonce,
    role,
    scopes,
    signedAtMs,
    token: token ?? null,
  });
}

export function buildDeviceAuthPayloadV3(params: Record<string, unknown>): string {
  // V3 payload: includes platform and deviceFamily.
  const {
    deviceId,
    clientId,
    clientMode,
    role,
    scopes,
    signedAtMs,
    token,
    nonce,
    platform,
    deviceFamily,
  } = params;
  return JSON.stringify({
    clientId,
    clientMode,
    deviceFamily: deviceFamily ?? undefined,
    deviceId,
    nonce,
    platform: platform ?? undefined,
    role,
    scopes,
    signedAtMs,
    token: token ?? null,
  });
}
