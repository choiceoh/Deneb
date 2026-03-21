import path from "node:path";
import { describe, expect, it } from "vitest";
import { formatCliCommand } from "./command-format.js";
import { applyCliProfileEnv, parseCliProfileArgs } from "./profile.js";

describe("parseCliProfileArgs", () => {
  it("leaves gateway --dev for subcommands", () => {
    const res = parseCliProfileArgs([
      "node",
      "deneb",
      "gateway",
      "--dev",
      "--allow-unconfigured",
    ]);
    if (!res.ok) {
      throw new Error(res.error);
    }
    expect(res.profile).toBeNull();
    expect(res.argv).toEqual(["node", "deneb", "gateway", "--dev", "--allow-unconfigured"]);
  });

  it("still accepts global --dev before subcommand", () => {
    const res = parseCliProfileArgs(["node", "deneb", "--dev", "gateway"]);
    if (!res.ok) {
      throw new Error(res.error);
    }
    expect(res.profile).toBe("dev");
    expect(res.argv).toEqual(["node", "deneb", "gateway"]);
  });

  it("parses --profile value and strips it", () => {
    const res = parseCliProfileArgs(["node", "deneb", "--profile", "work", "status"]);
    if (!res.ok) {
      throw new Error(res.error);
    }
    expect(res.profile).toBe("work");
    expect(res.argv).toEqual(["node", "deneb", "status"]);
  });

  it("rejects missing profile value", () => {
    const res = parseCliProfileArgs(["node", "deneb", "--profile"]);
    expect(res.ok).toBe(false);
  });

  it.each([
    ["--dev first", ["node", "deneb", "--dev", "--profile", "work", "status"]],
    ["--profile first", ["node", "deneb", "--profile", "work", "--dev", "status"]],
  ])("rejects combining --dev with --profile (%s)", (_name, argv) => {
    const res = parseCliProfileArgs(argv);
    expect(res.ok).toBe(false);
  });
});

describe("applyCliProfileEnv", () => {
  it("fills env defaults for dev profile", () => {
    const env: Record<string, string | undefined> = {};
    applyCliProfileEnv({
      profile: "dev",
      env,
      homedir: () => "/home/peter",
    });
    const expectedStateDir = path.join(path.resolve("/home/peter"), ".deneb-dev");
    expect(env.DENEB_PROFILE).toBe("dev");
    expect(env.DENEB_STATE_DIR).toBe(expectedStateDir);
    expect(env.DENEB_CONFIG_PATH).toBe(path.join(expectedStateDir, "deneb.json"));
    expect(env.DENEB_GATEWAY_PORT).toBe("19001");
  });

  it("does not override explicit env values", () => {
    const env: Record<string, string | undefined> = {
      DENEB_STATE_DIR: "/custom",
      DENEB_GATEWAY_PORT: "19099",
    };
    applyCliProfileEnv({
      profile: "dev",
      env,
      homedir: () => "/home/peter",
    });
    expect(env.DENEB_STATE_DIR).toBe("/custom");
    expect(env.DENEB_GATEWAY_PORT).toBe("19099");
    expect(env.DENEB_CONFIG_PATH).toBe(path.join("/custom", "deneb.json"));
  });

  it("uses DENEB_HOME when deriving profile state dir", () => {
    const env: Record<string, string | undefined> = {
      DENEB_HOME: "/srv/deneb-home",
      HOME: "/home/other",
    };
    applyCliProfileEnv({
      profile: "work",
      env,
      homedir: () => "/home/fallback",
    });

    const resolvedHome = path.resolve("/srv/deneb-home");
    expect(env.DENEB_STATE_DIR).toBe(path.join(resolvedHome, ".deneb-work"));
    expect(env.DENEB_CONFIG_PATH).toBe(
      path.join(resolvedHome, ".deneb-work", "deneb.json"),
    );
  });
});

describe("formatCliCommand", () => {
  it.each([
    {
      name: "no profile is set",
      cmd: "deneb doctor --fix",
      env: {},
      expected: "deneb doctor --fix",
    },
    {
      name: "profile is default",
      cmd: "deneb doctor --fix",
      env: { DENEB_PROFILE: "default" },
      expected: "deneb doctor --fix",
    },
    {
      name: "profile is Default (case-insensitive)",
      cmd: "deneb doctor --fix",
      env: { DENEB_PROFILE: "Default" },
      expected: "deneb doctor --fix",
    },
    {
      name: "profile is invalid",
      cmd: "deneb doctor --fix",
      env: { DENEB_PROFILE: "bad profile" },
      expected: "deneb doctor --fix",
    },
    {
      name: "--profile is already present",
      cmd: "deneb --profile work doctor --fix",
      env: { DENEB_PROFILE: "work" },
      expected: "deneb --profile work doctor --fix",
    },
    {
      name: "--dev is already present",
      cmd: "deneb --dev doctor",
      env: { DENEB_PROFILE: "dev" },
      expected: "deneb --dev doctor",
    },
  ])("returns command unchanged when $name", ({ cmd, env, expected }) => {
    expect(formatCliCommand(cmd, env)).toBe(expected);
  });

  it("inserts --profile flag when profile is set", () => {
    expect(formatCliCommand("deneb doctor --fix", { DENEB_PROFILE: "work" })).toBe(
      "deneb --profile work doctor --fix",
    );
  });

  it("trims whitespace from profile", () => {
    expect(formatCliCommand("deneb doctor --fix", { DENEB_PROFILE: "  jbdeneb  " })).toBe(
      "deneb --profile jbdeneb doctor --fix",
    );
  });

  it("handles command with no args after deneb", () => {
    expect(formatCliCommand("deneb", { DENEB_PROFILE: "test" })).toBe(
      "deneb --profile test",
    );
  });

  it("handles pnpm wrapper", () => {
    expect(formatCliCommand("pnpm deneb doctor", { DENEB_PROFILE: "work" })).toBe(
      "pnpm deneb --profile work doctor",
    );
  });
});
