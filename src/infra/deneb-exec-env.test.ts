import { describe, expect, it } from "vitest";
import {
  ensureDenebExecMarkerOnProcess,
  markDenebExecEnv,
  DENEB_CLI_ENV_VALUE,
  DENEB_CLI_ENV_VAR,
} from "./deneb-exec-env.js";

describe("markDenebExecEnv", () => {
  it("returns a cloned env object with the exec marker set", () => {
    const env = { PATH: "/usr/bin", DENEB_CLI: "0" };
    const marked = markDenebExecEnv(env);

    expect(marked).toEqual({
      PATH: "/usr/bin",
      DENEB_CLI: DENEB_CLI_ENV_VALUE,
    });
    expect(marked).not.toBe(env);
    expect(env.DENEB_CLI).toBe("0");
  });
});

describe("ensureDenebExecMarkerOnProcess", () => {
  it("mutates and returns the provided process env", () => {
    const env: NodeJS.ProcessEnv = { PATH: "/usr/bin" };

    expect(ensureDenebExecMarkerOnProcess(env)).toBe(env);
    expect(env[DENEB_CLI_ENV_VAR]).toBe(DENEB_CLI_ENV_VALUE);
  });

  it("defaults to mutating process.env when no env object is provided", () => {
    const previous = process.env[DENEB_CLI_ENV_VAR];
    delete process.env[DENEB_CLI_ENV_VAR];

    try {
      expect(ensureDenebExecMarkerOnProcess()).toBe(process.env);
      expect(process.env[DENEB_CLI_ENV_VAR]).toBe(DENEB_CLI_ENV_VALUE);
    } finally {
      if (previous === undefined) {
        delete process.env[DENEB_CLI_ENV_VAR];
      } else {
        process.env[DENEB_CLI_ENV_VAR] = previous;
      }
    }
  });
});
