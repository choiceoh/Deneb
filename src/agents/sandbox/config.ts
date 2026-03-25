import type { DenebConfig } from "../../config/config.js";
import type { SandboxSshSettings } from "../../config/types.sandbox.js";
import { normalizeSecretInputString } from "../../config/types.secrets.js";
import { resolveAgentConfig } from "../agent-scope.js";
import {
  DEFAULT_SANDBOX_CONTAINER_PREFIX,
  DEFAULT_SANDBOX_IDLE_HOURS,
  DEFAULT_SANDBOX_IMAGE,
  DEFAULT_SANDBOX_MAX_AGE_DAYS,
  DEFAULT_SANDBOX_WORKDIR,
  DEFAULT_SANDBOX_WORKSPACE_ROOT,
} from "./constants.js";
import { resolveSandboxToolPolicyForAgent } from "./tool-policy.js";
import type {
  SandboxConfig,
  SandboxDockerConfig,
  SandboxPruneConfig,
  SandboxScope,
  SandboxSshConfig,
} from "./types.js";

export const DANGEROUS_SANDBOX_DOCKER_BOOLEAN_KEYS = [
  "dangerouslyAllowReservedContainerTargets",
  "dangerouslyAllowExternalBindSources",
  "dangerouslyAllowContainerNamespaceJoin",
] as const;

const DEFAULT_SANDBOX_SSH_COMMAND = "ssh";
const DEFAULT_SANDBOX_SSH_WORKSPACE_ROOT = "/tmp/deneb-sandboxes";

type DangerousSandboxDockerBooleanKey = (typeof DANGEROUS_SANDBOX_DOCKER_BOOLEAN_KEYS)[number];
type DangerousSandboxDockerBooleans = Pick<SandboxDockerConfig, DangerousSandboxDockerBooleanKey>;

function resolveDangerousSandboxDockerBooleans(
  agentDocker?: Partial<SandboxDockerConfig>,
  globalDocker?: Partial<SandboxDockerConfig>,
): DangerousSandboxDockerBooleans {
  const resolved = {} as DangerousSandboxDockerBooleans;
  for (const key of DANGEROUS_SANDBOX_DOCKER_BOOLEAN_KEYS) {
    resolved[key] = agentDocker?.[key] ?? globalDocker?.[key];
  }
  return resolved;
}

export function resolveSandboxScope(params: {
  scope?: SandboxScope;
  perSession?: boolean;
}): SandboxScope {
  if (params.scope) {
    return params.scope;
  }
  if (typeof params.perSession === "boolean") {
    return params.perSession ? "session" : "shared";
  }
  return "agent";
}

export function resolveSandboxDockerConfig(params: {
  scope: SandboxScope;
  globalDocker?: Partial<SandboxDockerConfig>;
  agentDocker?: Partial<SandboxDockerConfig>;
}): SandboxDockerConfig {
  const agentDocker = params.scope === "shared" ? undefined : params.agentDocker;
  const globalDocker = params.globalDocker;

  const env = agentDocker?.env
    ? { ...(globalDocker?.env ?? { LANG: "C.UTF-8" }), ...agentDocker.env }
    : (globalDocker?.env ?? { LANG: "C.UTF-8" });

  const ulimits = agentDocker?.ulimits
    ? { ...globalDocker?.ulimits, ...agentDocker.ulimits }
    : globalDocker?.ulimits;

  const binds = [...(globalDocker?.binds ?? []), ...(agentDocker?.binds ?? [])];

  return {
    image: agentDocker?.image ?? globalDocker?.image ?? DEFAULT_SANDBOX_IMAGE,
    containerPrefix:
      agentDocker?.containerPrefix ??
      globalDocker?.containerPrefix ??
      DEFAULT_SANDBOX_CONTAINER_PREFIX,
    workdir: agentDocker?.workdir ?? globalDocker?.workdir ?? DEFAULT_SANDBOX_WORKDIR,
    readOnlyRoot: agentDocker?.readOnlyRoot ?? globalDocker?.readOnlyRoot ?? true,
    tmpfs: agentDocker?.tmpfs ?? globalDocker?.tmpfs ?? ["/tmp", "/var/tmp", "/run"],
    network: agentDocker?.network ?? globalDocker?.network ?? "none",
    user: agentDocker?.user ?? globalDocker?.user,
    capDrop: agentDocker?.capDrop ?? globalDocker?.capDrop ?? ["ALL"],
    env,
    setupCommand: agentDocker?.setupCommand ?? globalDocker?.setupCommand,
    pidsLimit: agentDocker?.pidsLimit ?? globalDocker?.pidsLimit,
    memory: agentDocker?.memory ?? globalDocker?.memory,
    memorySwap: agentDocker?.memorySwap ?? globalDocker?.memorySwap,
    cpus: agentDocker?.cpus ?? globalDocker?.cpus,
    ulimits,
    seccompProfile: agentDocker?.seccompProfile ?? globalDocker?.seccompProfile,
    apparmorProfile: agentDocker?.apparmorProfile ?? globalDocker?.apparmorProfile,
    dns: agentDocker?.dns ?? globalDocker?.dns,
    extraHosts: agentDocker?.extraHosts ?? globalDocker?.extraHosts,
    binds: binds.length ? binds : undefined,
    ...resolveDangerousSandboxDockerBooleans(agentDocker, globalDocker),
  };
}

export function resolveSandboxPruneConfig(params: {
  scope: SandboxScope;
  globalPrune?: Partial<SandboxPruneConfig>;
  agentPrune?: Partial<SandboxPruneConfig>;
}): SandboxPruneConfig {
  const agentPrune = params.scope === "shared" ? undefined : params.agentPrune;
  const globalPrune = params.globalPrune;
  return {
    idleHours: agentPrune?.idleHours ?? globalPrune?.idleHours ?? DEFAULT_SANDBOX_IDLE_HOURS,
    maxAgeDays: agentPrune?.maxAgeDays ?? globalPrune?.maxAgeDays ?? DEFAULT_SANDBOX_MAX_AGE_DAYS,
  };
}

function normalizeOptionalString(value: string | undefined): string | undefined {
  const trimmed = value?.trim();
  return trimmed ? trimmed : undefined;
}

function normalizeRemoteRoot(value: string | undefined, fallback: string): string {
  const normalized = normalizeOptionalString(value) ?? fallback;
  const posix = normalized.replaceAll("\\", "/");
  if (!posix.startsWith("/")) {
    throw new Error(`Sandbox SSH workspaceRoot must be an absolute POSIX path: ${normalized}`);
  }
  return posix.replace(/\/+$/g, "") || "/";
}

export function resolveSandboxSshConfig(params: {
  scope: SandboxScope;
  globalSsh?: Partial<SandboxSshSettings>;
  agentSsh?: Partial<SandboxSshSettings>;
}): SandboxSshConfig {
  const agentSsh = params.scope === "shared" ? undefined : params.agentSsh;
  const globalSsh = params.globalSsh;
  return {
    target: normalizeOptionalString(agentSsh?.target ?? globalSsh?.target),
    command:
      normalizeOptionalString(agentSsh?.command ?? globalSsh?.command) ??
      DEFAULT_SANDBOX_SSH_COMMAND,
    workspaceRoot: normalizeRemoteRoot(
      agentSsh?.workspaceRoot ?? globalSsh?.workspaceRoot,
      DEFAULT_SANDBOX_SSH_WORKSPACE_ROOT,
    ),
    strictHostKeyChecking:
      agentSsh?.strictHostKeyChecking ?? globalSsh?.strictHostKeyChecking ?? true,
    updateHostKeys: agentSsh?.updateHostKeys ?? globalSsh?.updateHostKeys ?? true,
    identityFile: normalizeOptionalString(agentSsh?.identityFile ?? globalSsh?.identityFile),
    certificateFile: normalizeOptionalString(
      agentSsh?.certificateFile ?? globalSsh?.certificateFile,
    ),
    knownHostsFile: normalizeOptionalString(agentSsh?.knownHostsFile ?? globalSsh?.knownHostsFile),
    identityData: normalizeSecretInputString(agentSsh?.identityData ?? globalSsh?.identityData),
    certificateData: normalizeSecretInputString(
      agentSsh?.certificateData ?? globalSsh?.certificateData,
    ),
    knownHostsData: normalizeSecretInputString(
      agentSsh?.knownHostsData ?? globalSsh?.knownHostsData,
    ),
  };
}

export function resolveSandboxConfigForAgent(cfg?: DenebConfig, agentId?: string): SandboxConfig {
  const agent = cfg?.agents?.defaults?.sandbox;

  // Agent-specific sandbox config overrides global
  let agentSandbox: typeof agent | undefined;
  const agentConfig = cfg && agentId ? resolveAgentConfig(cfg, agentId) : undefined;
  if (agentConfig?.sandbox) {
    agentSandbox = agentConfig.sandbox;
  }

  const scope = resolveSandboxScope({
    scope: agentSandbox?.scope ?? agent?.scope,
    perSession: agentSandbox?.perSession ?? agent?.perSession,
  });

  const toolPolicy = resolveSandboxToolPolicyForAgent(cfg, agentId);

  return {
    mode: agentSandbox?.mode ?? agent?.mode ?? "off",
    backend: agentSandbox?.backend?.trim() || agent?.backend?.trim() || "docker",
    scope,
    workspaceAccess: agentSandbox?.workspaceAccess ?? agent?.workspaceAccess ?? "none",
    workspaceRoot:
      agentSandbox?.workspaceRoot ?? agent?.workspaceRoot ?? DEFAULT_SANDBOX_WORKSPACE_ROOT,
    docker: resolveSandboxDockerConfig({
      scope,
      globalDocker: agent?.docker,
      agentDocker: agentSandbox?.docker,
    }),
    ssh: resolveSandboxSshConfig({
      scope,
      globalSsh: agent?.ssh,
      agentSsh: agentSandbox?.ssh,
    }),
    tools: {
      allow: toolPolicy.allow,
      deny: toolPolicy.deny,
    },
    prune: resolveSandboxPruneConfig({
      scope,
      globalPrune: agent?.prune,
      agentPrune: agentSandbox?.prune,
    }),
  };
}
