import { describe, expect, it } from "vitest";
import type { DenebConfig } from "../config/config.js";
import { resolveGatewayAuthTokenForService } from "./doctor-gateway-auth-token.js";

describe("resolveGatewayAuthTokenForService", () => {
  it("returns plaintext gateway.auth.token when configured", async () => {
    const resolved = await resolveGatewayAuthTokenForService(
      {
        gateway: {
          auth: {
            token: "config-token",
          },
        },
      } as DenebConfig,
      {} as NodeJS.ProcessEnv,
    );

    expect(resolved).toEqual({ token: "config-token" });
  });

  it("resolves SecretRef-backed gateway.auth.token", async () => {
    const resolved = await resolveGatewayAuthTokenForService(
      {
        gateway: {
          auth: {
            token: {
              source: "env",
              provider: "default",
              id: "CUSTOM_GATEWAY_TOKEN",
            },
          },
        },
        secrets: {
          providers: {
            default: { source: "env" },
          },
        },
      } as DenebConfig,
      {
        CUSTOM_GATEWAY_TOKEN: "resolved-token",
      } as NodeJS.ProcessEnv,
    );

    expect(resolved).toEqual({ token: "resolved-token" });
  });

  it("resolves env-template gateway.auth.token via SecretRef resolution", async () => {
    const resolved = await resolveGatewayAuthTokenForService(
      {
        gateway: {
          auth: {
            token: "${CUSTOM_GATEWAY_TOKEN}",
          },
        },
        secrets: {
          providers: {
            default: { source: "env" },
          },
        },
      } as DenebConfig,
      {
        CUSTOM_GATEWAY_TOKEN: "resolved-token",
      } as NodeJS.ProcessEnv,
    );

    expect(resolved).toEqual({ token: "resolved-token" });
  });

  it("falls back to DENEB_GATEWAY_TOKEN when SecretRef is unresolved", async () => {
    const resolved = await resolveGatewayAuthTokenForService(
      {
        gateway: {
          auth: {
            token: {
              source: "env",
              provider: "default",
              id: "MISSING_GATEWAY_TOKEN",
            },
          },
        },
        secrets: {
          providers: {
            default: { source: "env" },
          },
        },
      } as DenebConfig,
      {
        DENEB_GATEWAY_TOKEN: "env-fallback-token",
      } as NodeJS.ProcessEnv,
    );

    expect(resolved).toEqual({ token: "env-fallback-token" });
  });

  it("falls back to DENEB_GATEWAY_TOKEN when SecretRef resolves to empty", async () => {
    const resolved = await resolveGatewayAuthTokenForService(
      {
        gateway: {
          auth: {
            token: {
              source: "env",
              provider: "default",
              id: "CUSTOM_GATEWAY_TOKEN",
            },
          },
        },
        secrets: {
          providers: {
            default: { source: "env" },
          },
        },
      } as DenebConfig,
      {
        CUSTOM_GATEWAY_TOKEN: "   ",
        DENEB_GATEWAY_TOKEN: "env-fallback-token",
      } as NodeJS.ProcessEnv,
    );

    expect(resolved).toEqual({ token: "env-fallback-token" });
  });

  it("returns unavailableReason when SecretRef is unresolved without env fallback", async () => {
    const resolved = await resolveGatewayAuthTokenForService(
      {
        gateway: {
          auth: {
            token: {
              source: "env",
              provider: "default",
              id: "MISSING_GATEWAY_TOKEN",
            },
          },
        },
        secrets: {
          providers: {
            default: { source: "env" },
          },
        },
      } as DenebConfig,
      {} as NodeJS.ProcessEnv,
    );

    expect(resolved.token).toBeUndefined();
    expect(resolved.unavailableReason).toContain("gateway.auth.token SecretRef is configured");
  });
});
