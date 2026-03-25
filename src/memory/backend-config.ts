import path from "node:path";
import { resolveAgentWorkspaceDir } from "../agents/agent-scope.js";
import { parseDurationMs } from "../cli/parse-duration.js";
import type { DenebConfig } from "../config/config.js";
import type { SessionSendPolicyConfig } from "../config/types.base.js";
import type {
  MemoryBackend,
  MemoryCitationsMode,
  MemoryVegaConfig,
  MemoryVegaSearchMode,
} from "../config/types.memory.js";

export type ResolvedMemoryBackendConfig = {
  backend: MemoryBackend;
  citations: MemoryCitationsMode;
  vega?: ResolvedVegaConfig;
};

// ── Vega defaults ──

export type ResolvedVegaConfig = {
  command: string;
  paths: string[];
  update: ResolvedVegaUpdateConfig;
  limits: ResolvedVegaLimitsConfig;
  scope?: SessionSendPolicyConfig;
  searchMode: MemoryVegaSearchMode;
  env: Record<string, string>;
};

export type ResolvedVegaUpdateConfig = {
  intervalMs: number;
  embedIntervalMs: number;
  onBoot: boolean;
  commandTimeoutMs: number;
};

export type ResolvedVegaLimitsConfig = {
  maxResults: number;
  maxSnippetChars: number;
  maxInjectedChars: number;
  timeoutMs: number;
};

const DEFAULT_BACKEND: MemoryBackend = "builtin";
const DEFAULT_CITATIONS: MemoryCitationsMode = "auto";

const DEFAULT_VEGA_TIMEOUT_MS = 60_000;
const DEFAULT_VEGA_MAX_RESULTS = 12; // Slightly more (up from 10)
const DEFAULT_VEGA_MAX_SNIPPET_CHARS = 2_500; // Slightly more (up from 2000)
const DEFAULT_VEGA_MAX_INJECTED_CHARS = 10_000; // Slightly more (up from 8000)
const DEFAULT_VEGA_UPDATE_INTERVAL_MS = 180_000; // 3 min (down from 5m)
const DEFAULT_VEGA_EMBED_INTERVAL_MS = 900_000; // 15 min (down from 30m)
const DEFAULT_VEGA_COMMAND_TIMEOUT_MS = 120_000;

// GPU-accelerated rerank — use query mode for best recall
const DEFAULT_VEGA_SEARCH_MODE: MemoryVegaSearchMode = "query";

function resolveIntervalMs(raw: string | undefined, fallbackMs: number): number {
  const value = raw?.trim();
  if (!value) {
    return fallbackMs;
  }
  try {
    return parseDurationMs(value, { defaultUnit: "m" });
  } catch {
    return fallbackMs;
  }
}

function resolveTimeoutMs(raw: number | undefined, fallback: number): number {
  if (typeof raw === "number" && Number.isFinite(raw) && raw > 0) {
    return Math.floor(raw);
  }
  return fallback;
}

function resolveVegaSearchMode(raw?: MemoryVegaSearchMode): MemoryVegaSearchMode {
  if (raw === "search" || raw === "vsearch" || raw === "query") {
    return raw;
  }
  return DEFAULT_VEGA_SEARCH_MODE;
}

export function resolveVegaConfig(
  cfg: MemoryVegaConfig | undefined,
  workspaceDir: string,
): ResolvedVegaConfig {
  const command = cfg?.command?.trim() || "vega";
  const paths = (cfg?.paths ?? []).map((p) =>
    path.isAbsolute(p.path) ? p.path : path.resolve(workspaceDir, p.path),
  );
  // Sanitize env: only string values, no empty keys
  const env: Record<string, string> = {};
  if (cfg?.env) {
    for (const [key, value] of Object.entries(cfg.env)) {
      const k = key.trim();
      if (k && typeof value === "string") {
        env[k] = value;
      }
    }
  }
  return {
    command,
    paths,
    update: {
      intervalMs: resolveIntervalMs(cfg?.update?.interval, DEFAULT_VEGA_UPDATE_INTERVAL_MS),
      embedIntervalMs: resolveIntervalMs(
        cfg?.update?.embedInterval,
        DEFAULT_VEGA_EMBED_INTERVAL_MS,
      ),
      onBoot: cfg?.update?.onBoot ?? true,
      commandTimeoutMs: resolveTimeoutMs(
        cfg?.update?.commandTimeoutMs,
        DEFAULT_VEGA_COMMAND_TIMEOUT_MS,
      ),
    },
    limits: {
      maxResults: cfg?.limits?.maxResults ?? DEFAULT_VEGA_MAX_RESULTS,
      maxSnippetChars: cfg?.limits?.maxSnippetChars ?? DEFAULT_VEGA_MAX_SNIPPET_CHARS,
      maxInjectedChars: cfg?.limits?.maxInjectedChars ?? DEFAULT_VEGA_MAX_INJECTED_CHARS,
      timeoutMs: resolveTimeoutMs(cfg?.limits?.timeoutMs, DEFAULT_VEGA_TIMEOUT_MS),
    },
    scope: cfg?.scope,
    searchMode: resolveVegaSearchMode(cfg?.searchMode),
    env,
  };
}

export function resolveMemoryBackendConfig(params: {
  cfg: DenebConfig;
  agentId: string;
}): ResolvedMemoryBackendConfig {
  const backend = params.cfg.memory?.backend ?? DEFAULT_BACKEND;
  const citations = params.cfg.memory?.citations ?? DEFAULT_CITATIONS;

  if (backend === "vega") {
    const workspaceDir = resolveAgentWorkspaceDir(params.cfg, params.agentId);
    return {
      backend: "vega",
      citations,
      vega: resolveVegaConfig(params.cfg.memory?.vega, workspaceDir),
    };
  }

  return { backend: "builtin", citations };
}
