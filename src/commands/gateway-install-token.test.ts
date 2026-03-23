import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DenebConfig } from "../config/config.js";

const readConfigFileSnapshotMock = vi.hoisted(() => vi.fn());
const writeConfigFileMock = vi.hoisted(() => vi.fn());
const resolveSecretInputRefMock = vi.hoisted(() =>
  vi.fn((): { ref: unknown } => ({ ref: undefined })),
);
const hasConfiguredSecretInputMock = vi.hoisted(() =>
  vi.fn((value: unknown) => {
    if (typeof value === "string") {
      return value.trim().length > 0;
    }
    return value != null;
  }),
);
const resolveGatewayAuthMock = vi.hoisted(() =>
  vi.fn(() => ({
    mode: "token",
    token: undefined,
    password: undefined,
    allowTailscale: false,
  })),
);
const resolveSecretRefValuesMock = vi.hoisted(() => vi.fn());
const secretRefKeyMock = vi.hoisted(() => vi.fn(() => "env:default:DENEB_GATEWAY_TOKEN"));
const randomTokenMock = vi.hoisted(() => vi.fn(() => "generated-token"));

vi.mock("../config/config.js", () => ({
  readConfigFileSnapshot: readConfigFileSnapshotMock,
  writeConfigFile: writeConfigFileMock,
}));

vi.mock("../config/types.secrets.js", () => ({
  resolveSecretInputRef: resolveSecretInputRefMock,
  hasConfiguredSecretInput: hasConfiguredSecretInputMock,
}));

vi.mock("../gateway/auth/auth.js", () => ({
  resolveGatewayAuth: resolveGatewayAuthMock,
}));

vi.mock("../secrets/ref-contract.js", () => ({
  secretRefKey: secretRefKeyMock,
}));

vi.mock("../secrets/resolve.js", () => ({
  resolveSecretRefValues: resolveSecretRefValuesMock,
}));

vi.mock("./onboard-helpers.js", () => ({
  randomToken: randomTokenMock,
}));

const { resolveGatewayInstallToken } = await import("./gateway-install-token.js");

describe("resolveGatewayInstallToken", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    readConfigFileSnapshotMock.mockResolvedValue({ exists: false, valid: true, config: {} });
    resolveSecretInputRefMock.mockReturnValue({ ref: undefined });
    hasConfiguredSecretInputMock.mockImplementation((value: unknown) => {
      if (typeof value === "string") {
        return value.trim().length > 0;
      }
      return value != null;
    });
    resolveSecretRefValuesMock.mockResolvedValue(new Map());
    resolveGatewayAuthMock.mockReturnValue({
      mode: "token",
      token: undefined,
      password: undefined,
      allowTailscale: false,
    });
    randomTokenMock.mockReturnValue("generated-token");
  });

  it("uses plaintext gateway.auth.token when configured", async () => {
    const result = await resolveGatewayInstallToken({
      config: {
        gateway: { auth: { token: "config-token" } },
      } as DenebConfig,
      env: {} as NodeJS.ProcessEnv,
    });

    expect(result).toEqual({
      token: "config-token",
      tokenRefConfigured: false,
      unavailableReason: undefined,
      warnings: [],
    });
  });

  it("returns no token and no warnings when SecretRef-backed token is configured", async () => {
    const tokenRef = { source: "env", provider: "default", id: "DENEB_GATEWAY_TOKEN" };
    resolveSecretInputRefMock.mockReturnValue({ ref: tokenRef });

    const result = await resolveGatewayInstallToken({
      config: {
        gateway: { auth: { mode: "token", token: tokenRef } },
      } as DenebConfig,
      env: { DENEB_GATEWAY_TOKEN: "resolved-token" } as NodeJS.ProcessEnv,
    });

    expect(result.token).toBeUndefined();
    expect(result.tokenRefConfigured).toBe(true);
    expect(result.unavailableReason).toBeUndefined();
    expect(result.warnings).toEqual([]);
  });

  it("returns no unavailable reason when token SecretRef is unresolved (needsToken is always false)", async () => {
    resolveSecretInputRefMock.mockReturnValue({
      ref: { source: "env", provider: "default", id: "MISSING_GATEWAY_TOKEN" },
    });

    const result = await resolveGatewayInstallToken({
      config: {
        gateway: { auth: { mode: "token", token: "${MISSING_GATEWAY_TOKEN}" } },
      } as DenebConfig,
      env: {} as NodeJS.ProcessEnv,
    });

    expect(result.token).toBeUndefined();
    expect(result.unavailableReason).toBeUndefined();
  });

  it("returns plaintext token when both token and password are configured and mode is unset", async () => {
    const result = await resolveGatewayInstallToken({
      config: {
        gateway: {
          auth: {
            token: "token-value",
            password: "password-value", // pragma: allowlist secret
          },
        },
      } as DenebConfig,
      env: {} as NodeJS.ProcessEnv,
      autoGenerateWhenMissing: true,
      persistGeneratedToken: true,
    });

    expect(result.token).toBe("token-value");
    expect(result.unavailableReason).toBeUndefined();
    expect(writeConfigFileMock).not.toHaveBeenCalled();
  });

  it("does not auto-generate token (needsToken is always false)", async () => {
    const result = await resolveGatewayInstallToken({
      config: {
        gateway: { auth: { mode: "token" } },
      } as DenebConfig,
      env: {} as NodeJS.ProcessEnv,
      autoGenerateWhenMissing: true,
    });

    expect(result.token).toBeUndefined();
    expect(result.unavailableReason).toBeUndefined();
    expect(result.warnings).toEqual([]);
    expect(writeConfigFileMock).not.toHaveBeenCalled();
  });

  it("does not auto-generate when inferred mode has password SecretRef configured", async () => {
    const result = await resolveGatewayInstallToken({
      config: {
        gateway: {
          auth: {
            password: { source: "env", provider: "default", id: "GATEWAY_PASSWORD" },
          },
        },
        secrets: {
          providers: {
            default: { source: "env" },
          },
        },
      } as DenebConfig,
      env: {} as NodeJS.ProcessEnv,
      autoGenerateWhenMissing: true,
      persistGeneratedToken: true,
    });

    expect(result.token).toBeUndefined();
    expect(result.unavailableReason).toBeUndefined();
    expect(result.warnings.some((message) => message.includes("Auto-generated"))).toBe(false);
    expect(writeConfigFileMock).not.toHaveBeenCalled();
  });

  it("skips token SecretRef resolution when token auth is not required", async () => {
    const tokenRef = { source: "env", provider: "default", id: "DENEB_GATEWAY_TOKEN" };
    resolveSecretInputRefMock.mockReturnValue({ ref: tokenRef });
    const result = await resolveGatewayInstallToken({
      config: {
        gateway: {
          auth: {
            mode: "password",
            token: tokenRef,
          },
        },
      } as DenebConfig,
      env: {} as NodeJS.ProcessEnv,
    });

    expect(resolveSecretRefValuesMock).not.toHaveBeenCalled();
    expect(result.unavailableReason).toBeUndefined();
    expect(result.warnings).toEqual([]);
    expect(result.token).toBeUndefined();
    expect(result.tokenRefConfigured).toBe(true);
  });
});
