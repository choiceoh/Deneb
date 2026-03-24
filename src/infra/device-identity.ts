import { createHash, createPublicKey, generateKeyPairSync, sign, verify } from "node:crypto";

export type DeviceIdentity = {
  deviceId: string;
  publicKey?: string;
  publicKeyPem?: string;
  privateKeyPem?: string;
};

// In-memory identity cache keyed by path (or "__default__" for no-path calls).
const identityCache = new Map<string, DeviceIdentity>();

/**
 * Load or create a device identity. When no persistent path is provided the
 * identity is generated in-memory and cached for the process lifetime. This
 * is sufficient for gateway tests and solo-dev mode.
 */
export function loadOrCreateDeviceIdentity(pathOrOpts?: unknown): DeviceIdentity | undefined {
  const key =
    typeof pathOrOpts === "string"
      ? pathOrOpts
      : typeof pathOrOpts === "object" && pathOrOpts !== null
        ? JSON.stringify(pathOrOpts)
        : "__default__";
  const cached = identityCache.get(key);
  if (cached) {
    return cached;
  }
  const { publicKey, privateKey } = generateKeyPairSync("ed25519");
  const publicKeyPem = publicKey.export({ type: "spki", format: "pem" });
  const privateKeyPem = privateKey.export({ type: "pkcs8", format: "pem" });
  const rawPublicKey = publicKey.export({ type: "spki", format: "der" });
  const deviceId = createHash("sha256").update(rawPublicKey).digest("hex").slice(0, 32);

  const identity: DeviceIdentity = { deviceId, publicKeyPem, privateKeyPem };
  identityCache.set(key, identity);
  return identity;
}

export function getDeviceId(): string | undefined {
  return loadOrCreateDeviceIdentity()?.deviceId;
}

/**
 * Extract the raw 32-byte Ed25519 public key from an SPKI PEM and return it
 * as a base64url string.
 */
export function publicKeyRawBase64UrlFromPem(pem: string): string {
  if (!pem) {
    return "";
  }
  const b64 = pem.replace(/-----[A-Z ]+-----/g, "").replace(/\s/g, "");
  const der = Buffer.from(b64, "base64");
  // Ed25519 SPKI DER is 44 bytes: 12-byte header + 32-byte raw key.
  const raw = der.subarray(der.length - 32);
  return raw.toString("base64url");
}

/**
 * Sign a payload string using a device identity's private key. When no
 * identity is provided the default (cached) identity is used. Returns a
 * base64url-encoded Ed25519 signature.
 */
export function signDevicePayload(
  payload: unknown,
  identityOrKey?: DeviceIdentity | string,
): string {
  const privateKeyPem =
    typeof identityOrKey === "string"
      ? identityOrKey
      : (identityOrKey ?? loadOrCreateDeviceIdentity())?.privateKeyPem;
  if (!privateKeyPem) {
    return "";
  }
  const payloadStr = typeof payload === "string" ? payload : JSON.stringify(payload);
  const signature = sign(null, Buffer.from(payloadStr), privateKeyPem);
  return signature.toString("base64url");
}

/**
 * Derive a stable device ID from a raw base64url public key.
 */
export function deriveDeviceIdFromPublicKey(publicKeyBase64Url: string): string {
  if (!publicKeyBase64Url) {
    return "";
  }
  const raw = Buffer.from(publicKeyBase64Url, "base64url");
  // Reconstruct SPKI to match the derivation in loadOrCreateDeviceIdentity.
  const spkiHeader = Buffer.from("302a300506032b6570032100", "hex");
  const spki = Buffer.concat([spkiHeader, raw]);
  return createHash("sha256").update(spki).digest("hex").slice(0, 32);
}

export function normalizeDevicePublicKeyBase64Url(key: string): string {
  if (!key) {
    return "";
  }
  // Normalize by round-tripping through buffer to canonical base64url.
  return Buffer.from(key, "base64url").toString("base64url");
}

/**
 * Verify an Ed25519 signature against a raw base64url public key.
 */
export function verifyDeviceSignature(
  publicKeyBase64Url: string,
  payload: string,
  signature: string,
): boolean {
  if (!publicKeyBase64Url || !payload || !signature) {
    return false;
  }
  try {
    const rawKey = Buffer.from(publicKeyBase64Url, "base64url");
    // Reconstruct the SPKI DER envelope for Ed25519.
    const spkiHeader = Buffer.from("302a300506032b6570032100", "hex");
    const spki = Buffer.concat([spkiHeader, rawKey]);
    const publicKey = createPublicKey({ key: spki, format: "der", type: "spki" });
    const signatureBuffer = Buffer.from(signature, "base64url");
    return verify(null, Buffer.from(payload), publicKey, signatureBuffer);
  } catch {
    return false;
  }
}
