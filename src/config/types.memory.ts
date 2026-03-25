import type { SessionSendPolicyConfig } from "./types.base.js";

export type MemoryBackend = "builtin" | "vega";
export type MemoryCitationsMode = "auto" | "on" | "off";

export type MemoryConfig = {
  backend?: MemoryBackend;
  citations?: MemoryCitationsMode;
  vega?: MemoryVegaConfig;
};

export type MemoryVegaSearchMode = "query" | "search" | "vsearch";

export type MemoryVegaConfig = {
  /** Additional directories to index (beyond defaults) */
  paths?: MemoryVegaIndexPath[];
  update?: MemoryVegaUpdateConfig;
  limits?: MemoryVegaLimitsConfig;
  scope?: SessionSendPolicyConfig;
  /** Search mode: "search" (fast FTS), "vsearch" (vector only), "query" (hybrid+rerank, default) */
  searchMode?: MemoryVegaSearchMode;
  /** Extra environment variables forwarded to the Vega runtime via gateway RPC */
  env?: Record<string, string>;
};

export type MemoryVegaIndexPath = {
  path: string;
  name?: string;
};

export type MemoryVegaUpdateConfig = {
  interval?: string;
  onBoot?: boolean;
  embedInterval?: string;
};

export type MemoryVegaLimitsConfig = {
  maxResults?: number;
  maxSnippetChars?: number;
  maxInjectedChars?: number;
  timeoutMs?: number;
};
