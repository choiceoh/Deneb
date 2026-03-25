import fs from "node:fs/promises";
import type { DenebConfig } from "../../config/config.js";
import { defaultRuntime } from "../../runtime.js";
import { resolveUserPath } from "../../utils.js";
import { syncSkillsToWorkspace } from "../skills.js";
import { DEFAULT_AGENT_WORKSPACE_DIR } from "../workspace/workspace.js";
import { requireSandboxBackendFactory } from "./backend.js";
import { resolveSandboxConfigForAgent } from "./config.js";
import { createSandboxFsBridge } from "./fs-bridge.js";
import { maybePruneSandboxes } from "./prune.js";
import { updateRegistry } from "./registry.js";
import { resolveSandboxRuntimeStatus } from "./runtime-status.js";
import { resolveSandboxScopeKey, resolveSandboxWorkspaceDir } from "./shared.js";
import type { SandboxContext, SandboxDockerConfig, SandboxWorkspaceInfo } from "./types.js";
import { ensureSandboxWorkspace } from "./workspace.js";

async function ensureSandboxWorkspaceLayout(params: {
  cfg: ReturnType<typeof resolveSandboxConfigForAgent>;
  rawSessionKey: string;
  config?: DenebConfig;
  workspaceDir?: string;
}): Promise<{
  agentWorkspaceDir: string;
  scopeKey: string;
  sandboxWorkspaceDir: string;
  workspaceDir: string;
}> {
  const { cfg, rawSessionKey } = params;

  const agentWorkspaceDir = resolveUserPath(
    params.workspaceDir?.trim() || DEFAULT_AGENT_WORKSPACE_DIR,
  );
  const workspaceRoot = resolveUserPath(cfg.workspaceRoot);
  const scopeKey = resolveSandboxScopeKey(cfg.scope, rawSessionKey);
  const sandboxWorkspaceDir =
    cfg.scope === "shared" ? workspaceRoot : resolveSandboxWorkspaceDir(workspaceRoot, scopeKey);
  const workspaceDir = cfg.workspaceAccess === "rw" ? agentWorkspaceDir : sandboxWorkspaceDir;

  if (workspaceDir === sandboxWorkspaceDir) {
    await ensureSandboxWorkspace(
      sandboxWorkspaceDir,
      agentWorkspaceDir,
      params.config?.agents?.defaults?.skipBootstrap,
    );
    if (cfg.workspaceAccess !== "rw") {
      try {
        await syncSkillsToWorkspace({
          sourceWorkspaceDir: agentWorkspaceDir,
          targetWorkspaceDir: sandboxWorkspaceDir,
          config: params.config,
        });
      } catch (error) {
        const message = error instanceof Error ? error.message : JSON.stringify(error);
        defaultRuntime.error?.(`Sandbox skill sync failed: ${message}`);
      }
    }
  } else {
    await fs.mkdir(workspaceDir, { recursive: true });
  }

  return { agentWorkspaceDir, scopeKey, sandboxWorkspaceDir, workspaceDir };
}

export async function resolveSandboxDockerUser(params: {
  docker: SandboxDockerConfig;
  workspaceDir: string;
  stat?: (workspaceDir: string) => Promise<{ uid: number; gid: number }>;
}): Promise<SandboxDockerConfig> {
  const configuredUser = params.docker.user?.trim();
  if (configuredUser) {
    return params.docker;
  }
  const stat = params.stat ?? ((workspaceDir: string) => fs.stat(workspaceDir));
  try {
    const workspaceStat = await stat(params.workspaceDir);
    const uid = Number.isInteger(workspaceStat.uid) ? workspaceStat.uid : null;
    const gid = Number.isInteger(workspaceStat.gid) ? workspaceStat.gid : null;
    if (uid === null || gid === null || uid < 0 || gid < 0) {
      return params.docker;
    }
    return { ...params.docker, user: `${uid}:${gid}` };
  } catch {
    return params.docker;
  }
}

function resolveSandboxSession(params: { config?: DenebConfig; sessionKey?: string }) {
  const rawSessionKey = params.sessionKey?.trim();
  if (!rawSessionKey) {
    return null;
  }

  const runtime = resolveSandboxRuntimeStatus({
    cfg: params.config,
    sessionKey: rawSessionKey,
  });
  if (!runtime.sandboxed) {
    return null;
  }

  const cfg = resolveSandboxConfigForAgent(params.config, runtime.agentId);
  return { rawSessionKey, runtime, cfg };
}

export async function resolveSandboxContext(params: {
  config?: DenebConfig;
  sessionKey?: string;
  workspaceDir?: string;
}): Promise<SandboxContext | null> {
  const resolved = resolveSandboxSession(params);
  if (!resolved) {
    return null;
  }
  const { rawSessionKey, cfg } = resolved;

  await maybePruneSandboxes(cfg);

  const { agentWorkspaceDir, scopeKey, workspaceDir } = await ensureSandboxWorkspaceLayout({
    cfg,
    rawSessionKey,
    config: params.config,
    workspaceDir: params.workspaceDir,
  });

  const docker = await resolveSandboxDockerUser({
    docker: cfg.docker,
    workspaceDir,
  });
  const resolvedCfg = docker === cfg.docker ? cfg : { ...cfg, docker };

  const backendFactory = requireSandboxBackendFactory(resolvedCfg.backend);
  const backend = await backendFactory({
    sessionKey: rawSessionKey,
    scopeKey,
    workspaceDir,
    agentWorkspaceDir,
    cfg: resolvedCfg,
  });
  await updateRegistry({
    containerName: backend.runtimeId,
    backendId: backend.id,
    runtimeLabel: backend.runtimeLabel,
    sessionKey: scopeKey,
    createdAtMs: Date.now(),
    lastUsedAtMs: Date.now(),
    image: backend.configLabel ?? resolvedCfg.docker.image,
    configLabelKind: backend.configLabelKind ?? "Image",
  });

  const sandboxContext: SandboxContext = {
    enabled: true,
    backendId: backend.id,
    sessionKey: rawSessionKey,
    workspaceDir,
    agentWorkspaceDir,
    workspaceAccess: resolvedCfg.workspaceAccess,
    runtimeId: backend.runtimeId,
    runtimeLabel: backend.runtimeLabel,
    containerName: backend.runtimeId,
    containerWorkdir: backend.workdir,
    docker: resolvedCfg.docker,
    tools: resolvedCfg.tools,
    browserAllowHostControl: resolvedCfg.browser.allowHostControl,
    browser: undefined,
    backend,
  };

  sandboxContext.fsBridge =
    backend.createFsBridge?.({ sandbox: sandboxContext }) ??
    createSandboxFsBridge({ sandbox: sandboxContext });

  return sandboxContext;
}

export async function ensureSandboxWorkspaceForSession(params: {
  config?: DenebConfig;
  sessionKey?: string;
  workspaceDir?: string;
}): Promise<SandboxWorkspaceInfo | null> {
  const resolved = resolveSandboxSession(params);
  if (!resolved) {
    return null;
  }
  const { rawSessionKey, cfg } = resolved;

  const { workspaceDir } = await ensureSandboxWorkspaceLayout({
    cfg,
    rawSessionKey,
    config: params.config,
    workspaceDir: params.workspaceDir,
  });

  return {
    workspaceDir,
    containerWorkdir: cfg.docker.workdir,
  };
}
