// Pure reducers for an assistant turn's streamed parts — extracted from useChat so
// the segment/tool-chip logic is unit-testable without a React render or a live
// stream. Each takes a turn and returns the next turn (no mutation).
import type { ChatToolEvent } from "./gateway";
import type { AssistantPart, ChatTurn, ToolPart } from "./hooks";

// Stable id for a chat turn (crypto.randomUUID with a non-secure fallback).
export function chatTurnId(): string {
  return typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random()}`;
}

// Append streamed text to the turn's trailing text part, or open a new one after a
// tool chip — so prose interrupted by a tool call resumes in a fresh segment.
export function appendTextPart(turn: ChatTurn, t: string): ChatTurn {
  const parts: AssistantPart[] = [...(turn.parts ?? [])];
  const last = parts.at(-1);
  if (last?.kind === "text") parts[parts.length - 1] = { kind: "text", text: last.text + t };
  else parts.push({ kind: "text", text: t });
  return { ...turn, parts, text: turn.text + t };
}

// Insert a tool chip on `started`; flip it to its result on `completed` (same id).
export function upsertToolPart(turn: ChatTurn, ev: ChatToolEvent): ChatTurn {
  const parts: AssistantPart[] = [...(turn.parts ?? [])];
  const idx = ev.toolUseId ? parts.findIndex((p) => p.kind === "tool" && p.id === ev.toolUseId) : -1;
  const next: ToolPart = {
    kind: "tool",
    id: ev.toolUseId || `${ev.tool}-${parts.length}`,
    tool: ev.tool,
    state: ev.state || "started",
    detail: ev.detail,
    isError: ev.isError,
  };
  if (idx >= 0) parts[idx] = { ...(parts[idx] as ToolPart), ...next };
  else parts.push(next);
  return { ...turn, parts };
}
