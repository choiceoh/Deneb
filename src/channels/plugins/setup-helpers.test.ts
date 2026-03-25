import { describe, expect, it } from "vitest";
import type { DenebConfig } from "../../config/config.js";
import { DEFAULT_ACCOUNT_ID } from "../../routing/session-key.js";
import {
  applySetupAccountConfigPatch,
  createEnvPatchedAccountSetupAdapter,
  createPatchedAccountSetupAdapter,
  moveSingleAccountChannelSectionToDefaultAccount,
  prepareScopedSetupConfig,
} from "./setup-helpers.js";

function asConfig(value: unknown): DenebConfig {
  return value as DenebConfig;
}

describe("applySetupAccountConfigPatch", () => {
  it("patches top-level config for default account and enables channel", () => {
    const next = applySetupAccountConfigPatch({
      cfg: asConfig({
        channels: {
          matrix: {
            webhookPath: "/old",
            enabled: false,
          },
        },
      }),
      channelKey: "matrix",
      accountId: DEFAULT_ACCOUNT_ID,
      patch: { webhookPath: "/new", botToken: "tok" },
    });

    expect(next.channels?.matrix).toMatchObject({
      enabled: true,
      webhookPath: "/new",
      botToken: "tok",
    });
  });

  it("patches named account config and preserves existing account enabled flag", () => {
    const next = applySetupAccountConfigPatch({
      cfg: asConfig({
        channels: {
          matrix: {
            enabled: false,
            accounts: {
              work: { botToken: "old", enabled: false },
            },
          },
        },
      }),
      channelKey: "matrix",
      accountId: "work",
      patch: { botToken: "new" },
    });

    expect(next.channels?.matrix).toMatchObject({
      enabled: true,
      accounts: {
        work: { enabled: false, botToken: "new" },
      },
    });
  });

  it("normalizes account id and preserves other accounts", () => {
    const next = applySetupAccountConfigPatch({
      cfg: asConfig({
        channels: {
          matrix: {
            accounts: {
              personal: { botToken: "personal-token" },
            },
          },
        },
      }),
      channelKey: "matrix",
      accountId: "Work Team",
      patch: { botToken: "work-token" },
    });

    expect(next.channels?.matrix).toMatchObject({
      accounts: {
        personal: { botToken: "personal-token" },
        "work-team": { enabled: true, botToken: "work-token" },
      },
    });
  });
});

describe("createPatchedAccountSetupAdapter", () => {
  it("stores default-account patch at channel root", () => {
    const adapter = createPatchedAccountSetupAdapter({
      channelKey: "matrix",
      buildPatch: (input) => ({ botToken: input.token }),
    });

    const next = adapter.applyAccountConfig({
      cfg: asConfig({ channels: { matrix: { enabled: false } } }),
      accountId: DEFAULT_ACCOUNT_ID,
      input: { name: "Personal", token: "tok" },
    });

    expect(next.channels?.matrix).toMatchObject({
      enabled: true,
      name: "Personal",
      botToken: "tok",
    });
  });

  it("migrates base name into the default account before patching a named account", () => {
    const adapter = createPatchedAccountSetupAdapter({
      channelKey: "matrix",
      buildPatch: (input) => ({ botToken: input.token }),
    });

    const next = adapter.applyAccountConfig({
      cfg: asConfig({
        channels: {
          matrix: {
            name: "Personal",
            accounts: {
              work: { botToken: "old" },
            },
          },
        },
      }),
      accountId: "Work Team",
      input: { name: "Work", token: "new" },
    });

    expect(next.channels?.matrix).toMatchObject({
      accounts: {
        default: { name: "Personal" },
        work: { botToken: "old" },
        "work-team": { enabled: true, name: "Work", botToken: "new" },
      },
    });
    expect(next.channels?.matrix).not.toHaveProperty("name");
  });

  it("can store the default account in accounts.default", () => {
    const adapter = createPatchedAccountSetupAdapter({
      channelKey: "whatsapp",
      alwaysUseAccounts: true,
      buildPatch: (input) => ({ authDir: input.authDir }),
    });

    const next = adapter.applyAccountConfig({
      cfg: asConfig({ channels: { whatsapp: {} } }),
      accountId: DEFAULT_ACCOUNT_ID,
      input: { name: "Phone", authDir: "/tmp/auth" },
    });

    expect(next.channels?.whatsapp).toMatchObject({
      accounts: {
        default: {
          enabled: true,
          name: "Phone",
          authDir: "/tmp/auth",
        },
      },
    });
    expect(next.channels?.whatsapp).not.toHaveProperty("enabled");
    expect(next.channels?.whatsapp).not.toHaveProperty("authDir");
  });
});

describe("moveSingleAccountChannelSectionToDefaultAccount", () => {
  it("promotes legacy Matrix keys into the sole named account when defaultAccount is unset", () => {
    const next = moveSingleAccountChannelSectionToDefaultAccount({
      cfg: asConfig({
        channels: {
          matrix: {
            homeserver: "https://matrix.example.org",
            userId: "@bot:example.org",
            accessToken: "token",
            accounts: {
              main: {
                enabled: true,
              },
            },
          },
        },
      }),
      channelKey: "matrix",
    });

    expect(next.channels?.matrix).toMatchObject({
      accounts: {
        main: {
          enabled: true,
          homeserver: "https://matrix.example.org",
          userId: "@bot:example.org",
          accessToken: "token",
        },
      },
    });
    expect(next.channels?.matrix?.accounts?.default).toBeUndefined();
    expect(next.channels?.matrix?.homeserver).toBeUndefined();
    expect(next.channels?.matrix?.userId).toBeUndefined();
    expect(next.channels?.matrix?.accessToken).toBeUndefined();
  });

  it("promotes legacy Matrix keys into an existing non-canonical default account key", () => {
    const next = moveSingleAccountChannelSectionToDefaultAccount({
      cfg: asConfig({
        channels: {
          matrix: {
            defaultAccount: "ops",
            homeserver: "https://matrix.example.org",
            userId: "@ops:example.org",
            accessToken: "token",
            accounts: {
              Ops: {
                enabled: true,
              },
            },
          },
        },
      }),
      channelKey: "matrix",
    });

    expect(next.channels?.matrix).toMatchObject({
      defaultAccount: "ops",
      accounts: {
        Ops: {
          enabled: true,
          homeserver: "https://matrix.example.org",
          userId: "@ops:example.org",
          accessToken: "token",
        },
      },
    });
    expect(next.channels?.matrix?.accounts?.ops).toBeUndefined();
    expect(next.channels?.matrix?.accounts?.default).toBeUndefined();
    expect(next.channels?.matrix?.homeserver).toBeUndefined();
    expect(next.channels?.matrix?.userId).toBeUndefined();
    expect(next.channels?.matrix?.accessToken).toBeUndefined();
  });
});

describe("createEnvPatchedAccountSetupAdapter", () => {
  it("rejects env mode for named accounts and requires credentials otherwise", () => {
    const adapter = createEnvPatchedAccountSetupAdapter({
      channelKey: "telegram",
      defaultAccountOnlyEnvError: "env only on default",
      missingCredentialError: "token required",
      hasCredentials: (input) => Boolean(input.token || input.tokenFile),
      buildPatch: (input) => ({ token: input.token }),
    });

    expect(
      adapter.validateInput?.({
        cfg: asConfig({}),
        accountId: "work",
        input: { useEnv: true },
      }),
    ).toBe("env only on default");

    expect(
      adapter.validateInput?.({
        cfg: asConfig({}),
        accountId: DEFAULT_ACCOUNT_ID,
        input: {},
      }),
    ).toBe("token required");

    expect(
      adapter.validateInput?.({
        cfg: asConfig({}),
        accountId: DEFAULT_ACCOUNT_ID,
        input: { token: "tok" },
      }),
    ).toBeNull();
  });
});

describe("prepareScopedSetupConfig", () => {
  it("stores the name and migrates it for named accounts when requested", () => {
    const next = prepareScopedSetupConfig({
      cfg: asConfig({
        channels: {
          telegram: {
            name: "Personal",
          },
        },
      }),
      channelKey: "telegram",
      accountId: "Work Team",
      name: "Work",
      migrateBaseName: true,
    });

    expect(next.channels?.telegram).toMatchObject({
      accounts: {
        default: { name: "Personal" },
        "work-team": { name: "Work" },
      },
    });
    expect(next.channels?.telegram).not.toHaveProperty("name");
  });

  it("keeps the base shape for the default account when migration is disabled", () => {
    const next = prepareScopedSetupConfig({
      cfg: asConfig({ channels: { irc: { enabled: true } } }),
      channelKey: "irc",
      accountId: DEFAULT_ACCOUNT_ID,
      name: "Libera",
    });

    expect(next.channels?.irc).toMatchObject({
      enabled: true,
      name: "Libera",
    });
  });
});
