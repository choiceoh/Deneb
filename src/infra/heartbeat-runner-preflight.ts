import fs from "node:fs/promises";
import path from "node:path";
import { resolveSendableOutboundReplyParts } from "deneb/plugin-sdk/reply-payload";
import { resolveAgentWorkspaceDir } from "../agents/agent-scope.js";
import { DEFAULT_HEARTBEAT_FILENAME } from "../agents/workspace/workspace.js";
import {
  isHeartbeatContentEffectivelyEmpty,
  stripHeartbeatToken,
} from "../auto-reply/heartbeat.js";
import type { ReplyPayload } from "../auto-reply/types.js";
import type { DenebConfig } from "../config/config.js";
import { escapeRegExp } from "../utils.js";
import { hasErrnoCode } from "./errors.js";
import {
  buildExecEventPrompt,
  buildCronEventPrompt,
  isCronSystemEvent,
  isExecCompletionEvent,
} from "./heartbeat-events-filter.js";
import { resolveHeartbeatReasonKind } from "./heartbeat-reason.js";
import type { HeartbeatConfig } from "./heartbeat-runner-config.js";
import { resolveHeartbeatPrompt, resolveHeartbeatSession } from "./heartbeat-runner-config.js";
import { peekSystemEventEntries } from "./system-events.js";

export function resolveHeartbeatReasoningPayloads(
  replyResult: ReplyPayload | ReplyPayload[] | undefined,
): ReplyPayload[] {
  const payloads = Array.isArray(replyResult) ? replyResult : replyResult ? [replyResult] : [];
  return payloads.filter((payload) => {
    const text = typeof payload.text === "string" ? payload.text : "";
    return text.trimStart().startsWith("Reasoning:");
  });
}

export function stripLeadingHeartbeatResponsePrefix(
  text: string,
  responsePrefix: string | undefined,
): string {
  const normalizedPrefix = responsePrefix?.trim();
  if (!normalizedPrefix) {
    return text;
  }

  // Require a boundary after the configured prefix so short prefixes like "Hi"
  // do not strip the beginning of normal words like "History".
  const prefixPattern = new RegExp(
    `^${escapeRegExp(normalizedPrefix)}(?=$|\\s|[\\p{P}\\p{S}])\\s*`,
    "iu",
  );
  return text.replace(prefixPattern, "");
}

export function normalizeHeartbeatReply(
  payload: ReplyPayload,
  responsePrefix: string | undefined,
  ackMaxChars: number,
) {
  const rawText = typeof payload.text === "string" ? payload.text : "";
  const textForStrip = stripLeadingHeartbeatResponsePrefix(rawText, responsePrefix);
  const stripped = stripHeartbeatToken(textForStrip, {
    mode: "heartbeat",
    maxAckChars: ackMaxChars,
  });
  const hasMedia = resolveSendableOutboundReplyParts(payload).hasMedia;
  if (stripped.shouldSkip && !hasMedia) {
    return {
      shouldSkip: true,
      text: "",
      hasMedia,
    };
  }
  let finalText = stripped.text;
  if (responsePrefix && finalText && !finalText.startsWith(responsePrefix)) {
    finalText = `${responsePrefix} ${finalText}`;
  }
  return { shouldSkip: false, text: finalText, hasMedia };
}

export type HeartbeatReasonFlags = {
  isExecEventReason: boolean;
  isCronEventReason: boolean;
  isWakeReason: boolean;
};

export type HeartbeatSkipReason = "empty-heartbeat-file";

export type HeartbeatPreflight = HeartbeatReasonFlags & {
  session: ReturnType<typeof resolveHeartbeatSession>;
  pendingEventEntries: ReturnType<typeof peekSystemEventEntries>;
  hasTaggedCronEvents: boolean;
  shouldInspectPendingEvents: boolean;
  skipReason?: HeartbeatSkipReason;
};

export function resolveHeartbeatReasonFlags(reason?: string): HeartbeatReasonFlags {
  const reasonKind = resolveHeartbeatReasonKind(reason);
  return {
    isExecEventReason: reasonKind === "exec-event",
    isCronEventReason: reasonKind === "cron",
    isWakeReason: reasonKind === "wake" || reasonKind === "hook",
  };
}

export async function resolveHeartbeatPreflight(params: {
  cfg: DenebConfig;
  agentId: string;
  heartbeat?: HeartbeatConfig;
  forcedSessionKey?: string;
  reason?: string;
}): Promise<HeartbeatPreflight> {
  const reasonFlags = resolveHeartbeatReasonFlags(params.reason);
  const session = resolveHeartbeatSession(
    params.cfg,
    params.agentId,
    params.heartbeat,
    params.forcedSessionKey,
  );
  const pendingEventEntries = peekSystemEventEntries(session.sessionKey);
  const hasTaggedCronEvents = pendingEventEntries.some((event) =>
    event.contextKey?.startsWith("cron:"),
  );
  const shouldInspectPendingEvents =
    reasonFlags.isExecEventReason || reasonFlags.isCronEventReason || hasTaggedCronEvents;
  const shouldBypassFileGates =
    reasonFlags.isExecEventReason ||
    reasonFlags.isCronEventReason ||
    reasonFlags.isWakeReason ||
    hasTaggedCronEvents;
  const basePreflight = {
    ...reasonFlags,
    session,
    pendingEventEntries,
    hasTaggedCronEvents,
    shouldInspectPendingEvents,
  } satisfies Omit<HeartbeatPreflight, "skipReason">;

  if (shouldBypassFileGates) {
    return basePreflight;
  }

  const workspaceDir = resolveAgentWorkspaceDir(params.cfg, params.agentId);
  const heartbeatFilePath = path.join(workspaceDir, DEFAULT_HEARTBEAT_FILENAME);
  try {
    const heartbeatFileContent = await fs.readFile(heartbeatFilePath, "utf-8");
    if (isHeartbeatContentEffectivelyEmpty(heartbeatFileContent)) {
      return {
        ...basePreflight,
        skipReason: "empty-heartbeat-file",
      };
    }
  } catch (err: unknown) {
    if (hasErrnoCode(err, "ENOENT")) {
      // Missing HEARTBEAT.md is intentional in some setups (for example, when
      // heartbeat instructions live outside the file), so keep the run active.
      // The heartbeat prompt already says "if it exists".
      return basePreflight;
    }
    // For other read errors, proceed with heartbeat as before.
  }

  return basePreflight;
}

export type HeartbeatPromptResolution = {
  prompt: string;
  hasExecCompletion: boolean;
  hasCronEvents: boolean;
};

export function appendHeartbeatWorkspacePathHint(prompt: string, workspaceDir: string): string {
  if (!/heartbeat\.md/i.test(prompt)) {
    return prompt;
  }
  const heartbeatFilePath = path.join(workspaceDir, DEFAULT_HEARTBEAT_FILENAME).replace(/\\/g, "/");
  const hint = `When reading HEARTBEAT.md, use workspace file ${heartbeatFilePath} (exact case). Do not read docs/heartbeat.md.`;
  if (prompt.includes(hint)) {
    return prompt;
  }
  return `${prompt}\n${hint}`;
}

export function resolveHeartbeatRunPrompt(params: {
  cfg: DenebConfig;
  heartbeat?: HeartbeatConfig;
  preflight: HeartbeatPreflight;
  canRelayToUser: boolean;
  workspaceDir: string;
}): HeartbeatPromptResolution {
  const pendingEventEntries = params.preflight.pendingEventEntries;
  const pendingEvents = params.preflight.shouldInspectPendingEvents
    ? pendingEventEntries.map((event) => event.text)
    : [];
  const cronEvents = pendingEventEntries
    .filter(
      (event) =>
        (params.preflight.isCronEventReason || event.contextKey?.startsWith("cron:")) &&
        isCronSystemEvent(event.text),
    )
    .map((event) => event.text);
  const hasExecCompletion = pendingEvents.some(isExecCompletionEvent);
  const hasCronEvents = cronEvents.length > 0;
  const basePrompt = hasExecCompletion
    ? buildExecEventPrompt({ deliverToUser: params.canRelayToUser })
    : hasCronEvents
      ? buildCronEventPrompt(cronEvents, { deliverToUser: params.canRelayToUser })
      : resolveHeartbeatPrompt(params.cfg, params.heartbeat);
  const prompt = appendHeartbeatWorkspacePathHint(basePrompt, params.workspaceDir);

  return { prompt, hasExecCompletion, hasCronEvents };
}
