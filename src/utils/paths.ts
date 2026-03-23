import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import {
  resolveEffectiveHomeDir,
  resolveHomeRelativePath,
  resolveRequiredHomeDir,
} from "../infra/home-dir.js";

export function resolveUserPath(
  input: string,
  env: NodeJS.ProcessEnv = process.env,
  homedir: () => string = os.homedir,
): string {
  if (!input) {
    return "";
  }
  return resolveHomeRelativePath(input, { env, homedir });
}

export function resolveConfigDir(
  env: NodeJS.ProcessEnv = process.env,
  homedir: () => string = os.homedir,
): string {
  const override = env.DENEB_STATE_DIR?.trim() || env.CLAWDBOT_STATE_DIR?.trim();
  if (override) {
    return resolveUserPath(override, env, homedir);
  }
  const newDir = path.join(resolveRequiredHomeDir(env, homedir), ".deneb");
  try {
    const hasNew = fs.existsSync(newDir);
    if (hasNew) {
      return newDir;
    }
  } catch {
    // best-effort
  }
  return newDir;
}

export function resolveHomeDir(): string | undefined {
  return resolveEffectiveHomeDir(process.env, os.homedir);
}

function resolveHomeDisplayPrefix(): { home: string; prefix: string } | undefined {
  const home = resolveHomeDir();
  if (!home) {
    return undefined;
  }
  const explicitHome = process.env.DENEB_HOME?.trim();
  if (explicitHome) {
    return { home, prefix: "$DENEB_HOME" };
  }
  return { home, prefix: "~" };
}

export function shortenHomePath(input: string): string {
  if (!input) {
    return input;
  }
  const display = resolveHomeDisplayPrefix();
  if (!display) {
    return input;
  }
  const { home, prefix } = display;
  if (input === home) {
    return prefix;
  }
  if (input.startsWith(`${home}/`) || input.startsWith(`${home}\\`)) {
    return `${prefix}${input.slice(home.length)}`;
  }
  return input;
}

export function shortenHomeInString(input: string): string {
  if (!input) {
    return input;
  }
  const display = resolveHomeDisplayPrefix();
  if (!display) {
    return input;
  }
  return input.split(display.home).join(display.prefix);
}

export function displayPath(input: string): string {
  return shortenHomePath(input);
}

export function displayString(input: string): string {
  return shortenHomeInString(input);
}

// Configuration root; can be overridden via DENEB_STATE_DIR.
export const CONFIG_DIR = resolveConfigDir();
