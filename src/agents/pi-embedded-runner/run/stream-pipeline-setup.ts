import type { AgentMessage, StreamFn } from "@mariozechner/pi-agent-core";
import type { Api, Model } from "@mariozechner/pi-ai";
import type { AnthropicPayloadLogger } from "../../anthropic-payload-log.js";
import type { CacheTrace } from "../../cache-trace.js";
import { resolveToolCallArgumentsEncoding } from "../../models/model-compat.js";
import { downgradeOpenAIFunctionCallReasoningPairs } from "../../pi-embedded-helpers.js";
import { sanitizeToolCallIdsForCloudCodeAssist } from "../../tool-call-id.js";
import type { TranscriptPolicy } from "../../transcript-policy.js";
import { dropThinkingBlocks } from "../thinking.js";
import { createYieldAbortedResponse } from "./sessions-yield.js";
import {
  shouldRepairMalformedAnthropicToolCallArguments,
  wrapStreamFnRepairMalformedToolCallArguments,
} from "./stream-tool-call-repair.js";
import {
  wrapStreamFnSanitizeMalformedToolCalls,
  wrapStreamFnTrimToolCallNames,
} from "./stream-tool-call-sanitize.js";
import { wrapStreamFnDecodeXaiToolCallArguments } from "./stream-xai-decode.js";

export type StreamPipelineParams = {
  provider: string;
  modelId: string;
  model: Model<Api>;
  transcriptPolicy: TranscriptPolicy;
  allowedToolNames: Set<string>;
  runAbortController: AbortController;
  yieldDetectedRef: { current: boolean };
  cacheTrace?: CacheTrace | null;
  anthropicPayloadLogger?: AnthropicPayloadLogger | null;
};

/**
 * Applies all stream function wrappers to the agent's streamFn in the correct
 * order. Each wrapper intercepts outbound model requests to sanitize, repair,
 * or transform messages for provider compatibility.
 */
export function buildStreamPipeline(
  baseStreamFn: StreamFn,
  params: StreamPipelineParams,
): StreamFn {
  let streamFn: StreamFn = baseStreamFn;

  const { transcriptPolicy, model, provider, allowedToolNames, runAbortController } = params;

  // Wrap with cache trace recording
  if (params.cacheTrace) {
    streamFn = params.cacheTrace.wrapStreamFn(streamFn);
  }

  // Anthropic Claude: strip replayed thinking blocks that cause rejections
  if (transcriptPolicy.dropThinkingBlocks) {
    const inner = streamFn;
    streamFn = (mdl, context, options) => {
      const ctx = context as unknown as { messages?: unknown };
      const messages = ctx?.messages;
      if (!Array.isArray(messages)) {
        return inner(mdl, context, options);
      }
      const sanitized = dropThinkingBlocks(messages as unknown as AgentMessage[]) as unknown;
      if (sanitized === messages) {
        return inner(mdl, context, options);
      }
      const nextContext = {
        ...(context as unknown as Record<string, unknown>),
        messages: sanitized,
      } as unknown;
      return inner(mdl, nextContext as typeof context, options);
    };
  }

  // Strict providers: sanitize tool call IDs to match format requirements
  if (transcriptPolicy.sanitizeToolCallIds && transcriptPolicy.toolCallIdMode) {
    const inner = streamFn;
    const mode = transcriptPolicy.toolCallIdMode;
    streamFn = (mdl, context, options) => {
      const ctx = context as unknown as { messages?: unknown };
      const messages = ctx?.messages;
      if (!Array.isArray(messages)) {
        return inner(mdl, context, options);
      }
      const sanitized = sanitizeToolCallIdsForCloudCodeAssist(messages as AgentMessage[], mode);
      if (sanitized === messages) {
        return inner(mdl, context, options);
      }
      const nextContext = {
        ...(context as unknown as Record<string, unknown>),
        messages: sanitized,
      } as unknown;
      return inner(mdl, nextContext as typeof context, options);
    };
  }

  // OpenAI Responses: downgrade function call reasoning pairs
  if (model.api === "openai-responses" || model.api === "openai-codex-responses") {
    const inner = streamFn;
    streamFn = (mdl, context, options) => {
      const ctx = context as unknown as { messages?: unknown };
      const messages = ctx?.messages;
      if (!Array.isArray(messages)) {
        return inner(mdl, context, options);
      }
      const sanitized = downgradeOpenAIFunctionCallReasoningPairs(messages as AgentMessage[]);
      if (sanitized === messages) {
        return inner(mdl, context, options);
      }
      const nextContext = {
        ...(context as unknown as Record<string, unknown>),
        messages: sanitized,
      } as unknown;
      return inner(mdl, nextContext as typeof context, options);
    };
  }

  // Yield abort interception: short-circuit when sessions_yield is detected
  {
    const inner = streamFn;
    streamFn = (mdl, context, options) => {
      const signal = runAbortController.signal as AbortSignal & { reason?: unknown };
      if (params.yieldDetectedRef.current && signal.aborted && signal.reason === "sessions_yield") {
        return createYieldAbortedResponse(mdl) as unknown as ReturnType<StreamFn>;
      }
      return inner(mdl, context, options);
    };
  }

  // Normalize malformed tool names and trim whitespace
  streamFn = wrapStreamFnSanitizeMalformedToolCalls(streamFn, allowedToolNames, transcriptPolicy);
  streamFn = wrapStreamFnTrimToolCallNames(streamFn, allowedToolNames);

  // Repair malformed Anthropic tool call arguments
  if (
    model.api === "anthropic-messages" &&
    shouldRepairMalformedAnthropicToolCallArguments(provider)
  ) {
    streamFn = wrapStreamFnRepairMalformedToolCallArguments(streamFn);
  }

  // Decode HTML entities in xAI tool call arguments
  if (resolveToolCallArgumentsEncoding(model) === "html-entities") {
    streamFn = wrapStreamFnDecodeXaiToolCallArguments(streamFn);
  }

  // Anthropic payload logger (outermost wrapper)
  if (params.anthropicPayloadLogger) {
    streamFn = params.anthropicPayloadLogger.wrapStreamFn(streamFn);
  }

  return streamFn;
}
