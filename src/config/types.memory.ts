import type { SessionSendPolicyConfig } from "./types.base.js";

export type MemoryBackend = "builtin" | "qmd" | "vega";
export type MemoryCitationsMode = "auto" | "on" | "off";
export type MemoryQmdSearchMode = "query" | "search" | "vsearch";

export type MemoryConfig = {
  backend?: MemoryBackend;
  citations?: MemoryCitationsMode;
  qmd?: MemoryQmdConfig;
  vega?: MemoryVegaConfig;
};

export type MemoryVegaSearchMode = "query" | "search" | "vsearch";

export type MemoryVegaConfig = {
  /** Path to the vega binary (default: "vega") */
  command?: string;
  /** Additional directories to index (beyond defaults) */
  paths?: MemoryVegaIndexPath[];
  update?: MemoryVegaUpdateConfig;
  limits?: MemoryVegaLimitsConfig;
  scope?: SessionSendPolicyConfig;
  /** Search mode: "search" (fast FTS), "vsearch" (vector only), "query" (hybrid+rerank, default) */
  searchMode?: MemoryVegaSearchMode;
  /** Extra environment variables passed to the Vega subprocess */
  env?: Record<string, string>;
};

export type MemoryVegaIndexPath = {
  path: string;
  name?: string;
};

export type MemoryVegaUpdateConfig = {
  interval?: string;
  onBoot?: boolean;
  commandTimeoutMs?: number;
  embedInterval?: string;
};

export type MemoryVegaLimitsConfig = {
  maxResults?: number;
  maxSnippetChars?: number;
  maxInjectedChars?: number;
  timeoutMs?: number;
};

export type MemoryQmdConfig = {
  command?: string;
  mcporter?: MemoryQmdMcporterConfig;
  searchMode?: MemoryQmdSearchMode;
  includeDefaultMemory?: boolean;
  paths?: MemoryQmdIndexPath[];
  sessions?: MemoryQmdSessionConfig;
  update?: MemoryQmdUpdateConfig;
  limits?: MemoryQmdLimitsConfig;
  scope?: SessionSendPolicyConfig;
};

export type MemoryQmdMcporterConfig = {
  /**
   * Route QMD searches through mcporter (MCP runtime) instead of spawning `qmd` per query.
   * Requires:
   * - `mcporter` installed and on PATH
   * - A configured mcporter server that runs `qmd mcp` with `lifecycle: keep-alive`
   */
  enabled?: boolean;
  /** mcporter server name (defaults to "qmd") */
  serverName?: string;
  /** Start the mcporter daemon automatically (defaults to true when enabled). */
  startDaemon?: boolean;
};

export type MemoryQmdIndexPath = {
  path: string;
  name?: string;
  pattern?: string;
};

export type MemoryQmdSessionConfig = {
  enabled?: boolean;
  exportDir?: string;
  retentionDays?: number;
};

export type MemoryQmdUpdateConfig = {
  interval?: string;
  debounceMs?: number;
  onBoot?: boolean;
  waitForBootSync?: boolean;
  embedInterval?: string;
  commandTimeoutMs?: number;
  updateTimeoutMs?: number;
  embedTimeoutMs?: number;
};

export type MemoryQmdLimitsConfig = {
  maxResults?: number;
  maxSnippetChars?: number;
  maxInjectedChars?: number;
  timeoutMs?: number;
};
