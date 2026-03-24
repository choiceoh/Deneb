import { resolveAgentMaxConcurrent, resolveSubagentMaxConcurrent } from "../config/agent-limits.js";
import type { loadConfig } from "../config/config.js";
import { PERF } from "../infra/hardware-profile.js";
import { setCommandLaneConcurrency } from "../process/command-queue.js";
import { CommandLane } from "../process/lanes.js";

export function applyGatewayLaneConcurrency(cfg: ReturnType<typeof loadConfig>) {
  setCommandLaneConcurrency(CommandLane.Cron, cfg.cron?.maxConcurrentRuns ?? 1);
  setCommandLaneConcurrency(CommandLane.Main, resolveAgentMaxConcurrent(cfg));
  setCommandLaneConcurrency(CommandLane.Subagent, resolveSubagentMaxConcurrent(cfg));
  // Plugin loading is I/O-bound (network fetches, disk reads), so allow
  // higher concurrency than CPU-bound agent lanes. Cap at imageWorkerCount
  // to stay within the hardware tier's resource budget.
  setCommandLaneConcurrency(CommandLane.PluginLoad, PERF.imageWorkerCount);
}
