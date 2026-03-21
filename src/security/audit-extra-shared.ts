/**
 * Shared types and helpers used by both sync and async audit-extra modules.
 */
import { resolveSandboxToolPolicyForAgent } from "../agents/sandbox/tool-policy.js";
import type { SandboxToolPolicy } from "../agents/sandbox/types.js";
import { resolveToolProfilePolicy } from "../agents/tool-policy.js";
import type { DenebConfig } from "../config/config.js";
import type { AgentToolsConfig } from "../config/types.tools.js";
import { pickSandboxToolPolicy } from "./audit-tool-policy.js";

export type SecurityAuditFinding = {
  checkId: string;
  severity: "info" | "warn" | "critical";
  title: string;
  detail: string;
  remediation?: string;
};

/**
 * Build the ordered list of tool policies that apply to a given agent context.
 * Returns only defined policies (undefined entries are filtered out).
 */
export function resolveToolPolicies(params: {
  cfg: DenebConfig;
  agentTools?: AgentToolsConfig;
  sandboxMode?: "off" | "non-main" | "all";
  agentId?: string | null;
}): SandboxToolPolicy[] {
  const policies: SandboxToolPolicy[] = [];
  const profile = params.agentTools?.profile ?? params.cfg.tools?.profile;
  const profilePolicy = resolveToolProfilePolicy(profile);
  if (profilePolicy) {
    policies.push(profilePolicy);
  }

  const globalPolicy = pickSandboxToolPolicy(params.cfg.tools ?? undefined);
  if (globalPolicy) {
    policies.push(globalPolicy);
  }

  const agentPolicy = pickSandboxToolPolicy(params.agentTools);
  if (agentPolicy) {
    policies.push(agentPolicy);
  }

  if (params.sandboxMode === "all") {
    const sandboxPolicy = resolveSandboxToolPolicyForAgent(params.cfg, params.agentId ?? undefined);
    policies.push(sandboxPolicy);
  }

  return policies;
}
