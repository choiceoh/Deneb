import os from "node:os";
import path from "node:path";
import { VERSION } from "../version.js";
import {
  GATEWAY_SERVICE_KIND,
  GATEWAY_SERVICE_MARKER,
  resolveGatewaySystemdServiceName,
} from "./constants.js";

export type MinimalServicePathOptions = {
  platform?: NodeJS.Platform;
  extraDirs?: string[];
  home?: string;
  env?: Record<string, string | undefined>;
};

type BuildServicePathOptions = MinimalServicePathOptions & {
  env?: Record<string, string | undefined>;
};

type SharedServiceEnvironmentFields = {
  stateDir: string | undefined;
  configPath: string | undefined;
  tmpDir: string;
  minimalPath: string | undefined;
  proxyEnv: Record<string, string | undefined>;
  nodeCaCerts: string | undefined;
  nodeUseSystemCa: string | undefined;
};

const SERVICE_PROXY_ENV_KEYS = [
  "HTTP_PROXY",
  "HTTPS_PROXY",
  "NO_PROXY",
  "ALL_PROXY",
  "http_proxy",
  "https_proxy",
  "no_proxy",
  "all_proxy",
] as const;

function readServiceProxyEnvironment(
  env: Record<string, string | undefined>,
): Record<string, string | undefined> {
  const out: Record<string, string | undefined> = {};
  for (const key of SERVICE_PROXY_ENV_KEYS) {
    const value = env[key];
    if (typeof value !== "string") {
      continue;
    }
    const trimmed = value.trim();
    if (!trimmed) {
      continue;
    }
    out[key] = trimmed;
  }
  return out;
}

function addNonEmptyDir(dirs: string[], dir: string | undefined): void {
  if (dir) {
    dirs.push(dir);
  }
}

function appendSubdir(base: string | undefined, subdir: string): string | undefined {
  if (!base) {
    return undefined;
  }
  return base.endsWith(`/${subdir}`) ? base : path.posix.join(base, subdir);
}

function addCommonUserBinDirs(dirs: string[], home: string): void {
  dirs.push(`${home}/.local/bin`);
  dirs.push(`${home}/.npm-global/bin`);
  dirs.push(`${home}/bin`);
  dirs.push(`${home}/.volta/bin`);
  dirs.push(`${home}/.asdf/shims`);
  dirs.push(`${home}/.bun/bin`);
}

function addCommonEnvConfiguredBinDirs(
  dirs: string[],
  env: Record<string, string | undefined> | undefined,
): void {
  addNonEmptyDir(dirs, env?.PNPM_HOME);
  addNonEmptyDir(dirs, appendSubdir(env?.NPM_CONFIG_PREFIX, "bin"));
  addNonEmptyDir(dirs, appendSubdir(env?.BUN_INSTALL, "bin"));
  addNonEmptyDir(dirs, appendSubdir(env?.VOLTA_HOME, "bin"));
  addNonEmptyDir(dirs, appendSubdir(env?.ASDF_DATA_DIR, "shims"));
}

function resolveSystemPathDirs(platform: NodeJS.Platform): string[] {
  if (platform === "linux") {
    return ["/usr/local/bin", "/usr/bin", "/bin"];
  }
  return [];
}

/**
 * Resolve common user bin directories for Linux.
 * These are paths where npm global installs and node version managers typically place binaries.
 */
export function resolveLinuxUserBinDirs(
  home: string | undefined,
  env?: Record<string, string | undefined>,
): string[] {
  if (!home) {
    return [];
  }

  const dirs: string[] = [];

  // Env-configured bin roots (override defaults when present).
  addCommonEnvConfiguredBinDirs(dirs, env);
  addNonEmptyDir(dirs, appendSubdir(env?.NVM_DIR, "current/bin"));
  addNonEmptyDir(dirs, appendSubdir(env?.FNM_DIR, "current/bin"));

  // Common user bin directories
  addCommonUserBinDirs(dirs, home);

  // Node version managers
  dirs.push(`${home}/.nvm/current/bin`); // nvm with current symlink
  dirs.push(`${home}/.fnm/current/bin`); // fnm
  dirs.push(`${home}/.local/share/pnpm`); // pnpm global bin

  return dirs;
}

export function getMinimalServicePathParts(options: MinimalServicePathOptions = {}): string[] {
  const platform = options.platform ?? process.platform;
  const parts: string[] = [];
  const extraDirs = options.extraDirs ?? [];
  const systemDirs = resolveSystemPathDirs(platform);

  // Add user bin directories for version managers (npm global, nvm, fnm, volta, etc.)
  const userDirs = platform === "linux" ? resolveLinuxUserBinDirs(options.home, options.env) : [];

  const add = (dir: string) => {
    if (!dir) {
      return;
    }
    if (!parts.includes(dir)) {
      parts.push(dir);
    }
  };

  for (const dir of extraDirs) {
    add(dir);
  }
  // User dirs first so user-installed binaries take precedence
  for (const dir of userDirs) {
    add(dir);
  }
  for (const dir of systemDirs) {
    add(dir);
  }

  return parts;
}

export function getMinimalServicePathPartsFromEnv(options: BuildServicePathOptions = {}): string[] {
  const env = options.env ?? process.env;
  return getMinimalServicePathParts({
    ...options,
    home: options.home ?? env.HOME,
    env,
  });
}

export function buildMinimalServicePath(options: BuildServicePathOptions = {}): string {
  const env = options.env ?? process.env;
  return getMinimalServicePathPartsFromEnv({ ...options, env }).join(path.posix.delimiter);
}

export function buildServiceEnvironment(params: {
  env: Record<string, string | undefined>;
  port: number;
  platform?: NodeJS.Platform;
  extraPathDirs?: string[];
}): Record<string, string | undefined> {
  const { env, port, extraPathDirs } = params;
  const platform = params.platform ?? process.platform;
  const sharedEnv = resolveSharedServiceEnvironmentFields(env, platform, extraPathDirs);
  const profile = env.DENEB_PROFILE;
  const systemdUnit = `${resolveGatewaySystemdServiceName(profile)}.service`;
  return {
    ...buildCommonServiceEnvironment(env, sharedEnv),
    DENEB_PROFILE: profile,
    DENEB_GATEWAY_PORT: String(port),
    DENEB_SYSTEMD_UNIT: systemdUnit,
    DENEB_SERVICE_MARKER: GATEWAY_SERVICE_MARKER,
    DENEB_SERVICE_KIND: GATEWAY_SERVICE_KIND,
    DENEB_SERVICE_VERSION: VERSION,
  };
}

function buildCommonServiceEnvironment(
  env: Record<string, string | undefined>,
  sharedEnv: SharedServiceEnvironmentFields,
): Record<string, string | undefined> {
  const serviceEnv: Record<string, string | undefined> = {
    TMPDIR: sharedEnv.tmpDir,
    ...sharedEnv.proxyEnv,
    NODE_EXTRA_CA_CERTS: sharedEnv.nodeCaCerts,
    NODE_USE_SYSTEM_CA: sharedEnv.nodeUseSystemCa,
    DENEB_STATE_DIR: sharedEnv.stateDir,
    DENEB_CONFIG_PATH: sharedEnv.configPath,
  };
  if (sharedEnv.minimalPath) {
    serviceEnv.PATH = sharedEnv.minimalPath;
  }
  return serviceEnv;
}

function resolveSharedServiceEnvironmentFields(
  env: Record<string, string | undefined>,
  platform: NodeJS.Platform,
  extraPathDirs: string[] | undefined,
): SharedServiceEnvironmentFields {
  const stateDir = env.DENEB_STATE_DIR;
  const configPath = env.DENEB_CONFIG_PATH;
  // Keep a usable temp directory for supervised services even when the host env omits TMPDIR.
  const tmpDir = env.TMPDIR?.trim() || os.tmpdir();
  const proxyEnv = readServiceProxyEnvironment(env);
  const nodeCaCerts = env.NODE_EXTRA_CA_CERTS;
  const nodeUseSystemCa = env.NODE_USE_SYSTEM_CA;
  return {
    stateDir,
    configPath,
    tmpDir,
    minimalPath: buildMinimalServicePath({ env, platform, extraDirs: extraPathDirs }),
    proxyEnv,
    nodeCaCerts,
    nodeUseSystemCa,
  };
}
