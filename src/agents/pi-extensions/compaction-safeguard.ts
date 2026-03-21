import fs from "node:fs";
import path from "node:path";
import type { AgentMessage } from "@mariozechner/pi-agent-core";
import type { Api, Model } from "@mariozechner/pi-ai";
import type { ExtensionAPI, FileOperations } from "@mariozechner/pi-coding-agent";
import { extractSections } from "../../auto-reply/reply/post-compaction-context.js";
import type { AgentCompactionIdentifierPolicy } from "../../config/types.agent-defaults.js";
import { openBoundaryFile } from "../../infra/boundary-file-read.js";
import { createSubsystemLogger } from "../../logging/subsystem.js";
import {
  type CompactionSummarizationInstructions,
  SAFETY_MARGIN,
  SUMMARIZATION_OVERHEAD_TOKENS,
  computeAdaptiveChunkRatio,
  estimateMessagesTokens,
  pruneHistoryForContextShare,
  resolveContextWindowTokens,
  summarizeWithFallback,
} from "../compaction.js";
import { collectTextContentBlocks } from "../content-blocks.js";
import { wrapUntrustedPromptDataBlock } from "../sanitize-for-prompt.js";
import { repairToolUseResultPairing } from "../session-transcript-repair.js";
import { extractToolCallsFromAssistant, extractToolResultId } from "../tool-call-id.js";
import { createSessionManagerRuntimeRegistry } from "./session-manager-runtime-registry.js";

const log = createSubsystemLogger("compaction-safeguard");

// ── Compaction instruction resolution ────────────────────────────────────────

const DEFAULT_COMPACTION_INSTRUCTIONS =
  "Write the summary body in the primary language used in the conversation.\n" +
  "Focus on factual content: what was discussed, decisions made, and current state.\n" +
  "Keep the required summary structure and section headers unchanged.\n" +
  "Do not translate or alter code, file paths, identifiers, or error messages.";

const MAX_INSTRUCTION_LENGTH = 800;

function truncateUnicodeSafe(s: string, maxCodePoints: number): string {
  const chars = Array.from(s);
  return chars.length <= maxCodePoints ? s : chars.slice(0, maxCodePoints).join("");
}

function normalizeInstruction(s: string | undefined): string | undefined {
  if (s == null) {
    return undefined;
  }
  const trimmed = s.trim();
  return trimmed.length > 0 ? trimmed : undefined;
}

export function resolveCompactionInstructions(
  eventInstructions: string | undefined,
  runtimeInstructions: string | undefined,
): string {
  const resolved =
    normalizeInstruction(eventInstructions) ??
    normalizeInstruction(runtimeInstructions) ??
    DEFAULT_COMPACTION_INSTRUCTIONS;
  return truncateUnicodeSafe(resolved, MAX_INSTRUCTION_LENGTH);
}

export function composeSplitTurnInstructions(
  turnPrefixInstructions: string,
  resolvedInstructions: string,
): string {
  return [turnPrefixInstructions, "Additional requirements:", resolvedInstructions].join("\n\n");
}

// ── Runtime registry ─────────────────────────────────────────────────────────

export type CompactionSafeguardRuntimeValue = {
  maxHistoryShare?: number;
  contextWindowTokens?: number;
  identifierPolicy?: AgentCompactionIdentifierPolicy;
  identifierInstructions?: string;
  customInstructions?: string;
  model?: Model<Api>;
  recentTurnsPreserve?: number;
};

const registry = createSessionManagerRuntimeRegistry<CompactionSafeguardRuntimeValue>();
export const setCompactionSafeguardRuntime = registry.set;
export const getCompactionSafeguardRuntime = registry.get;

// ── Constants ────────────────────────────────────────────────────────────────

const missedModelWarningSessions = new WeakSet<object>();
const TURN_PREFIX_INSTRUCTIONS =
  "This summary covers the prefix of a split turn. Focus on the original request," +
  " early progress, and any details needed to understand the retained suffix.";
const MAX_TOOL_FAILURES = 8;
const MAX_TOOL_FAILURE_CHARS = 240;
const DEFAULT_RECENT_TURNS_PRESERVE = 3;
const MAX_RECENT_TURNS_PRESERVE = 12;
const MAX_RECENT_TURN_TEXT_CHARS = 600;
const MAX_UNTRUSTED_INSTRUCTION_CHARS = 4000;

// ── Performance stability bounds ─────────────────────────────────────────────
// These ensure compaction runs predictably regardless of config values.

/** Minimum maxHistoryShare to prevent zero-budget pruning loops. */
const MIN_MAX_HISTORY_SHARE = 0.1;
/** Maximum maxHistoryShare to prevent the summarizer from being overwhelmed. */
const MAX_MAX_HISTORY_SHARE = 0.9;
/** Floor for context window tokens to avoid degenerate chunk calculations. */
const MIN_CONTEXT_WINDOW_TOKENS = 4096;
/** Floor for maxChunkTokens to prevent excessive micro-chunking. */
const MIN_MAX_CHUNK_TOKENS = 1024;
/** Reserve tokens must not exceed this share of context window. */
const MAX_RESERVE_TOKEN_SHARE = 0.8;
const REQUIRED_SUMMARY_SECTIONS = [
  "## Decisions",
  "## Open TODOs",
  "## Constraints/Rules",
  "## Pending user asks",
  "## Exact identifiers",
] as const;
const STRICT_EXACT_IDENTIFIERS_INSTRUCTION =
  "For ## Exact identifiers, preserve literal values exactly as seen (IDs, URLs, file paths, ports, hashes, dates, times).";
const POLICY_OFF_EXACT_IDENTIFIERS_INSTRUCTION =
  "For ## Exact identifiers, include identifiers only when needed for continuity; do not enforce literal-preservation rules.";

// ── Performance stability helpers ─────────────────────────────────────────────

function clampMaxHistoryShare(value: number | undefined): number {
  const raw = value ?? 0.5;
  const clamped = Math.min(MAX_MAX_HISTORY_SHARE, Math.max(MIN_MAX_HISTORY_SHARE, raw));
  if (clamped !== raw) {
    log.warn(`compaction-safeguard: clamped maxHistoryShare ${raw} → ${clamped}`);
  }
  return clamped;
}

function clampContextWindowTokens(value: number | undefined, modelContextWindow: number): number {
  const raw = value ?? modelContextWindow;
  const clamped = Math.max(MIN_CONTEXT_WINDOW_TOKENS, raw);
  if (clamped !== raw) {
    log.warn(`compaction-safeguard: clamped contextWindowTokens ${raw} → ${clamped}`);
  }
  return clamped;
}

function clampMaxChunkTokens(value: number): number {
  const clamped = Math.max(MIN_MAX_CHUNK_TOKENS, value);
  if (clamped !== value) {
    log.warn(`compaction-safeguard: clamped maxChunkTokens ${value} → ${clamped}`);
  }
  return clamped;
}

function clampReserveTokens(value: number, contextWindowTokens: number): number {
  const maxReserve = Math.floor(contextWindowTokens * MAX_RESERVE_TOKEN_SHARE);
  const clamped = Math.max(1, Math.min(value, maxReserve));
  if (clamped !== value) {
    log.warn(
      `compaction-safeguard: clamped reserveTokens ${value} → ${clamped} (ctxWindow=${contextWindowTokens})`,
    );
  }
  return clamped;
}

// ── Helpers ──────────────────────────────────────────────────────────────────

type ToolFailure = {
  toolCallId: string;
  toolName: string;
  summary: string;
  meta?: string;
};

function clampNonNegativeInt(value: unknown, fallback: number): number {
  const normalized = typeof value === "number" && Number.isFinite(value) ? value : fallback;
  return Math.max(0, Math.floor(normalized));
}

function resolveRecentTurnsPreserve(value: unknown): number {
  return Math.min(
    MAX_RECENT_TURNS_PRESERVE,
    clampNonNegativeInt(value, DEFAULT_RECENT_TURNS_PRESERVE),
  );
}

function normalizeFailureText(text: string): string {
  return text.replace(/\s+/g, " ").trim();
}

function truncateFailureText(text: string, maxChars: number): string {
  return text.length <= maxChars ? text : `${text.slice(0, Math.max(0, maxChars - 3))}...`;
}

function formatToolFailureMeta(details: unknown): string | undefined {
  if (!details || typeof details !== "object") {
    return undefined;
  }
  const record = details as Record<string, unknown>;
  const status = typeof record.status === "string" ? record.status : undefined;
  const exitCode =
    typeof record.exitCode === "number" && Number.isFinite(record.exitCode)
      ? record.exitCode
      : undefined;
  const parts: string[] = [];
  if (status) {
    parts.push(`status=${status}`);
  }
  if (exitCode !== undefined) {
    parts.push(`exitCode=${exitCode}`);
  }
  return parts.length > 0 ? parts.join(" ") : undefined;
}

function extractToolResultText(content: unknown): string {
  return collectTextContentBlocks(content).join("\n");
}

function collectToolFailures(messages: AgentMessage[]): ToolFailure[] {
  const failures: ToolFailure[] = [];
  const seen = new Set<string>();

  for (const message of messages) {
    if (!message || typeof message !== "object") {
      continue;
    }
    const role = (message as { role?: unknown }).role;
    if (role !== "toolResult") {
      continue;
    }
    const toolResult = message as {
      toolCallId?: unknown;
      toolName?: unknown;
      content?: unknown;
      details?: unknown;
      isError?: unknown;
    };
    if (toolResult.isError !== true) {
      continue;
    }
    const toolCallId = typeof toolResult.toolCallId === "string" ? toolResult.toolCallId : "";
    if (!toolCallId || seen.has(toolCallId)) {
      continue;
    }
    seen.add(toolCallId);

    const toolName =
      typeof toolResult.toolName === "string" && toolResult.toolName.trim()
        ? toolResult.toolName
        : "tool";
    const rawText = extractToolResultText(toolResult.content);
    const meta = formatToolFailureMeta(toolResult.details);
    const normalized = normalizeFailureText(rawText);
    const summary = truncateFailureText(
      normalized || (meta ? "failed" : "failed (no output)"),
      MAX_TOOL_FAILURE_CHARS,
    );
    failures.push({ toolCallId, toolName, summary, meta });
  }

  return failures;
}

function formatToolFailuresSection(failures: ToolFailure[]): string {
  if (failures.length === 0) {
    return "";
  }
  const lines = failures.slice(0, MAX_TOOL_FAILURES).map((failure) => {
    const meta = failure.meta ? ` (${failure.meta})` : "";
    return `- ${failure.toolName}${meta}: ${failure.summary}`;
  });
  if (failures.length > MAX_TOOL_FAILURES) {
    lines.push(`- ...and ${failures.length - MAX_TOOL_FAILURES} more`);
  }
  return `\n\n## Tool Failures\n${lines.join("\n")}`;
}

function isRealConversationMessage(message: AgentMessage): boolean {
  return message.role === "user" || message.role === "assistant" || message.role === "toolResult";
}

function computeFileLists(fileOps: FileOperations): {
  readFiles: string[];
  modifiedFiles: string[];
} {
  const modified = new Set([...fileOps.edited, ...fileOps.written]);
  const readFiles = [...fileOps.read].filter((f) => !modified.has(f)).toSorted();
  const modifiedFiles = [...modified].toSorted();
  return { readFiles, modifiedFiles };
}

function formatFileOperations(readFiles: string[], modifiedFiles: string[]): string {
  const sections: string[] = [];
  if (readFiles.length > 0) {
    sections.push(`<read-files>\n${readFiles.join("\n")}\n</read-files>`);
  }
  if (modifiedFiles.length > 0) {
    sections.push(`<modified-files>\n${modifiedFiles.join("\n")}\n</modified-files>`);
  }
  return sections.length === 0 ? "" : `\n\n${sections.join("\n\n")}`;
}

function extractMessageText(message: AgentMessage): string {
  const content = (message as { content?: unknown }).content;
  if (typeof content === "string") {
    return content.trim();
  }
  if (!Array.isArray(content)) {
    return "";
  }
  return collectTextContentBlocks(content)
    .map((t) => t.trim())
    .filter((t) => t.length > 0)
    .join("\n")
    .trim();
}

function formatNonTextPlaceholder(content: unknown): string | null {
  if (content === null || content === undefined) {
    return null;
  }
  if (typeof content === "string") {
    return null;
  }
  if (!Array.isArray(content)) {
    return "[non-text content]";
  }
  const typeCounts = new Map<string, number>();
  for (const block of content) {
    if (!block || typeof block !== "object") {
      continue;
    }
    const typeRaw = (block as { type?: unknown }).type;
    const type = typeof typeRaw === "string" && typeRaw.trim().length > 0 ? typeRaw : "unknown";
    if (type === "text") {
      continue;
    }
    typeCounts.set(type, (typeCounts.get(type) ?? 0) + 1);
  }
  if (typeCounts.size === 0) {
    return null;
  }
  const parts = [...typeCounts.entries()].map(([type, count]) =>
    count > 1 ? `${type} x${count}` : type,
  );
  return `[non-text content: ${parts.join(", ")}]`;
}

function splitPreservedRecentTurns(params: {
  messages: AgentMessage[];
  recentTurnsPreserve: number;
}): { summarizableMessages: AgentMessage[]; preservedMessages: AgentMessage[] } {
  if (params.recentTurnsPreserve <= 0) {
    return { summarizableMessages: params.messages, preservedMessages: [] };
  }
  const conversationIndexes: number[] = [];
  const userIndexes: number[] = [];
  for (let i = 0; i < params.messages.length; i += 1) {
    const role = (params.messages[i] as { role?: unknown }).role;
    if (role === "user" || role === "assistant") {
      conversationIndexes.push(i);
      if (role === "user") {
        userIndexes.push(i);
      }
    }
  }
  if (conversationIndexes.length === 0) {
    return { summarizableMessages: params.messages, preservedMessages: [] };
  }

  const preservedIndexSet = new Set<number>();
  if (userIndexes.length >= params.recentTurnsPreserve) {
    const boundaryStartIndex = userIndexes[userIndexes.length - params.recentTurnsPreserve] ?? -1;
    if (boundaryStartIndex >= 0) {
      for (const index of conversationIndexes) {
        if (index >= boundaryStartIndex) {
          preservedIndexSet.add(index);
        }
      }
    }
  } else {
    const fallbackMessageCount = params.recentTurnsPreserve * 2;
    for (const userIndex of userIndexes) {
      preservedIndexSet.add(userIndex);
    }
    for (let i = conversationIndexes.length - 1; i >= 0; i -= 1) {
      const index = conversationIndexes[i];
      if (index === undefined) {
        continue;
      }
      preservedIndexSet.add(index);
      if (preservedIndexSet.size >= fallbackMessageCount) {
        break;
      }
    }
  }
  if (preservedIndexSet.size === 0) {
    return { summarizableMessages: params.messages, preservedMessages: [] };
  }
  const preservedToolCallIds = new Set<string>();
  for (let i = 0; i < params.messages.length; i += 1) {
    if (!preservedIndexSet.has(i)) {
      continue;
    }
    const message = params.messages[i];
    if ((message as { role?: unknown }).role !== "assistant") {
      continue;
    }
    const toolCalls = extractToolCallsFromAssistant(
      message as Extract<AgentMessage, { role: "assistant" }>,
    );
    for (const toolCall of toolCalls) {
      preservedToolCallIds.add(toolCall.id);
    }
  }
  if (preservedToolCallIds.size > 0) {
    let preservedStartIndex = -1;
    for (let i = 0; i < params.messages.length; i += 1) {
      if (preservedIndexSet.has(i)) {
        preservedStartIndex = i;
        break;
      }
    }
    if (preservedStartIndex >= 0) {
      for (let i = preservedStartIndex; i < params.messages.length; i += 1) {
        const message = params.messages[i];
        if ((message as { role?: unknown }).role !== "toolResult") {
          continue;
        }
        const toolResultId = extractToolResultId(
          message as Extract<AgentMessage, { role: "toolResult" }>,
        );
        if (toolResultId && preservedToolCallIds.has(toolResultId)) {
          preservedIndexSet.add(i);
        }
      }
    }
  }
  const summarizableMessages = params.messages.filter((_, idx) => !preservedIndexSet.has(idx));
  const repairedSummarizableMessages = repairToolUseResultPairing(summarizableMessages).messages;
  const preservedMessages = params.messages
    .filter((_, idx) => preservedIndexSet.has(idx))
    .filter((msg) => {
      const role = (msg as { role?: unknown }).role;
      return role === "user" || role === "assistant" || role === "toolResult";
    });
  return { summarizableMessages: repairedSummarizableMessages, preservedMessages };
}

function formatPreservedTurnsSection(messages: AgentMessage[]): string {
  if (messages.length === 0) {
    return "";
  }
  const lines = messages
    .map((message) => {
      let roleLabel: string;
      if (message.role === "assistant") {
        roleLabel = "Assistant";
      } else if (message.role === "user") {
        roleLabel = "User";
      } else if (message.role === "toolResult") {
        const toolName = (message as { toolName?: unknown }).toolName;
        const safeToolName = typeof toolName === "string" && toolName.trim() ? toolName : "tool";
        roleLabel = `Tool result (${safeToolName})`;
      } else {
        return null;
      }
      const text = extractMessageText(message);
      const nonTextPlaceholder = formatNonTextPlaceholder(
        (message as { content?: unknown }).content,
      );
      const rendered =
        text && nonTextPlaceholder ? `${text}\n${nonTextPlaceholder}` : text || nonTextPlaceholder;
      if (!rendered) {
        return null;
      }
      const trimmed =
        rendered.length > MAX_RECENT_TURN_TEXT_CHARS
          ? `${rendered.slice(0, MAX_RECENT_TURN_TEXT_CHARS)}...`
          : rendered;
      return `- ${roleLabel}: ${trimmed}`;
    })
    .filter((line): line is string => Boolean(line));
  if (lines.length === 0) {
    return "";
  }
  return `\n\n## Recent turns preserved verbatim\n${lines.join("\n")}`;
}

function wrapUntrustedInstructionBlock(label: string, text: string): string {
  return wrapUntrustedPromptDataBlock({
    label,
    text,
    maxChars: MAX_UNTRUSTED_INSTRUCTION_CHARS,
  });
}

function resolveExactIdentifierSectionInstruction(
  summarizationInstructions?: CompactionSummarizationInstructions,
): string {
  const policy = summarizationInstructions?.identifierPolicy ?? "strict";
  if (policy === "off") {
    return POLICY_OFF_EXACT_IDENTIFIERS_INSTRUCTION;
  }
  if (policy === "custom") {
    const custom = summarizationInstructions?.identifierInstructions?.trim();
    if (custom) {
      const customBlock = wrapUntrustedInstructionBlock(
        "For ## Exact identifiers, apply this operator-defined policy text",
        custom,
      );
      if (customBlock) {
        return customBlock;
      }
    }
  }
  return STRICT_EXACT_IDENTIFIERS_INSTRUCTION;
}

function buildCompactionStructureInstructions(
  customInstructions?: string,
  summarizationInstructions?: CompactionSummarizationInstructions,
): string {
  const identifierSectionInstruction =
    resolveExactIdentifierSectionInstruction(summarizationInstructions);
  const sectionsTemplate = [
    "Produce a compact, factual summary with these exact section headings:",
    ...REQUIRED_SUMMARY_SECTIONS,
    identifierSectionInstruction,
    "Do not omit unresolved asks from the user.",
  ].join("\n");
  const custom = customInstructions?.trim();
  if (!custom) {
    return sectionsTemplate;
  }
  const customBlock = wrapUntrustedInstructionBlock("Additional context from /compact", custom);
  if (!customBlock) {
    return sectionsTemplate;
  }
  return `${sectionsTemplate}\n\n${customBlock}`;
}

function normalizedSummaryLines(summary: string): string[] {
  return summary
    .split(/\r?\n/u)
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
}

function hasRequiredSummarySections(summary: string): boolean {
  const lines = normalizedSummaryLines(summary);
  let cursor = 0;
  for (const heading of REQUIRED_SUMMARY_SECTIONS) {
    const index = lines.findIndex((line, lineIndex) => lineIndex >= cursor && line === heading);
    if (index < 0) {
      return false;
    }
    cursor = index + 1;
  }
  return true;
}

function buildStructuredFallbackSummary(previousSummary: string | undefined): string {
  const trimmedPreviousSummary = previousSummary?.trim() ?? "";
  if (trimmedPreviousSummary && hasRequiredSummarySections(trimmedPreviousSummary)) {
    return trimmedPreviousSummary;
  }
  return [
    "## Decisions",
    trimmedPreviousSummary || "No prior history.",
    "",
    "## Open TODOs",
    "None.",
    "",
    "## Constraints/Rules",
    "None.",
    "",
    "## Pending user asks",
    "None.",
    "",
    "## Exact identifiers",
    "None captured.",
  ].join("\n");
}

function appendSummarySection(summary: string, section: string): string {
  if (!section) {
    return summary;
  }
  if (!summary.trim()) {
    return section.trimStart();
  }
  return `${summary}${section}`;
}

/**
 * Read and format critical workspace context for compaction summary.
 * Extracts "Session Startup" and "Red Lines" from AGENTS.md.
 */
async function readWorkspaceContextForSummary(): Promise<string> {
  const MAX_SUMMARY_CONTEXT_CHARS = 2000;
  const workspaceDir = process.cwd();
  const agentsPath = path.join(workspaceDir, "AGENTS.md");

  try {
    const opened = await openBoundaryFile({
      absolutePath: agentsPath,
      rootPath: workspaceDir,
      boundaryLabel: "workspace root",
    });
    if (!opened.ok) {
      return "";
    }

    const content = (() => {
      try {
        return fs.readFileSync(opened.fd, "utf-8");
      } finally {
        fs.closeSync(opened.fd);
      }
    })();
    let sections = extractSections(content, ["Session Startup", "Red Lines"]);
    if (sections.length === 0) {
      sections = extractSections(content, ["Every Session", "Safety"]);
    }
    if (sections.length === 0) {
      return "";
    }

    const combined = sections.join("\n\n");
    const safeContent =
      combined.length > MAX_SUMMARY_CONTEXT_CHARS
        ? combined.slice(0, MAX_SUMMARY_CONTEXT_CHARS) + "\n...[truncated]..."
        : combined;

    return `\n\n<workspace-critical-rules>\n${safeContent}\n</workspace-critical-rules>`;
  } catch {
    return "";
  }
}

// ── Main extension ───────────────────────────────────────────────────────────

export default function compactionSafeguardExtension(api: ExtensionAPI): void {
  api.on("session_before_compact", async (event, ctx) => {
    const { preparation, customInstructions: eventInstructions, signal } = event;
    const hasRealSummarizable = preparation.messagesToSummarize.some(isRealConversationMessage);
    const hasRealTurnPrefix = preparation.turnPrefixMessages.some(isRealConversationMessage);
    if (!hasRealSummarizable && !hasRealTurnPrefix) {
      // Write a minimal compaction boundary to suppress re-trigger loops (#41981).
      log.info(
        "Compaction safeguard: no real conversation messages to summarize; writing compaction boundary to suppress re-trigger loop.",
      );
      const fallbackSummary = buildStructuredFallbackSummary(preparation.previousSummary);
      return {
        compaction: {
          summary: fallbackSummary,
          firstKeptEntryId: preparation.firstKeptEntryId,
          tokensBefore: preparation.tokensBefore,
        },
      };
    }
    const { readFiles, modifiedFiles } = computeFileLists(preparation.fileOps);
    const fileOpsSummary = formatFileOperations(readFiles, modifiedFiles);
    const toolFailures = collectToolFailures([
      ...preparation.messagesToSummarize,
      ...preparation.turnPrefixMessages,
    ]);
    const toolFailureSection = formatToolFailuresSection(toolFailures);

    const runtime = getCompactionSafeguardRuntime(ctx.sessionManager);
    const customInstructions = resolveCompactionInstructions(
      eventInstructions,
      runtime?.customInstructions,
    );
    const summarizationInstructions = {
      identifierPolicy: runtime?.identifierPolicy,
      identifierInstructions: runtime?.identifierInstructions,
    };
    const model = ctx.model ?? runtime?.model;
    if (!model) {
      if (!missedModelWarningSessions.has(ctx.sessionManager)) {
        missedModelWarningSessions.add(ctx.sessionManager);
        log.warn(
          "[compaction-safeguard] Both ctx.model and runtime.model are undefined. " +
            "Compaction summarization will not run.",
        );
      }
      return { cancel: true };
    }

    const apiKey = await ctx.modelRegistry.getApiKey(model);
    if (!apiKey) {
      log.warn(
        "Compaction safeguard: no API key available; cancelling compaction to preserve history.",
      );
      return { cancel: true };
    }

    try {
      const modelContextWindow = resolveContextWindowTokens(model);
      const contextWindowTokens = clampContextWindowTokens(
        runtime?.contextWindowTokens,
        modelContextWindow,
      );
      const turnPrefixMessages = preparation.turnPrefixMessages ?? [];
      let messagesToSummarize = preparation.messagesToSummarize;
      const recentTurnsPreserve = resolveRecentTurnsPreserve(runtime?.recentTurnsPreserve);
      const structuredInstructions = buildCompactionStructureInstructions(
        customInstructions,
        summarizationInstructions,
      );

      const maxHistoryShare = clampMaxHistoryShare(runtime?.maxHistoryShare);

      const tokensBefore =
        typeof preparation.tokensBefore === "number" && Number.isFinite(preparation.tokensBefore)
          ? preparation.tokensBefore
          : undefined;

      let droppedSummary: string | undefined;

      if (tokensBefore !== undefined) {
        const summarizableTokens =
          estimateMessagesTokens(messagesToSummarize) + estimateMessagesTokens(turnPrefixMessages);
        const newContentTokens = Math.max(0, Math.floor(tokensBefore - summarizableTokens));
        const maxHistoryTokens = Math.floor(contextWindowTokens * maxHistoryShare * SAFETY_MARGIN);

        if (newContentTokens > maxHistoryTokens) {
          const pruned = pruneHistoryForContextShare({
            messages: messagesToSummarize,
            maxContextTokens: contextWindowTokens,
            maxHistoryShare,
          });
          if (pruned.droppedChunks > 0) {
            log.warn(
              `Compaction safeguard: dropped ${pruned.droppedChunks} older chunk(s) ` +
                `(${pruned.droppedMessages} messages) to fit history budget.`,
            );
            messagesToSummarize = pruned.messages;

            if (pruned.droppedMessagesList.length > 0) {
              try {
                const droppedChunkRatio = computeAdaptiveChunkRatio(
                  pruned.droppedMessagesList,
                  contextWindowTokens,
                );
                const droppedMaxChunkTokens = clampMaxChunkTokens(
                  Math.max(
                    1,
                    Math.floor(contextWindowTokens * droppedChunkRatio) -
                      SUMMARIZATION_OVERHEAD_TOKENS,
                  ),
                );
                const droppedReserveTokens = clampReserveTokens(
                  Math.max(1, Math.floor(preparation.settings.reserveTokens)),
                  contextWindowTokens,
                );
                droppedSummary = await summarizeWithFallback({
                  messages: pruned.droppedMessagesList,
                  model,
                  apiKey,
                  signal,
                  reserveTokens: droppedReserveTokens,
                  maxChunkTokens: droppedMaxChunkTokens,
                  contextWindow: contextWindowTokens,
                  customInstructions: structuredInstructions,
                  summarizationInstructions,
                  previousSummary: preparation.previousSummary,
                });
              } catch (droppedError) {
                log.warn(
                  `Compaction safeguard: failed to summarize dropped messages: ${
                    droppedError instanceof Error ? droppedError.message : String(droppedError)
                  }`,
                );
              }
            }
          }
        }
      }

      const {
        summarizableMessages: summaryTargetMessages,
        preservedMessages: preservedRecentMessages,
      } = splitPreservedRecentTurns({
        messages: messagesToSummarize,
        recentTurnsPreserve,
      });
      messagesToSummarize = summaryTargetMessages;
      const preservedTurnsSection = formatPreservedTurnsSection(preservedRecentMessages);

      const allMessages = [...messagesToSummarize, ...turnPrefixMessages];
      const adaptiveRatio = computeAdaptiveChunkRatio(allMessages, contextWindowTokens);
      const maxChunkTokens = clampMaxChunkTokens(
        Math.max(
          1,
          Math.floor(contextWindowTokens * adaptiveRatio) - SUMMARIZATION_OVERHEAD_TOKENS,
        ),
      );
      const reserveTokens = clampReserveTokens(
        Math.max(1, Math.floor(preparation.settings.reserveTokens)),
        contextWindowTokens,
      );
      const effectivePreviousSummary = droppedSummary ?? preparation.previousSummary;

      const baseSummarizeOpts = {
        model,
        apiKey,
        signal,
        reserveTokens,
        maxChunkTokens,
        contextWindow: contextWindowTokens,
        summarizationInstructions,
      };

      let summary =
        messagesToSummarize.length > 0
          ? await summarizeWithFallback({
              ...baseSummarizeOpts,
              messages: messagesToSummarize,
              customInstructions: structuredInstructions,
              previousSummary: effectivePreviousSummary,
            })
          : buildStructuredFallbackSummary(effectivePreviousSummary);

      if (preparation.isSplitTurn && turnPrefixMessages.length > 0) {
        const prefixSummary = await summarizeWithFallback({
          ...baseSummarizeOpts,
          messages: turnPrefixMessages,
          customInstructions: composeSplitTurnInstructions(
            TURN_PREFIX_INSTRUCTIONS,
            structuredInstructions,
          ),
          previousSummary: undefined,
        });
        const splitTurnSection = `**Turn Context (split turn):**\n\n${prefixSummary}`;
        summary = summary.trim() ? `${summary}\n\n---\n\n${splitTurnSection}` : splitTurnSection;
      }

      summary = appendSummarySection(summary, preservedTurnsSection);
      summary = appendSummarySection(summary, toolFailureSection);
      summary = appendSummarySection(summary, fileOpsSummary);
      summary = appendSummarySection(summary, await readWorkspaceContextForSummary());

      return {
        compaction: {
          summary,
          firstKeptEntryId: preparation.firstKeptEntryId,
          tokensBefore: preparation.tokensBefore,
          details: { readFiles, modifiedFiles },
        },
      };
    } catch (error) {
      log.warn(
        `Compaction summarization failed; cancelling compaction to preserve history: ${
          error instanceof Error ? error.message : String(error)
        }`,
      );
      return { cancel: true };
    }
  });
}

export const __testing = {
  collectToolFailures,
  formatToolFailuresSection,
  splitPreservedRecentTurns,
  formatPreservedTurnsSection,
  buildCompactionStructureInstructions,
  buildStructuredFallbackSummary,
  appendSummarySection,
  resolveRecentTurnsPreserve,
  readWorkspaceContextForSummary,
  resolveCompactionInstructions,
  composeSplitTurnInstructions,
  clampMaxHistoryShare,
  clampContextWindowTokens,
  clampMaxChunkTokens,
  clampReserveTokens,
} as const;
