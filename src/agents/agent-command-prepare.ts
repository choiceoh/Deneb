import { getAcpSessionManager } from "../acp/control-plane/manager.js";
import {
  formatThinkingLevels,
  normalizeThinkLevel,
  normalizeVerboseLevel,
} from "../auto-reply/thinking.js";
import { formatCliCommand } from "../cli/command-format.js";
import { resolveCommandSecretRefsViaGateway } from "../cli/command-secret-gateway.js";
import { getAgentRuntimeCommandSecretTargetIds } from "../cli/command-secret-targets.js";
import {
  loadConfig,
  readConfigFileSnapshotForWrite,
  setRuntimeConfigSnapshot,
} from "../config/config.js";
import { resolveAgentIdFromSessionKey } from "../config/sessions.js";
import { buildOutboundSessionContext } from "../infra/outbound/session-context.js";
import { normalizeAgentId } from "../routing/session-key.js";
import { type RuntimeEnv } from "../runtime.js";
import { prependInternalEventContext } from "./agent-command-helpers.js";
import {
  listAgentIds,
  resolveAgentDir,
  resolveSessionAgentId,
  resolveAgentWorkspaceDir,
} from "./agent-scope.js";
import { resolveSession } from "./command/session.js";
import type { AgentCommandOpts } from "./command/types.js";
import { DEFAULT_MODEL, DEFAULT_PROVIDER } from "./defaults.js";
import { AGENT_LANE_SUBAGENT } from "./lanes.js";
import { resolveConfiguredModelRef } from "./models/model-selection.js";
import { normalizeSpawnedRunMetadata } from "./spawned-context.js";
import { resolveAgentTimeoutMs } from "./timeout.js";
import { ensureAgentWorkspace } from "./workspace/workspace.js";

export async function prepareAgentCommandExecution(
  opts: AgentCommandOpts & { senderIsOwner: boolean },
  runtime: RuntimeEnv,
) {
  const message = opts.message ?? "";
  if (!message.trim()) {
    throw new Error("Message (--message) is required");
  }
  const body = prependInternalEventContext(message, opts.internalEvents);
  if (!opts.to && !opts.sessionId && !opts.sessionKey && !opts.agentId) {
    throw new Error("Pass --to <E.164>, --session-id, or --agent to choose a session");
  }

  const loadedRaw = loadConfig();
  const sourceConfig = await (async () => {
    try {
      const { snapshot } = await readConfigFileSnapshotForWrite();
      if (snapshot.valid) {
        return snapshot.resolved;
      }
    } catch {
      // Fall back to runtime-loaded config when source snapshot is unavailable.
    }
    return loadedRaw;
  })();
  const { resolvedConfig: cfg, diagnostics } = await resolveCommandSecretRefsViaGateway({
    config: loadedRaw,
    commandName: "agent",
    targetIds: getAgentRuntimeCommandSecretTargetIds(),
  });
  setRuntimeConfigSnapshot(cfg, sourceConfig);
  const normalizedSpawned = normalizeSpawnedRunMetadata({
    spawnedBy: opts.spawnedBy,
    groupId: opts.groupId,
    groupChannel: opts.groupChannel,
    groupSpace: opts.groupSpace,
    workspaceDir: opts.workspaceDir,
  });
  for (const entry of diagnostics) {
    runtime.log(`[secrets] ${entry}`);
  }
  const agentIdOverrideRaw = opts.agentId?.trim();
  const agentIdOverride = agentIdOverrideRaw ? normalizeAgentId(agentIdOverrideRaw) : undefined;
  if (agentIdOverride) {
    const knownAgents = listAgentIds(cfg);
    if (!knownAgents.includes(agentIdOverride)) {
      throw new Error(
        `Unknown agent id "${agentIdOverrideRaw}". Use "${formatCliCommand("deneb agents list")}" to see configured agents.`,
      );
    }
  }
  if (agentIdOverride && opts.sessionKey) {
    const sessionAgentId = resolveAgentIdFromSessionKey(opts.sessionKey);
    if (sessionAgentId !== agentIdOverride) {
      throw new Error(
        `Agent id "${agentIdOverrideRaw}" does not match session key agent "${sessionAgentId}".`,
      );
    }
  }
  const agentCfg = cfg.agents?.defaults;
  const configuredModel = resolveConfiguredModelRef({
    cfg,
    defaultProvider: DEFAULT_PROVIDER,
    defaultModel: DEFAULT_MODEL,
  });
  const thinkingLevelsHint = formatThinkingLevels(configuredModel.provider, configuredModel.model);

  const thinkOverride = normalizeThinkLevel(opts.thinking);
  const thinkOnce = normalizeThinkLevel(opts.thinkingOnce);
  if (opts.thinking && !thinkOverride) {
    throw new Error(`Invalid thinking level. Use one of: ${thinkingLevelsHint}.`);
  }
  if (opts.thinkingOnce && !thinkOnce) {
    throw new Error(`Invalid one-shot thinking level. Use one of: ${thinkingLevelsHint}.`);
  }

  const verboseOverride = normalizeVerboseLevel(opts.verbose);
  if (opts.verbose && !verboseOverride) {
    throw new Error('Invalid verbose level. Use "on", "full", or "off".');
  }

  const laneRaw = typeof opts.lane === "string" ? opts.lane.trim() : "";
  const isSubagentLane = laneRaw === String(AGENT_LANE_SUBAGENT);
  const timeoutSecondsRaw =
    opts.timeout !== undefined
      ? Number.parseInt(String(opts.timeout), 10)
      : isSubagentLane
        ? 0
        : undefined;
  if (
    timeoutSecondsRaw !== undefined &&
    (Number.isNaN(timeoutSecondsRaw) || timeoutSecondsRaw < 0)
  ) {
    throw new Error("--timeout must be a non-negative integer (seconds; 0 means no timeout)");
  }
  const timeoutMs = resolveAgentTimeoutMs({
    cfg,
    overrideSeconds: timeoutSecondsRaw,
  });

  const sessionResolution = resolveSession({
    cfg,
    to: opts.to,
    sessionId: opts.sessionId,
    sessionKey: opts.sessionKey,
    agentId: agentIdOverride,
  });

  const {
    sessionId,
    sessionKey,
    sessionEntry: sessionEntryRaw,
    sessionStore,
    storePath,
    isNewSession,
    persistedThinking,
    persistedVerbose,
  } = sessionResolution;
  const sessionAgentId =
    agentIdOverride ??
    resolveSessionAgentId({
      sessionKey: sessionKey ?? opts.sessionKey?.trim(),
      config: cfg,
    });
  const outboundSession = buildOutboundSessionContext({
    cfg,
    agentId: sessionAgentId,
    sessionKey,
  });
  // Internal callers (for example subagent spawns) may pin workspace inheritance.
  const workspaceDirRaw =
    normalizedSpawned.workspaceDir ?? resolveAgentWorkspaceDir(cfg, sessionAgentId);
  const agentDir = resolveAgentDir(cfg, sessionAgentId);
  const workspace = await ensureAgentWorkspace({
    dir: workspaceDirRaw,
    ensureBootstrapFiles: !agentCfg?.skipBootstrap,
  });
  const workspaceDir = workspace.dir;
  const runId = opts.runId?.trim() || sessionId;
  const acpManager = getAcpSessionManager();
  const acpResolution = sessionKey
    ? acpManager.resolveSession({
        cfg,
        sessionKey,
      })
    : null;

  return {
    body,
    cfg,
    normalizedSpawned,
    agentCfg,
    thinkOverride,
    thinkOnce,
    verboseOverride,
    timeoutMs,
    sessionId,
    sessionKey,
    sessionEntry: sessionEntryRaw,
    sessionStore,
    storePath,
    isNewSession,
    persistedThinking,
    persistedVerbose,
    sessionAgentId,
    outboundSession,
    workspaceDir,
    agentDir,
    runId,
    acpManager,
    acpResolution,
  };
}
