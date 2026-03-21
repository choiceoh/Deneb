import type { EmbeddedCompactionRuntimeContext } from "../agents/pi-embedded-runner/compaction-runtime-context.js";
import type { ContextEngine, CompactResult, ContextEngineRuntimeContext } from "./types.js";

// ── Type-safe narrowing helpers ──────────────────────────────────────────────

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

/** Narrow untyped runtimeContext into EmbeddedCompactionRuntimeContext. */
function narrowRuntimeContext(
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

// ── Delegation ───────────────────────────────────────────────────────────────

/**
 * Delegate a context-engine compaction request to Deneb's built-in runtime compaction path.
 *
 * Third-party engines can call this from their own `compact()` when they do not
 * own the compaction algorithm but still need `/compact` and overflow recovery
 * to use the stock runtime behavior.
 */
export async function delegateCompactionToRuntime(
  params: Parameters<ContextEngine["compact"]>[0],
): Promise<CompactResult> {
  const { compactEmbeddedPiSessionDirect } =
    await import("../agents/pi-embedded-runner/compact.runtime.js");

  const typed = narrowRuntimeContext(params.runtimeContext);
  // currentTokenCount may be an ad-hoc field on runtimeContext (overflow recovery).
  const rawCurrentTokenCount = params.runtimeContext?.currentTokenCount;
  const currentTokenCount =
    params.currentTokenCount ??
    (typeof rawCurrentTokenCount === "number" &&
    Number.isFinite(rawCurrentTokenCount) &&
    rawCurrentTokenCount > 0
      ? Math.floor(rawCurrentTokenCount)
      : undefined);

  const result = await compactEmbeddedPiSessionDirect({
    ...typed,
    sessionId: params.sessionId,
    sessionFile: params.sessionFile,
    tokenBudget: params.tokenBudget,
    ...(currentTokenCount !== undefined ? { currentTokenCount } : {}),
    force: params.force,
    customInstructions: params.customInstructions,
    workspaceDir: typed?.workspaceDir ?? process.cwd(),
  } as Parameters<typeof compactEmbeddedPiSessionDirect>[0]);

  return {
    ok: result.ok,
    compacted: result.compacted,
    reason: result.reason,
    result: result.result
      ? {
          summary: result.result.summary,
          firstKeptEntryId: result.result.firstKeptEntryId,
          tokensBefore: result.result.tokensBefore,
          tokensAfter: result.result.tokensAfter,
          details: result.result.details,
        }
      : undefined,
  };
}
