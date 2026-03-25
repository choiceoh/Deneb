/**
 * Private helper utilities shared across audit-extra.sync.* modules.
 *
 * Nothing in this file is exported from the package surface — all exports are
 * internal to the audit-extra.sync family.
 */
import { resolveSandboxConfigForAgent } from "../agents/sandbox/config.js";
import { isToolAllowedByPolicies } from "../agents/tool-policy-match.js";
import type { DenebConfig } from "../config/config.js";
import {
  resolveAgentModelFallbackValues,
  resolveAgentModelPrimaryValue,
} from "../config/model-input.js";
import type { AgentToolsConfig } from "../config/types.tools.js";
import {
  DEFAULT_DANGEROUS_NODE_COMMANDS,
  resolveNodeCommandAllowlist,
} from "../gateway/node-command-policy.js";
import { resolveToolPolicies } from "./audit-extra-shared.js";

export { DEFAULT_DANGEROUS_NODE_COMMANDS };

// --------------------------------------------------------------------------
// Channel / group policy helpers
// --------------------------------------------------------------------------

export function summarizeGroupPolicy(cfg: DenebConfig): {
  open: number;
  allowlist: number;
  other: number;
} {
  const channels = cfg.channels as Record<string, unknown> | undefined;
  if (!channels || typeof channels !== "object") {
    return { open: 0, allowlist: 0, other: 0 };
  }
  let open = 0;
  let allowlist = 0;
  let other = 0;
  for (const value of Object.values(channels)) {
    if (!value || typeof value !== "object") {
      continue;
    }
    const section = value as Record<string, unknown>;
    const policy = section.groupPolicy;
    if (policy === "open") {
      open += 1;
    } else if (policy === "allowlist") {
      allowlist += 1;
    } else {
      other += 1;
    }
  }
  return { open, allowlist, other };
}

export function listGroupPolicyOpen(cfg: DenebConfig): string[] {
  const out: string[] = [];
  const channels = cfg.channels as Record<string, unknown> | undefined;
  if (!channels || typeof channels !== "object") {
    return out;
  }
  for (const [channelId, value] of Object.entries(channels)) {
    if (!value || typeof value !== "object") {
      continue;
    }
    const section = value as Record<string, unknown>;
    if (section.groupPolicy === "open") {
      out.push(`channels.${channelId}.groupPolicy`);
    }
    const accounts = section.accounts;
    if (accounts && typeof accounts === "object") {
      for (const [accountId, accountVal] of Object.entries(accounts)) {
        if (!accountVal || typeof accountVal !== "object") {
          continue;
        }
        const acc = accountVal as Record<string, unknown>;
        if (acc.groupPolicy === "open") {
          out.push(`channels.${channelId}.accounts.${accountId}.groupPolicy`);
        }
      }
    }
  }
  return out;
}

function hasConfiguredGroupTargets(section: Record<string, unknown>): boolean {
  const groupKeys = ["groups", "guilds", "channels", "rooms"];
  return groupKeys.some((key) => {
    const value = section[key];
    return Boolean(value && typeof value === "object" && Object.keys(value).length > 0);
  });
}

export function listPotentialMultiUserSignals(cfg: DenebConfig): string[] {
  const out = new Set<string>();
  const channels = cfg.channels as Record<string, unknown> | undefined;
  if (!channels || typeof channels !== "object") {
    return [];
  }

  const inspectSection = (section: Record<string, unknown>, basePath: string) => {
    const groupPolicy = typeof section.groupPolicy === "string" ? section.groupPolicy : null;
    if (groupPolicy === "open") {
      out.add(`${basePath}.groupPolicy="open"`);
    } else if (groupPolicy === "allowlist" && hasConfiguredGroupTargets(section)) {
      out.add(`${basePath}.groupPolicy="allowlist" with configured group targets`);
    }

    const dmPolicy = typeof section.dmPolicy === "string" ? section.dmPolicy : null;
    if (dmPolicy === "open") {
      out.add(`${basePath}.dmPolicy="open"`);
    }

    const allowFrom = Array.isArray(section.allowFrom) ? section.allowFrom : [];
    if (allowFrom.some((entry) => String(entry).trim() === "*")) {
      out.add(`${basePath}.allowFrom includes "*"`);
    }

    const groupAllowFrom = Array.isArray(section.groupAllowFrom) ? section.groupAllowFrom : [];
    if (groupAllowFrom.some((entry) => String(entry).trim() === "*")) {
      out.add(`${basePath}.groupAllowFrom includes "*"`);
    }

    const dm = section.dm;
    if (dm && typeof dm === "object") {
      const dmSection = dm as Record<string, unknown>;
      const dmLegacyPolicy = typeof dmSection.policy === "string" ? dmSection.policy : null;
      if (dmLegacyPolicy === "open") {
        out.add(`${basePath}.dm.policy="open"`);
      }
      const dmAllowFrom = Array.isArray(dmSection.allowFrom) ? dmSection.allowFrom : [];
      if (dmAllowFrom.some((entry) => String(entry).trim() === "*")) {
        out.add(`${basePath}.dm.allowFrom includes "*"`);
      }
    }
  };

  for (const [channelId, value] of Object.entries(channels)) {
    if (!value || typeof value !== "object") {
      continue;
    }
    const section = value as Record<string, unknown>;
    inspectSection(section, `channels.${channelId}`);
    const accounts = section.accounts;
    if (!accounts || typeof accounts !== "object") {
      continue;
    }
    for (const [accountId, accountValue] of Object.entries(accounts)) {
      if (!accountValue || typeof accountValue !== "object") {
        continue;
      }
      inspectSection(
        accountValue as Record<string, unknown>,
        `channels.${channelId}.accounts.${accountId}`,
      );
    }
  }

  return Array.from(out);
}

// --------------------------------------------------------------------------
// Gateway exposure helpers
// --------------------------------------------------------------------------

export function isGatewayRemotelyExposed(cfg: DenebConfig): boolean {
  const bind = typeof cfg.gateway?.bind === "string" ? cfg.gateway.bind : "loopback";
  if (bind !== "loopback") {
    return true;
  }
  const tailscaleMode = cfg.gateway?.tailscale?.mode ?? "off";
  return tailscaleMode === "serve" || tailscaleMode === "funnel";
}

// --------------------------------------------------------------------------
// Config value helpers
// --------------------------------------------------------------------------

export function isProbablySyncedPath(p: string): boolean {
  const s = p.toLowerCase();
  return (
    s.includes("icloud") ||
    s.includes("dropbox") ||
    s.includes("google drive") ||
    s.includes("googledrive") ||
    s.includes("onedrive")
  );
}

export function looksLikeEnvRef(value: string): boolean {
  const v = value.trim();
  return v.startsWith("${") && v.endsWith("}");
}

// --------------------------------------------------------------------------
// Model helpers
// --------------------------------------------------------------------------

export type ModelRef = { id: string; source: string };

function addModel(models: ModelRef[], raw: unknown, source: string) {
  if (typeof raw !== "string") {
    return;
  }
  const id = raw.trim();
  if (!id) {
    return;
  }
  models.push({ id, source });
}

export function collectModels(cfg: DenebConfig): ModelRef[] {
  const out: ModelRef[] = [];
  addModel(
    out,
    resolveAgentModelPrimaryValue(cfg.agents?.defaults?.model),
    "agents.defaults.model.primary",
  );
  for (const f of resolveAgentModelFallbackValues(cfg.agents?.defaults?.model)) {
    addModel(out, f, "agents.defaults.model.fallbacks");
  }
  addModel(
    out,
    resolveAgentModelPrimaryValue(cfg.agents?.defaults?.imageModel),
    "agents.defaults.imageModel.primary",
  );
  for (const f of resolveAgentModelFallbackValues(cfg.agents?.defaults?.imageModel)) {
    addModel(out, f, "agents.defaults.imageModel.fallbacks");
  }

  const list = Array.isArray(cfg.agents?.list) ? cfg.agents?.list : [];
  for (const agent of list ?? []) {
    if (!agent || typeof agent !== "object") {
      continue;
    }
    const id =
      typeof (agent as { id?: unknown }).id === "string" ? (agent as { id: string }).id : "";
    const model = (agent as { model?: unknown }).model;
    if (typeof model === "string") {
      addModel(out, model, `agents.list.${id}.model`);
    } else if (model && typeof model === "object") {
      addModel(out, (model as { primary?: unknown }).primary, `agents.list.${id}.model.primary`);
      const fallbacks = (model as { fallbacks?: unknown }).fallbacks;
      if (Array.isArray(fallbacks)) {
        for (const f of fallbacks) {
          addModel(out, f, `agents.list.${id}.model.fallbacks`);
        }
      }
    }
  }
  return out;
}

export function extractAgentIdFromSource(source: string): string | null {
  const match = source.match(/^agents\.list\.([^.]*)\./);
  return match?.[1] ?? null;
}

export function isGptModel(id: string): boolean {
  return /\bgpt-/i.test(id);
}

export function isGpt5OrHigher(id: string): boolean {
  return /\bgpt-5(?:\b|[.-])/i.test(id);
}

export function isClaudeModel(id: string): boolean {
  return /\bclaude-/i.test(id);
}

export function isClaude45OrHigher(id: string): boolean {
  // Match claude-*-4-5+, claude-*-45+, claude-*4.5+, or future 5.x+ majors.
  return /\bclaude-[^\s/]*?(?:-4-?(?:[5-9]|[1-9]\d)\b|4\.(?:[5-9]|[1-9]\d)\b|-[5-9](?:\b|[.-]))/i.test(
    id,
  );
}

// --------------------------------------------------------------------------
// Web tool helpers
// --------------------------------------------------------------------------

export function hasWebSearchKey(cfg: DenebConfig, env: NodeJS.ProcessEnv): boolean {
  const search = cfg.tools?.web?.search;
  return Boolean(
    search?.apiKey || search?.perplexity?.apiKey || env.BRAVE_API_KEY || env.PERPLEXITY_API_KEY,
  );
}

export function isWebSearchEnabled(cfg: DenebConfig, env: NodeJS.ProcessEnv): boolean {
  const enabled = cfg.tools?.web?.search?.enabled;
  if (enabled === false) {
    return false;
  }
  if (enabled === true) {
    return true;
  }
  return hasWebSearchKey(cfg, env);
}

export function isWebFetchEnabled(cfg: DenebConfig): boolean {
  const enabled = cfg.tools?.web?.fetch?.enabled;
  if (enabled === false) {
    return false;
  }
  return true;
}

export function isBrowserEnabled(_cfg: DenebConfig): boolean {
  return false;
}

// --------------------------------------------------------------------------
// Node command helpers
// --------------------------------------------------------------------------

export function normalizeNodeCommand(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

export function listKnownNodeCommands(cfg: DenebConfig): Set<string> {
  const baseCfg: DenebConfig = {
    ...cfg,
    gateway: {
      ...cfg.gateway,
      nodes: {
        ...cfg.gateway?.nodes,
        denyCommands: [],
      },
    },
  };
  const out = new Set<string>();
  for (const platform of ["linux", "windows", "unknown"]) {
    const allow = resolveNodeCommandAllowlist(baseCfg, { platform });
    for (const cmd of allow) {
      const normalized = normalizeNodeCommand(cmd);
      if (normalized) {
        out.add(normalized);
      }
    }
  }
  return out;
}

export function looksLikeNodeCommandPattern(value: string): boolean {
  if (!value) {
    return false;
  }
  if (/[?*[\]{}(),|]/.test(value)) {
    return true;
  }
  if (
    value.startsWith("/") ||
    value.endsWith("/") ||
    value.startsWith("^") ||
    value.endsWith("$")
  ) {
    return true;
  }
  return /\s/.test(value) || value.includes("group:");
}

function editDistance(a: string, b: string): number {
  if (a === b) {
    return 0;
  }
  if (!a) {
    return b.length;
  }
  if (!b) {
    return a.length;
  }

  const dp: number[] = Array.from({ length: b.length + 1 }, (_, j) => j);

  for (let i = 1; i <= a.length; i++) {
    let prev = dp[0];
    dp[0] = i;
    for (let j = 1; j <= b.length; j++) {
      const temp = dp[j];
      const cost = a[i - 1] === b[j - 1] ? 0 : 1;
      dp[j] = Math.min(dp[j] + 1, dp[j - 1] + 1, prev + cost);
      prev = temp;
    }
  }

  return dp[b.length];
}

export function suggestKnownNodeCommands(unknown: string, known: Set<string>): string[] {
  const needle = unknown.trim();
  if (!needle) {
    return [];
  }

  // Fast path: prefix-ish suggestions.
  const prefix = needle.includes(".") ? needle.split(".").slice(0, 2).join(".") : needle;
  const prefixHits = Array.from(known)
    .filter((cmd) => cmd.startsWith(prefix))
    .slice(0, 3);
  if (prefixHits.length > 0) {
    return prefixHits;
  }

  // Fuzzy: Levenshtein over a small-ish known set.
  const ranked = Array.from(known)
    .map((cmd) => ({ cmd, d: editDistance(needle, cmd) }))
    .toSorted((a, b) => a.d - b.d || a.cmd.localeCompare(b.cmd));

  const best = ranked[0]?.d ?? Infinity;
  const threshold = Math.max(2, Math.min(4, best));
  return ranked
    .filter((r) => r.d <= threshold)
    .slice(0, 3)
    .map((r) => r.cmd);
}

// --------------------------------------------------------------------------
// Tool exposure helpers (used by exposure + models modules)
// --------------------------------------------------------------------------

export function collectRiskyToolExposureContexts(cfg: DenebConfig): {
  riskyContexts: string[];
  hasRuntimeRisk: boolean;
} {
  const contexts: Array<{
    label: string;
    agentId?: string;
    tools?: AgentToolsConfig;
  }> = [{ label: "agents.defaults" }];
  for (const agent of cfg.agents?.list ?? []) {
    if (!agent || typeof agent !== "object" || typeof agent.id !== "string") {
      continue;
    }
    contexts.push({
      label: `agents.list.${agent.id}`,
      agentId: agent.id,
      tools: agent.tools,
    });
  }

  const riskyContexts: string[] = [];
  let hasRuntimeRisk = false;
  for (const context of contexts) {
    const sandboxMode = resolveSandboxConfigForAgent(cfg, context.agentId).mode;
    const policies = resolveToolPolicies({
      cfg,
      agentTools: context.tools,
      sandboxMode,
      agentId: context.agentId ?? null,
    });
    const runtimeTools = ["exec", "process"].filter((tool) =>
      isToolAllowedByPolicies(tool, policies),
    );
    const fsTools = ["read", "write", "edit", "apply_patch"].filter((tool) =>
      isToolAllowedByPolicies(tool, policies),
    );
    const fsWorkspaceOnly = context.tools?.fs?.workspaceOnly ?? cfg.tools?.fs?.workspaceOnly;
    const runtimeUnguarded = runtimeTools.length > 0 && sandboxMode !== "all";
    const fsUnguarded = fsTools.length > 0 && sandboxMode !== "all" && fsWorkspaceOnly !== true;
    if (!runtimeUnguarded && !fsUnguarded) {
      continue;
    }
    if (runtimeUnguarded) {
      hasRuntimeRisk = true;
    }
    riskyContexts.push(
      `${context.label} (sandbox=${sandboxMode}; runtime=[${runtimeTools.join(", ") || "off"}]; fs=[${fsTools.join(", ") || "off"}]; fs.workspaceOnly=${
        fsWorkspaceOnly === true ? "true" : "false"
      })`,
    );
  }

  return { riskyContexts, hasRuntimeRisk };
}

// --------------------------------------------------------------------------
// Docker config helpers
// --------------------------------------------------------------------------

export function hasConfiguredDockerConfig(
  docker: Record<string, unknown> | undefined | null,
): docker is Record<string, unknown> {
  if (!docker || typeof docker !== "object") {
    return false;
  }
  return Object.values(docker).some((value) => value !== undefined);
}
