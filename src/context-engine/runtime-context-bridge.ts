import type { EmbeddedCompactionRuntimeContext } from "../agents/pi-embedded-runner/compaction-runtime-context.js";
import type { ContextEngineRuntimeContext } from "./types.js";

function safeString(v: unknown): string | undefined {
  return typeof v === "string" ? v : undefined;
}

function safeStringOrNumber(v: unknown): string | number | undefined {
  return typeof v === "string" || typeof v === "number" ? v : undefined;
}

function safeBoolean(v: unknown): boolean | undefined {
  return typeof v === "boolean" ? v : undefined;
}

function safeStringArray(v: unknown): string[] | undefined {
  return Array.isArray(v) && v.every((x) => typeof x === "string") ? v : undefined;
}

/**
 * Narrow an untyped ContextEngineRuntimeContext into the typed
 * EmbeddedCompactionRuntimeContext used by the built-in compaction path.
 *
 * Each field is explicitly extracted with a type guard so that changes to the
 * typed interface surface as compile errors instead of silent mismatches.
 */
export function narrowRuntimeContext(
  ctx: ContextEngineRuntimeContext | undefined,
): EmbeddedCompactionRuntimeContext | undefined {
  if (!ctx || typeof ctx !== "object") {
    return undefined;
  }
  return {
    sessionKey: safeString(ctx.sessionKey),
    messageChannel: safeString(ctx.messageChannel),
    messageProvider: safeString(ctx.messageProvider),
    agentAccountId: safeString(ctx.agentAccountId),
    currentChannelId: safeString(ctx.currentChannelId),
    currentThreadTs: safeString(ctx.currentThreadTs),
    currentMessageId: safeStringOrNumber(ctx.currentMessageId),
    authProfileId: safeString(ctx.authProfileId),
    workspaceDir: safeString(ctx.workspaceDir) ?? process.cwd(),
    agentDir: safeString(ctx.agentDir) ?? "",
    config: ctx.config as EmbeddedCompactionRuntimeContext["config"],
    skillsSnapshot: ctx.skillsSnapshot as EmbeddedCompactionRuntimeContext["skillsSnapshot"],
    senderIsOwner: safeBoolean(ctx.senderIsOwner),
    senderId: safeString(ctx.senderId),
    provider: safeString(ctx.provider),
    model: safeString(ctx.model),
    thinkLevel: safeString(ctx.thinkLevel) as EmbeddedCompactionRuntimeContext["thinkLevel"],
    reasoningLevel: safeString(
      ctx.reasoningLevel,
    ) as EmbeddedCompactionRuntimeContext["reasoningLevel"],
    bashElevated: ctx.bashElevated as EmbeddedCompactionRuntimeContext["bashElevated"],
    extraSystemPrompt: safeString(ctx.extraSystemPrompt),
    ownerNumbers: safeStringArray(ctx.ownerNumbers),
  };
}
