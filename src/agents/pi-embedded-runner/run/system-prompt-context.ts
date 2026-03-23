import os from "node:os";
import type { AgentTool } from "@mariozechner/pi-agent-core";
import {
  resolveTelegramInlineButtonsScope,
  resolveTelegramReactionLevel,
} from "deneb/plugin-sdk/telegram";
import { resolveHeartbeatPrompt } from "../../../auto-reply/heartbeat.js";
import type { ThinkLevel } from "../../../auto-reply/thinking.js";
import type { ReasoningLevel } from "../../../auto-reply/thinking.shared.js";
import { resolveChannelCapabilities } from "../../../config/channel-capabilities.js";
import type { DenebConfig } from "../../../config/config.js";
import type { SessionSystemPromptReport } from "../../../config/sessions/types.js";
import { buildTtsSystemPromptHint } from "../../../tts/tts.js";
import { normalizeMessageChannel } from "../../../utils/message-channel.js";
import { isReasoningTagProvider } from "../../../utils/provider-utils.js";
import type { ExecElevatedDefaults } from "../../bash-tools/bash-tools.exec-types.js";
import {
  analyzeBootstrapBudget,
  buildBootstrapPromptWarning,
  buildBootstrapTruncationReportMeta,
  buildBootstrapInjectionStats,
  type BootstrapPromptWarning,
} from "../../bootstrap-budget.js";
import {
  listChannelSupportedActions,
  resolveChannelMessageToolHints,
} from "../../channel-tools.js";
import { resolveDefaultModelForAgent } from "../../model-selection.js";
import { resolveOwnerDisplaySetting } from "../../owner-display.js";
import {
  resolveBootstrapMaxChars,
  resolveBootstrapPromptTruncationWarningMode,
  resolveBootstrapTotalMaxChars,
} from "../../pi-embedded-helpers.js";
import type { EmbeddedContextFile } from "../../pi-embedded-helpers/types.js";
import { resolveSandboxRuntimeStatus } from "../../sandbox/runtime-status.js";
import type { SandboxContext } from "../../sandbox/types.js";
import { detectRuntimeShell } from "../../shell-utils.js";
import { buildSystemPromptParams } from "../../system-prompt/system-prompt-params.js";
import { buildSystemPromptReport } from "../../system-prompt/system-prompt-report.js";
import type { WorkspaceBootstrapFile } from "../../workspace/workspace.js";
import { DEFAULT_BOOTSTRAP_FILENAME } from "../../workspace/workspace.js";
import { buildEmbeddedMessageActionDiscoveryInput } from "../message-action-discovery-input.js";
import { buildModelAliasLines } from "../model.js";
import { buildEmbeddedSandboxInfo } from "../sandbox-info.js";
import { buildEmbeddedSystemPrompt, createSystemPromptOverride } from "../system-prompt.js";
import { resolvePromptModeForSession } from "./prompt-hooks.js";

export type SystemPromptContextInput = {
  config: DenebConfig | undefined;
  provider: string;
  modelId: string;
  thinkLevel: ThinkLevel;
  reasoningLevel?: ReasoningLevel;
  extraSystemPrompt?: string;
  ownerNumbers?: string[];
  sessionKey?: string;
  sessionId: string;
  agentAccountId?: string | null;
  messageChannel?: string;
  messageProvider?: string;
  currentChannelId?: string;
  currentThreadTs?: string;
  currentMessageId?: string | number;
  senderId?: string | null;
  bashElevated?: ExecElevatedDefaults;
  bootstrapPromptWarningSignaturesSeen?: string[];
  bootstrapPromptWarningSignature?: string;

  machineName: string;
  sessionAgentId: string;
  defaultAgentId: string;
  effectiveWorkspace: string;
  sandboxSessionKey: string;
  sandbox: SandboxContext | null | undefined;
  effectiveTools: AgentTool[];
  skillsPrompt?: string;
  bootstrapFiles: WorkspaceBootstrapFile[];
  contextFiles: EmbeddedContextFile[];
  docsPath: string | null;
};

export type SystemPromptContextResult = {
  systemPromptText: string;
  systemPromptReport: SessionSystemPromptReport;
  bootstrapPromptWarning: BootstrapPromptWarning;
  heartbeatPrompt: string | undefined;
  promptMode: "minimal" | "full";
};

/**
 * Resolves all channel-specific context (capabilities, reaction guidance,
 * message tool hints, channel actions), builds the system prompt, and
 * generates the system prompt report.
 */
export function resolveSystemPromptContext(
  input: SystemPromptContextInput,
): SystemPromptContextResult {
  const runtimeChannel = normalizeMessageChannel(input.messageChannel ?? input.messageProvider);

  // Resolve channel capabilities with Telegram inline buttons extension
  let runtimeCapabilities = runtimeChannel
    ? (resolveChannelCapabilities({
        cfg: input.config,
        channel: runtimeChannel,
        accountId: input.agentAccountId,
      }) ?? [])
    : undefined;
  if (runtimeChannel === "telegram" && input.config) {
    const inlineButtonsScope = resolveTelegramInlineButtonsScope({
      cfg: input.config,
      accountId: input.agentAccountId ?? undefined,
    });
    if (inlineButtonsScope !== "off") {
      if (!runtimeCapabilities) {
        runtimeCapabilities = [];
      }
      if (
        !runtimeCapabilities.some((cap) => String(cap).trim().toLowerCase() === "inlinebuttons")
      ) {
        runtimeCapabilities.push("inlineButtons");
      }
    }
  }

  // Resolve reaction guidance (currently Telegram-only)
  const reactionGuidance =
    runtimeChannel && input.config
      ? (() => {
          if (runtimeChannel === "telegram") {
            const resolved = resolveTelegramReactionLevel({
              cfg: input.config,
              accountId: input.agentAccountId ?? undefined,
            });
            const level = resolved.agentReactionGuidance;
            return level ? { level, channel: "Telegram" } : undefined;
          }
          return undefined;
        })()
      : undefined;

  const sandboxInfo = buildEmbeddedSandboxInfo(input.sandbox, input.bashElevated);
  const reasoningTagHint = isReasoningTagProvider(input.provider);

  // Resolve channel-specific message actions for system prompt
  const channelActions = runtimeChannel
    ? listChannelSupportedActions(
        buildEmbeddedMessageActionDiscoveryInput({
          cfg: input.config,
          channel: runtimeChannel,
          currentChannelId: input.currentChannelId,
          currentThreadTs: input.currentThreadTs,
          currentMessageId: input.currentMessageId,
          accountId: input.agentAccountId,
          sessionKey: input.sessionKey,
          sessionId: input.sessionId,
          agentId: input.sessionAgentId,
          senderId: input.senderId,
        }),
      )
    : undefined;

  const messageToolHints = runtimeChannel
    ? resolveChannelMessageToolHints({
        cfg: input.config,
        channel: runtimeChannel,
        accountId: input.agentAccountId,
      })
    : undefined;

  // Build system prompt params (runtime info, timezone, etc.)
  const defaultModelRef = resolveDefaultModelForAgent({
    cfg: input.config ?? {},
    agentId: input.sessionAgentId,
  });
  const defaultModelLabel = `${defaultModelRef.provider}/${defaultModelRef.model}`;
  const { runtimeInfo, userTimezone, userTime, userTimeFormat } = buildSystemPromptParams({
    config: input.config,
    agentId: input.sessionAgentId,
    workspaceDir: input.effectiveWorkspace,
    cwd: process.cwd(),
    runtime: {
      host: input.machineName,
      os: `${os.type()} ${os.release()}`,
      arch: os.arch(),
      node: process.version,
      model: `${input.provider}/${input.modelId}`,
      defaultModel: defaultModelLabel,
      shell: detectRuntimeShell(),
      channel: runtimeChannel,
      capabilities: runtimeCapabilities,
      channelActions,
    },
  });

  const isDefaultAgent = input.sessionAgentId === input.defaultAgentId;
  const promptMode = resolvePromptModeForSession(input.sessionKey);

  // Process bootstrap context
  const bootstrapMaxChars = resolveBootstrapMaxChars(input.config);
  const bootstrapTotalMaxChars = resolveBootstrapTotalMaxChars(input.config);
  const bootstrapAnalysis = analyzeBootstrapBudget({
    files: buildBootstrapInjectionStats({
      bootstrapFiles: input.bootstrapFiles,
      injectedFiles: input.contextFiles,
    }),
    bootstrapMaxChars,
    bootstrapTotalMaxChars,
  });
  const bootstrapPromptWarningMode = resolveBootstrapPromptTruncationWarningMode(input.config);
  const bootstrapPromptWarning = buildBootstrapPromptWarning({
    analysis: bootstrapAnalysis,
    mode: bootstrapPromptWarningMode,
    seenSignatures: input.bootstrapPromptWarningSignaturesSeen,
    previousSignature: input.bootstrapPromptWarningSignature,
  });
  const workspaceNotes = input.bootstrapFiles.some(
    (file) => file.name === DEFAULT_BOOTSTRAP_FILENAME && !file.missing,
  )
    ? ["Reminder: commit your changes in this workspace after edits."]
    : undefined;

  const ttsHint = input.config ? buildTtsSystemPromptHint(input.config) : undefined;
  const ownerDisplay = resolveOwnerDisplaySetting(input.config);
  const heartbeatPrompt = isDefaultAgent
    ? resolveHeartbeatPrompt(input.config?.agents?.defaults?.heartbeat?.prompt)
    : undefined;

  // Build the system prompt text
  const appendPrompt = buildEmbeddedSystemPrompt({
    workspaceDir: input.effectiveWorkspace,
    defaultThinkLevel: input.thinkLevel,
    reasoningLevel: input.reasoningLevel || "off",
    extraSystemPrompt: input.extraSystemPrompt,
    ownerNumbers: input.ownerNumbers,
    ownerDisplay: ownerDisplay.ownerDisplay,
    ownerDisplaySecret: ownerDisplay.ownerDisplaySecret,
    reasoningTagHint,
    heartbeatPrompt,
    skillsPrompt: input.skillsPrompt,
    docsPath: input.docsPath ?? undefined,
    ttsHint,
    workspaceNotes,
    reactionGuidance,
    promptMode,
    acpEnabled: input.config?.acp?.enabled !== false,
    runtimeInfo,
    messageToolHints,
    sandboxInfo,
    tools: input.effectiveTools,
    modelAliasLines: buildModelAliasLines(input.config),
    userTimezone,
    userTime,
    userTimeFormat,
    contextFiles: input.contextFiles,
    memoryCitationsMode: input.config?.memory?.citations,
  });

  // Build system prompt report
  const systemPromptReport = buildSystemPromptReport({
    source: "run",
    generatedAt: Date.now(),
    sessionId: input.sessionId,
    sessionKey: input.sessionKey,
    provider: input.provider,
    model: input.modelId,
    workspaceDir: input.effectiveWorkspace,
    bootstrapMaxChars,
    bootstrapTotalMaxChars,
    bootstrapTruncation: buildBootstrapTruncationReportMeta({
      analysis: bootstrapAnalysis,
      warningMode: bootstrapPromptWarningMode,
      warning: bootstrapPromptWarning,
    }),
    sandbox: (() => {
      const runtime = resolveSandboxRuntimeStatus({
        cfg: input.config,
        sessionKey: input.sandboxSessionKey,
      });
      return { mode: runtime.mode, sandboxed: runtime.sandboxed };
    })(),
    systemPrompt: appendPrompt,
    bootstrapFiles: input.bootstrapFiles,
    injectedFiles: input.contextFiles,
    skillsPrompt: input.skillsPrompt ?? "",
    tools: input.effectiveTools,
  });

  const systemPromptOverride = createSystemPromptOverride(appendPrompt);
  const systemPromptText = systemPromptOverride();

  return {
    systemPromptText,
    systemPromptReport,
    bootstrapPromptWarning,
    heartbeatPrompt,
    promptMode,
  };
}
