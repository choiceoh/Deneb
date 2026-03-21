import { narrowRuntimeContext } from "./runtime-context-bridge.js";
import type { ContextEngine, CompactResult } from "./types.js";

/**
 * Delegate a context-engine compaction request to Deneb's built-in runtime compaction path.
 *
 * This is the same bridge used by the legacy context engine. Third-party
 * engines can call it from their own `compact()` implementations when they do
 * not own the compaction algorithm but still need `/compact` and overflow
 * recovery to use the stock runtime behavior.
 *
 * Note: `compactionTarget` is part of the public `compact()` contract, but the
 * built-in runtime compaction path does not expose that knob. This helper
 * ignores it to preserve legacy behavior; engines that need target-specific
 * compaction should implement their own `compact()` algorithm.
 */
export async function delegateCompactionToRuntime(
  params: Parameters<ContextEngine["compact"]>[0],
): Promise<CompactResult> {
  // Import through a dedicated runtime boundary so the lazy edge remains effective.
  const { compactEmbeddedPiSessionDirect } =
    await import("../agents/pi-embedded-runner/compact.runtime.js");

  const typed = narrowRuntimeContext(params.runtimeContext);
  // currentTokenCount may also be passed as an ad-hoc field on runtimeContext
  // (e.g. during overflow recovery).  Check the raw context for it since the
  // typed bridge intentionally ignores unknown fields.
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
