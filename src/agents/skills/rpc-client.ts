/**
 * Thin RPC client that calls the Go gateway for skill operations.
 *
 * This replaces the local TypeScript skill loading/filtering/prompt building
 * with Go gateway RPC calls. The Go gateway handles discovery, eligibility
 * evaluation, prompt building, and installation.
 *
 * @module
 */

import type { SkillCommandSpec, SkillSnapshot } from "./types.js";

// Re-export types that consumers need.
export type { SkillCommandSpec, SkillSnapshot } from "./types.js";

/**
 * Options for loading a skill snapshot from the Go gateway.
 */
export type SkillSnapshotOptions = {
  workspaceDir: string;
  bundledSkillsDir?: string;
  managedSkillsDir?: string;
  extraDirs?: string[];
  pluginSkillDirs?: string[];
  skillFilter?: string[];
  skillConfigs?: Record<string, { enabled?: boolean; apiKey?: string; env?: Record<string, string> }>;
  allowBundled?: string[];
  configValues?: Record<string, boolean>;
  envVars?: Record<string, string>;
  remoteNote?: string;
};

/**
 * Options for loading skill command specs from the Go gateway.
 */
export type SkillCommandsOptions = {
  workspaceDir: string;
  bundledSkillsDir?: string;
  extraDirs?: string[];
  pluginSkillDirs?: string[];
  skillFilter?: string[];
  skillConfigs?: Record<string, { enabled?: boolean; apiKey?: string; env?: Record<string, string> }>;
  allowBundled?: string[];
  reservedNames?: string[];
};

/**
 * Skill install request for the Go gateway.
 */
export type SkillInstallRequest = {
  workspaceDir: string;
  skillName: string;
  installId: string;
  timeoutMs?: number;
};

/**
 * Skill install result from the Go gateway.
 */
export type SkillInstallResult = {
  ok: boolean;
  message: string;
  stdout?: string;
  stderr?: string;
  code?: number | null;
  warnings?: string[];
};

/**
 * Skill status entry from the Go gateway.
 */
export type SkillStatusEntry = {
  name: string;
  source: string;
  eligible: boolean;
  emoji?: string;
  description?: string;
  primaryEnv?: string;
};

/**
 * Full skill status report from the Go gateway.
 */
export type SkillStatusReport = {
  skills: SkillStatusEntry[];
  requiredBins?: string[];
  totalCount: number;
  eligibleCount: number;
};

/**
 * Send an RPC request to the Go gateway.
 * This is a placeholder that should be wired to the actual gateway RPC transport.
 */
type RpcCaller = (method: string, params: Record<string, unknown>) => Promise<unknown>;

let rpcCaller: RpcCaller | null = null;

/**
 * Set the RPC caller function. Must be called during gateway initialization
 * to wire up the Go gateway RPC transport.
 */
export function setSkillRpcCaller(caller: RpcCaller): void {
  rpcCaller = caller;
}

async function callRpc<T>(method: string, params: Record<string, unknown>): Promise<T> {
  if (!rpcCaller) {
    throw new Error(
      `Skill RPC caller not initialized. Call setSkillRpcCaller() during gateway startup.`,
    );
  }
  const result = await rpcCaller(method, params);
  return result as T;
}

/**
 * Load a skill snapshot from the Go gateway.
 * Returns the complete snapshot with prompt, skills metadata, and version.
 */
export async function loadSkillSnapshot(opts: SkillSnapshotOptions): Promise<SkillSnapshot> {
  return callRpc<SkillSnapshot>("skills.snapshot", opts as unknown as Record<string, unknown>);
}

/**
 * Load skill command specs from the Go gateway.
 * Returns slash command specifications for eligible skills.
 */
export async function loadSkillCommands(
  opts: SkillCommandsOptions,
): Promise<SkillCommandSpec[]> {
  const result = await callRpc<{ commands: SkillCommandSpec[] }>(
    "skills.commands",
    opts as unknown as Record<string, unknown>,
  );
  return result.commands ?? [];
}

/**
 * Install a skill dependency via the Go gateway.
 */
export async function installSkillViaRpc(req: SkillInstallRequest): Promise<SkillInstallResult> {
  return callRpc<SkillInstallResult>("skills.install", req as unknown as Record<string, unknown>);
}

/**
 * Get the skill status report from the Go gateway.
 */
export async function getSkillStatus(
  workspaceDir: string,
  opts?: {
    bundledSkillsDir?: string;
    extraDirs?: string[];
    skillConfigs?: Record<string, { enabled?: boolean; apiKey?: string; env?: Record<string, string> }>;
    allowBundled?: string[];
  },
): Promise<SkillStatusReport> {
  return callRpc<SkillStatusReport>("skills.workspace_status", {
    workspaceDir,
    ...opts,
  });
}

/**
 * Trigger skill re-discovery on the Go gateway.
 */
export async function discoverSkills(
  workspaceDir: string,
  opts?: {
    bundledSkillsDir?: string;
    extraDirs?: string[];
    pluginSkillDirs?: string[];
  },
): Promise<{ ok: boolean; count: number }> {
  return callRpc<{ ok: boolean; count: number }>("skills.discover", {
    workspaceDir,
    ...opts,
  });
}
