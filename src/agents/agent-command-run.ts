import { type ThinkLevel, type VerboseLevel } from "../auto-reply/thinking.js";
import { loadConfig } from "../config/config.js";
import { type SessionEntry } from "../config/sessions.js";
import { createSubsystemLogger } from "../logging/subsystem.js";
import { sanitizeForLog } from "../terminal/ansi.js";
import { resolveMessageChannel } from "../utils/message-channel.js";
import { persistSessionEntry, resolveFallbackRetryPrompt } from "./agent-command-helpers.js";
import { resolveBootstrapWarningSignaturesSeen } from "./bootstrap-budget.js";
import { runCliAgent } from "./cli-runner.js";
import { getCliSessionId, setCliSessionId } from "./cli-session.js";
import { resolveAgentRunContext } from "./command/run-context.js";
import type { AgentCommandOpts } from "./command/types.js";
import { FailoverError } from "./failover-error.js";
import { isCliProvider, normalizeProviderId } from "./models/model-selection.js";
import { runEmbeddedPiAgent } from "./pi-embedded.js";
import { buildWorkspaceSkillSnapshot } from "./skills.js";

const log = createSubsystemLogger("agents/agent-command");

export function runAgentAttempt(params: {
  providerOverride: string;
  modelOverride: string;
  cfg: ReturnType<typeof loadConfig>;
  sessionEntry: SessionEntry | undefined;
  sessionId: string;
  sessionKey: string | undefined;
  sessionAgentId: string;
  sessionFile: string;
  workspaceDir: string;
  body: string;
  isFallbackRetry: boolean;
  resolvedThinkLevel: ThinkLevel;
  timeoutMs: number;
  runId: string;
  opts: AgentCommandOpts & { senderIsOwner: boolean };
  runContext: ReturnType<typeof resolveAgentRunContext>;
  spawnedBy: string | undefined;
  messageChannel: ReturnType<typeof resolveMessageChannel>;
  skillsSnapshot: ReturnType<typeof buildWorkspaceSkillSnapshot> | undefined;
  resolvedVerboseLevel: VerboseLevel | undefined;
  agentDir: string;
  onAgentEvent: (evt: { stream: string; data?: Record<string, unknown> }) => void;
  authProfileProvider: string;
  sessionStore?: Record<string, SessionEntry>;
  storePath?: string;
  allowTransientCooldownProbe?: boolean;
}) {
  const effectivePrompt = resolveFallbackRetryPrompt({
    body: params.body,
    isFallbackRetry: params.isFallbackRetry,
  });
  const bootstrapPromptWarningSignaturesSeen = resolveBootstrapWarningSignaturesSeen(
    params.sessionEntry?.systemPromptReport,
  );
  const bootstrapPromptWarningSignature =
    bootstrapPromptWarningSignaturesSeen[bootstrapPromptWarningSignaturesSeen.length - 1];
  if (isCliProvider(params.providerOverride, params.cfg)) {
    const cliSessionId = getCliSessionId(params.sessionEntry, params.providerOverride);
    const runCliWithSession = (nextCliSessionId: string | undefined) =>
      runCliAgent({
        sessionId: params.sessionId,
        sessionKey: params.sessionKey,
        agentId: params.sessionAgentId,
        sessionFile: params.sessionFile,
        workspaceDir: params.workspaceDir,
        config: params.cfg,
        prompt: effectivePrompt,
        provider: params.providerOverride,
        model: params.modelOverride,
        thinkLevel: params.resolvedThinkLevel,
        timeoutMs: params.timeoutMs,
        runId: params.runId,
        extraSystemPrompt: params.opts.extraSystemPrompt,
        cliSessionId: nextCliSessionId,
        bootstrapPromptWarningSignaturesSeen,
        bootstrapPromptWarningSignature,
        images: params.isFallbackRetry ? undefined : params.opts.images,
        streamParams: params.opts.streamParams,
      });
    return runCliWithSession(cliSessionId).catch(async (err) => {
      // Handle CLI session expired error
      if (
        err instanceof FailoverError &&
        err.reason === "session_expired" &&
        cliSessionId &&
        params.sessionKey &&
        params.sessionStore &&
        params.storePath
      ) {
        log.warn(
          `CLI session expired, clearing from session store: provider=${sanitizeForLog(params.providerOverride)} sessionKey=${params.sessionKey}`,
        );

        // Clear the expired session ID from the session store
        const entry = params.sessionStore[params.sessionKey];
        if (entry) {
          const updatedEntry = { ...entry };
          if (params.providerOverride === "claude-cli") {
            delete updatedEntry.claudeCliSessionId;
          }
          if (updatedEntry.cliSessionIds) {
            const normalizedProvider = normalizeProviderId(params.providerOverride);
            const newCliSessionIds = { ...updatedEntry.cliSessionIds };
            delete newCliSessionIds[normalizedProvider];
            updatedEntry.cliSessionIds = newCliSessionIds;
          }
          updatedEntry.updatedAt = Date.now();

          await persistSessionEntry({
            sessionStore: params.sessionStore,
            sessionKey: params.sessionKey,
            storePath: params.storePath,
            entry: updatedEntry,
          });

          // Update the session entry reference
          params.sessionEntry = updatedEntry;
        }

        // Retry with no session ID (will create a new session)
        return runCliWithSession(undefined).then(async (result) => {
          // Update session store with new CLI session ID if available
          if (
            result.meta.agentMeta?.sessionId &&
            params.sessionKey &&
            params.sessionStore &&
            params.storePath
          ) {
            const entry = params.sessionStore[params.sessionKey];
            if (entry) {
              const updatedEntry = { ...entry };
              setCliSessionId(
                updatedEntry,
                params.providerOverride,
                result.meta.agentMeta.sessionId,
              );
              updatedEntry.updatedAt = Date.now();

              await persistSessionEntry({
                sessionStore: params.sessionStore,
                sessionKey: params.sessionKey,
                storePath: params.storePath,
                entry: updatedEntry,
              });
            }
          }
          return result;
        });
      }
      throw err;
    });
  }

  const authProfileId =
    params.providerOverride === params.authProfileProvider
      ? params.sessionEntry?.authProfileOverride
      : undefined;
  return runEmbeddedPiAgent({
    sessionId: params.sessionId,
    sessionKey: params.sessionKey,
    agentId: params.sessionAgentId,
    trigger: "user",
    messageChannel: params.messageChannel,
    agentAccountId: params.runContext.accountId,
    messageTo: params.opts.replyTo ?? params.opts.to,
    messageThreadId: params.opts.threadId,
    groupId: params.runContext.groupId,
    groupChannel: params.runContext.groupChannel,
    groupSpace: params.runContext.groupSpace,
    spawnedBy: params.spawnedBy,
    currentChannelId: params.runContext.currentChannelId,
    currentThreadTs: params.runContext.currentThreadTs,
    replyToMode: params.runContext.replyToMode,
    hasRepliedRef: params.runContext.hasRepliedRef,
    senderIsOwner: params.opts.senderIsOwner,
    sessionFile: params.sessionFile,
    workspaceDir: params.workspaceDir,
    config: params.cfg,
    skillsSnapshot: params.skillsSnapshot,
    prompt: effectivePrompt,
    images: params.isFallbackRetry ? undefined : params.opts.images,
    clientTools: params.opts.clientTools,
    provider: params.providerOverride,
    model: params.modelOverride,
    authProfileId,
    authProfileIdSource: authProfileId ? params.sessionEntry?.authProfileOverrideSource : undefined,
    thinkLevel: params.resolvedThinkLevel,
    verboseLevel: params.resolvedVerboseLevel,
    timeoutMs: params.timeoutMs,
    runId: params.runId,
    lane: params.opts.lane,
    abortSignal: params.opts.abortSignal,
    extraSystemPrompt: params.opts.extraSystemPrompt,
    inputProvenance: params.opts.inputProvenance,
    streamParams: params.opts.streamParams,
    agentDir: params.agentDir,
    allowTransientCooldownProbe: params.allowTransientCooldownProbe,
    onAgentEvent: params.onAgentEvent,
    bootstrapPromptWarningSignaturesSeen,
    bootstrapPromptWarningSignature,
  });
}
