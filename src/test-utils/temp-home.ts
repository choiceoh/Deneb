import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { captureEnv } from "./env.js";

// ── createTempHomeEnv (manual restore) ──────────────────────────────────────

const HOME_ENV_KEYS = ["HOME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "DENEB_STATE_DIR"] as const;

export type TempHomeEnv = {
  home: string;
  restore: () => Promise<void>;
};

/**
 * Create an isolated temp HOME directory and redirect HOME/USERPROFILE env vars to it.
 * Returns a handle with `home` path and `restore()` for manual cleanup.
 * Use when you need the HOME to persist across multiple async steps (e.g., beforeAll/afterAll).
 * For simpler per-test isolation, prefer `withTempHome()`.
 */
export async function createTempHomeEnv(prefix: string): Promise<TempHomeEnv> {
  const home = await fs.mkdtemp(path.join(os.tmpdir(), prefix));
  await fs.mkdir(path.join(home, ".deneb"), { recursive: true });

  const snapshot = captureEnv([...HOME_ENV_KEYS]);
  process.env.HOME = home;
  process.env.USERPROFILE = home;
  process.env.DENEB_STATE_DIR = path.join(home, ".deneb");

  if (process.platform === "win32") {
    const match = home.match(/^([A-Za-z]:)(.*)$/);
    if (match) {
      process.env.HOMEDRIVE = match[1];
      process.env.HOMEPATH = match[2] || "\\";
    }
  }

  return {
    home,
    restore: async () => {
      snapshot.restore();
      await fs.rm(home, { recursive: true, force: true });
    },
  };
}

// ── withTempHome (callback-based, auto-cleanup) ─────────────────────────────

type EnvValue = string | undefined | ((home: string) => string | undefined);

type EnvSnapshot = {
  home: string | undefined;
  userProfile: string | undefined;
  homeDrive: string | undefined;
  homePath: string | undefined;
  denebHome: string | undefined;
  stateDir: string | undefined;
};

type SharedHomeRootState = {
  rootPromise: Promise<string>;
  nextCaseId: number;
};

const SHARED_HOME_ROOTS = new Map<string, SharedHomeRootState>();

function snapshotEnv(): EnvSnapshot {
  return {
    home: process.env.HOME,
    userProfile: process.env.USERPROFILE,
    homeDrive: process.env.HOMEDRIVE,
    homePath: process.env.HOMEPATH,
    denebHome: process.env.DENEB_HOME,
    stateDir: process.env.DENEB_STATE_DIR,
  };
}

function restoreEnvSnapshot(snapshot: EnvSnapshot) {
  const restoreKey = (key: string, value: string | undefined) => {
    if (value === undefined) {
      delete process.env[key];
    } else {
      process.env[key] = value;
    }
  };
  restoreKey("HOME", snapshot.home);
  restoreKey("USERPROFILE", snapshot.userProfile);
  restoreKey("HOMEDRIVE", snapshot.homeDrive);
  restoreKey("HOMEPATH", snapshot.homePath);
  restoreKey("DENEB_HOME", snapshot.denebHome);
  restoreKey("DENEB_STATE_DIR", snapshot.stateDir);
}

function snapshotExtraEnv(keys: string[]): Record<string, string | undefined> {
  const snapshot: Record<string, string | undefined> = {};
  for (const key of keys) {
    snapshot[key] = process.env[key];
  }
  return snapshot;
}

function restoreExtraEnv(snapshot: Record<string, string | undefined>) {
  for (const [key, value] of Object.entries(snapshot)) {
    if (value === undefined) {
      delete process.env[key];
    } else {
      process.env[key] = value;
    }
  }
}

function setTempHome(base: string) {
  process.env.HOME = base;
  process.env.USERPROFILE = base;
  // Ensure tests using HOME isolation aren't affected by leaked DENEB_HOME.
  delete process.env.DENEB_HOME;
  process.env.DENEB_STATE_DIR = path.join(base, ".deneb");

  if (process.platform !== "win32") {
    return;
  }
  const match = base.match(/^([A-Za-z]:)(.*)$/);
  if (!match) {
    return;
  }
  process.env.HOMEDRIVE = match[1];
  process.env.HOMEPATH = match[2] || "\\";
}

async function allocateTempHomeBase(prefix: string): Promise<string> {
  let state = SHARED_HOME_ROOTS.get(prefix);
  if (!state) {
    state = {
      rootPromise: fs.mkdtemp(path.join(os.tmpdir(), prefix)),
      nextCaseId: 0,
    };
    SHARED_HOME_ROOTS.set(prefix, state);
  }
  const root = await state.rootPromise;
  const base = path.join(root, `case-${state.nextCaseId++}`);
  await fs.mkdir(base, { recursive: true });
  return base;
}

/**
 * Run an async callback with an isolated temp HOME directory.
 * HOME/USERPROFILE are set for the callback duration and restored after.
 * Preferred for most config/state tests that need filesystem isolation.
 * Pass `opts.env` to set additional env vars scoped to the callback.
 */
export async function withTempHome<T>(
  fn: (home: string) => Promise<T>,
  opts: { env?: Record<string, EnvValue>; prefix?: string } = {},
): Promise<T> {
  const prefix = opts.prefix ?? "deneb-test-home-";
  const base = await allocateTempHomeBase(prefix);
  const snapshot = snapshotEnv();
  const envKeys = Object.keys(opts.env ?? {});
  for (const key of envKeys) {
    if (key === "HOME" || key === "USERPROFILE" || key === "HOMEDRIVE" || key === "HOMEPATH") {
      throw new Error(`withTempHome: use built-in home env (got ${key})`);
    }
  }
  const envSnapshot = snapshotExtraEnv(envKeys);

  setTempHome(base);
  await fs.mkdir(path.join(base, ".deneb", "agents", "main", "sessions"), { recursive: true });
  if (opts.env) {
    for (const [key, raw] of Object.entries(opts.env)) {
      const value = typeof raw === "function" ? raw(base) : raw;
      if (value === undefined) {
        delete process.env[key];
      } else {
        process.env[key] = value;
      }
    }
  }

  try {
    return await fn(base);
  } finally {
    restoreExtraEnv(envSnapshot);
    restoreEnvSnapshot(snapshot);
    try {
      if (process.platform === "win32") {
        await fs.rm(base, {
          recursive: true,
          force: true,
          maxRetries: 10,
          retryDelay: 50,
        });
      } else {
        await fs.rm(base, {
          recursive: true,
          force: true,
        });
      }
    } catch {
      // ignore cleanup failures in tests
    }
  }
}
