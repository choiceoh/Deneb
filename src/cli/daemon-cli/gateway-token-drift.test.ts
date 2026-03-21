import { describe, expect, it } from "vitest";
import type { DenebConfig } from "../../config/config.js";
import { resolveGatewayTokenForDriftCheck } from "./gateway-token-drift.js";

describe("resolveGatewayTokenForDriftCheck", () => {
  it("prefers persisted config token over shell env", () => {
    const token = resolveGatewayTokenForDriftCheck({
      cfg: {
        gateway: {
          mode: "local",
          auth: {
            token: "config-token",
          },
        },
      } as DenebConfig,
      env: {
        DENEB_GATEWAY_TOKEN: "env-token",
      } as NodeJS.ProcessEnv,
    });

    expect(token).toBe("config-token");
  });

  it("does not fall back to caller env for unresolved config token refs", () => {
    expect(() =>
      resolveGatewayTokenForDriftCheck({
        cfg: {
          secrets: {
            providers: {
              default: { source: "env" },
            },
          },
          gateway: {
            mode: "local",
            auth: {
              token: { source: "env", provider: "default", id: "DENEB_GATEWAY_TOKEN" },
            },
          },
        } as DenebConfig,
        env: {
          DENEB_GATEWAY_TOKEN: "env-token",
        } as NodeJS.ProcessEnv,
      }),
    ).toThrow(/gateway\.auth\.token/i);
  });

  it("does not fall back to gateway.remote token for unresolved local token refs", () => {
    expect(() =>
      resolveGatewayTokenForDriftCheck({
        cfg: {
          secrets: {
            providers: {
              default: { source: "env" },
            },
          },
          gateway: {
            mode: "local",
            auth: {
              mode: "token",
              token: { source: "env", provider: "default", id: "MISSING_LOCAL_TOKEN" },
            },
            remote: {
              token: "remote-token",
            },
          },
        } as DenebConfig,
        env: {} as NodeJS.ProcessEnv,
      }),
    ).toThrow(/gateway\.auth\.token/i);
  });
});
