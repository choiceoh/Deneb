import { describe, expect, it, vi } from "vitest";
import * as noteModule from "../terminal/note.js";
import { loadAndMaybeMigrateDoctorConfig } from "./doctor-config-flow.js";
import { runDoctorConfigWithInput } from "./doctor-config-flow.test-utils.js";

describe("doctor config flow", () => {
  it("preserves invalid config for doctor repairs", async () => {
    const result = await runDoctorConfigWithInput({
      config: {
        gateway: { auth: { mode: "token", token: 123 } },
        agents: { list: [{ id: "pi" }] },
      },
      run: loadAndMaybeMigrateDoctorConfig,
    });

    expect((result.cfg as Record<string, unknown>).gateway).toEqual({
      auth: { mode: "token", token: 123 },
    });
  });

  it("drops unknown keys on repair", async () => {
    const result = await runDoctorConfigWithInput({
      repair: true,
      config: {
        bridge: { bind: "auto" },
        gateway: { auth: { mode: "token", token: "ok", extra: true } },
        agents: { list: [{ id: "pi" }] },
      },
      run: loadAndMaybeMigrateDoctorConfig,
    });

    const cfg = result.cfg as Record<string, unknown>;
    expect(cfg.bridge).toBeUndefined();
    expect((cfg.gateway as Record<string, unknown>)?.auth).toEqual({
      mode: "token",
      token: "ok",
    });
  });

  it("migrates legacy browser extension profiles to existing-session on repair", async () => {
    const result = await runDoctorConfigWithInput({
      repair: true,
      config: {
        browser: {
          relayBindHost: "0.0.0.0",
          profiles: {
            chromeLive: {
              driver: "extension",
              color: "#00AA00",
            },
          },
        },
      },
      run: loadAndMaybeMigrateDoctorConfig,
    });

    const browser = (result.cfg as { browser?: Record<string, unknown> }).browser ?? {};
    expect(browser.relayBindHost).toBeUndefined();
    expect(
      ((browser.profiles as Record<string, { driver?: string }>)?.chromeLive ?? {}).driver,
    ).toBe("existing-session");
  });

  it("notes legacy browser extension migration changes", async () => {
    const noteSpy = vi.spyOn(noteModule, "note").mockImplementation(() => {});
    try {
      await runDoctorConfigWithInput({
        config: {
          browser: {
            relayBindHost: "127.0.0.1",
            profiles: {
              chromeLive: {
                driver: "extension",
                color: "#00AA00",
              },
            },
          },
        },
        run: loadAndMaybeMigrateDoctorConfig,
      });

      const messages = noteSpy.mock.calls
        .filter((call) => call[1] === "Doctor changes")
        .map((call) => String(call[0]));
      expect(
        messages.some((line) => line.includes('browser.profiles.chromeLive.driver "extension"')),
      ).toBe(true);
      expect(messages.some((line) => line.includes("browser.relayBindHost"))).toBe(true);
    } finally {
      noteSpy.mockRestore();
    }
  });

  it("resolves Telegram @username allowFrom entries to numeric IDs on repair", async () => {
    const fetchSpy = vi.fn(async (url: string) => {
      const u = String(url);
      const chatId = new URL(u).searchParams.get("chat_id") ?? "";
      const id =
        chatId.toLowerCase() === "@testuser"
          ? 111
          : chatId.toLowerCase() === "@groupuser"
            ? 222
            : chatId.toLowerCase() === "@topicuser"
              ? 333
              : chatId.toLowerCase() === "@accountuser"
                ? 444
                : null;
      return {
        ok: id != null,
        json: async () => (id != null ? { ok: true, result: { id } } : { ok: false }),
      } as unknown as Response;
    });
    vi.stubGlobal("fetch", fetchSpy);
    try {
      const result = await runDoctorConfigWithInput({
        repair: true,
        config: {
          channels: {
            telegram: {
              botToken: "123:abc",
              allowFrom: ["@testuser"],
              groupAllowFrom: ["groupUser"],
              groups: {
                "-100123": {
                  allowFrom: ["tg:@topicUser"],
                  topics: { "99": { allowFrom: ["@accountUser"] } },
                },
              },
              accounts: {
                alerts: { botToken: "456:def", allowFrom: ["@accountUser"] },
              },
            },
          },
        },
        run: loadAndMaybeMigrateDoctorConfig,
      });

      const cfg = result.cfg as unknown as {
        channels: {
          telegram: {
            allowFrom?: string[];
            groupAllowFrom?: string[];
            groups: Record<
              string,
              { allowFrom: string[]; topics: Record<string, { allowFrom: string[] }> }
            >;
            accounts: Record<string, { allowFrom?: string[]; groupAllowFrom?: string[] }>;
          };
        };
      };
      expect(cfg.channels.telegram.allowFrom).toBeUndefined();
      expect(cfg.channels.telegram.groupAllowFrom).toBeUndefined();
      expect(cfg.channels.telegram.groups["-100123"].allowFrom).toEqual(["333"]);
      expect(cfg.channels.telegram.groups["-100123"].topics["99"].allowFrom).toEqual(["444"]);
      expect(cfg.channels.telegram.accounts.alerts.allowFrom).toEqual(["444"]);
      expect(cfg.channels.telegram.accounts.default.allowFrom).toEqual(["111"]);
      expect(cfg.channels.telegram.accounts.default.groupAllowFrom).toEqual(["222"]);
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("does not crash when Telegram allowFrom repair sees unavailable SecretRef-backed credentials", async () => {
    const noteSpy = vi.spyOn(noteModule, "note").mockImplementation(() => {});
    const fetchSpy = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);
    try {
      const result = await runDoctorConfigWithInput({
        repair: true,
        config: {
          secrets: {
            providers: {
              default: { source: "env" },
            },
          },
          channels: {
            telegram: {
              botToken: { source: "env", provider: "default", id: "TELEGRAM_BOT_TOKEN" },
              allowFrom: ["@testuser"],
            },
          },
        },
        run: loadAndMaybeMigrateDoctorConfig,
      });

      const cfg = result.cfg as {
        channels?: {
          telegram?: {
            allowFrom?: string[];
            accounts?: Record<string, { allowFrom?: string[] }>;
          };
        };
      };
      const retainedAllowFrom =
        cfg.channels?.telegram?.accounts?.default?.allowFrom ?? cfg.channels?.telegram?.allowFrom;
      expect(retainedAllowFrom).toEqual(["@testuser"]);
      expect(fetchSpy).not.toHaveBeenCalled();
      expect(
        noteSpy.mock.calls.some((call) =>
          String(call[0]).includes(
            "configured Telegram bot credentials are unavailable in this command path",
          ),
        ),
      ).toBe(true);
    } finally {
      noteSpy.mockRestore();
      vi.unstubAllGlobals();
    }
  });

  it("warns and continues when Telegram account inspection hits inactive SecretRef surfaces", async () => {
    const noteSpy = vi.spyOn(noteModule, "note").mockImplementation(() => {});
    const fetchSpy = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);
    try {
      const result = await runDoctorConfigWithInput({
        repair: true,
        config: {
          secrets: {
            providers: {
              default: { source: "env" },
            },
          },
          channels: {
            telegram: {
              accounts: {
                inactive: {
                  enabled: false,
                  botToken: { source: "env", provider: "default", id: "TELEGRAM_BOT_TOKEN" },
                  allowFrom: ["@testuser"],
                },
              },
            },
          },
        },
        run: loadAndMaybeMigrateDoctorConfig,
      });

      const cfg = result.cfg as {
        channels?: {
          telegram?: {
            accounts?: Record<string, { allowFrom?: string[] }>;
          };
        };
      };
      expect(cfg.channels?.telegram?.accounts?.inactive?.allowFrom).toEqual(["@testuser"]);
      expect(fetchSpy).not.toHaveBeenCalled();
      expect(
        noteSpy.mock.calls.some((call) =>
          String(call[0]).includes("Telegram account inactive: failed to inspect bot token"),
        ),
      ).toBe(true);
      expect(
        noteSpy.mock.calls.some((call) =>
          String(call[0]).includes(
            "Telegram allowFrom contains @username entries, but no Telegram bot token is configured",
          ),
        ),
      ).toBe(true);
    } finally {
      noteSpy.mockRestore();
      vi.unstubAllGlobals();
    }
  });

  it("migrates legacy toolsBySender keys to typed id entries on repair", async () => {
    const result = await runDoctorConfigWithInput({
      repair: true,
      config: {
        channels: {
          telegram: {
            groups: {
              "-100123": {
                toolsBySender: {
                  owner: { allow: ["exec"] },
                  alice: { deny: ["exec"] },
                  "id:owner": { deny: ["exec"] },
                  "username:@ops-bot": { allow: ["fs.read"] },
                  "*": { deny: ["exec"] },
                },
              },
            },
          },
        },
      },
      run: loadAndMaybeMigrateDoctorConfig,
    });

    const cfg = result.cfg as unknown as {
      channels: {
        telegram: {
          groups: {
            "-100123": {
              toolsBySender: Record<string, { allow?: string[]; deny?: string[] }>;
            };
          };
        };
      };
    };
    const toolsBySender = cfg.channels.telegram.groups["-100123"].toolsBySender;
    expect(toolsBySender.owner).toBeUndefined();
    expect(toolsBySender.alice).toBeUndefined();
    expect(toolsBySender["id:owner"]).toEqual({ deny: ["exec"] });
    expect(toolsBySender["id:alice"]).toEqual({ deny: ["exec"] });
    expect(toolsBySender["username:@ops-bot"]).toEqual({ allow: ["fs.read"] });
    expect(toolsBySender["*"]).toEqual({ deny: ["exec"] });
  });

  it("migrates top-level heartbeat into agents.defaults.heartbeat on repair", async () => {
    const result = await runDoctorConfigWithInput({
      repair: true,
      config: {
        heartbeat: {
          model: "anthropic/claude-3-5-haiku-20241022",
          every: "30m",
        },
      },
      run: loadAndMaybeMigrateDoctorConfig,
    });

    const cfg = result.cfg as {
      heartbeat?: unknown;
      agents?: {
        defaults?: {
          heartbeat?: {
            model?: string;
            every?: string;
          };
        };
      };
    };
    expect(cfg.heartbeat).toBeUndefined();
    expect(cfg.agents?.defaults?.heartbeat).toMatchObject({
      model: "anthropic/claude-3-5-haiku-20241022",
      every: "30m",
    });
  });

  it("migrates top-level heartbeat visibility into channels.defaults.heartbeat on repair", async () => {
    const result = await runDoctorConfigWithInput({
      repair: true,
      config: {
        heartbeat: {
          showOk: true,
          showAlerts: false,
        },
      },
      run: loadAndMaybeMigrateDoctorConfig,
    });

    const cfg = result.cfg as {
      heartbeat?: unknown;
      channels?: {
        defaults?: {
          heartbeat?: {
            showOk?: boolean;
            showAlerts?: boolean;
            useIndicator?: boolean;
          };
        };
      };
    };
    expect(cfg.heartbeat).toBeUndefined();
    expect(cfg.channels?.defaults?.heartbeat).toMatchObject({
      showOk: true,
      showAlerts: false,
    });
  });
});
