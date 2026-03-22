import { PERF } from "../infra/hardware-profile.js";
import type { DenebConfig } from "./types.js";

// Defaults tuned for DGX SPARK (Grace Blackwell, 128GB unified memory)
export const DEFAULT_AGENT_MAX_CONCURRENT = PERF.agentMaxConcurrent;
export const DEFAULT_SUBAGENT_MAX_CONCURRENT = PERF.subagentMaxConcurrent;
// Keep depth-1 subagents as leaves unless config explicitly opts into nesting.
export const DEFAULT_SUBAGENT_MAX_SPAWN_DEPTH = 1;

export function resolveAgentMaxConcurrent(cfg?: DenebConfig): number {
  const raw = cfg?.agents?.defaults?.maxConcurrent;
  if (typeof raw === "number" && Number.isFinite(raw)) {
    return Math.max(1, Math.floor(raw));
  }
  return DEFAULT_AGENT_MAX_CONCURRENT;
}

export function resolveSubagentMaxConcurrent(cfg?: DenebConfig): number {
  const raw = cfg?.agents?.defaults?.subagents?.maxConcurrent;
  if (typeof raw === "number" && Number.isFinite(raw)) {
    return Math.max(1, Math.floor(raw));
  }
  return DEFAULT_SUBAGENT_MAX_CONCURRENT;
}
