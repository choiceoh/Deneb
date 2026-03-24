import { Type } from "@sinclair/typebox";
import type { DenebConfig } from "../../config/config.js";
import { resolveAgentIdFromSessionKey } from "../../routing/session-key.js";
import type { AnyAgentTool } from "./common.js";
import { jsonResult } from "./common.js";

const AgentsListToolSchema = Type.Object({});

export function createAgentsListTool(opts?: {
  agentSessionKey?: string;
  config?: DenebConfig;
}): AnyAgentTool {
  return {
    label: "Agents",
    name: "agents_list",
    description: "List Deneb agent ids allowed for sessions_spawn",
    parameters: AgentsListToolSchema,
    execute: async () => {
      const { loadConfig } = await import("../../config/config.js");
      const cfg = opts?.config ?? loadConfig();
      const agentList = cfg.agents?.list ?? [];
      const requesterId = resolveAgentIdFromSessionKey(opts?.agentSessionKey) ?? "main";

      const requesterAgent = agentList.find((a) => a.id === requesterId);
      const rawAllowlist = requesterAgent?.subagents?.allowAgents ?? [];
      const allowAny = rawAllowlist.includes("*");

      const seen = new Set<string>();
      const agents: Array<{ id: string; name?: string; configured: boolean }> = [];

      // Always include the requester agent.
      seen.add(requesterId);
      agents.push({
        id: requesterId,
        name: requesterAgent?.name,
        configured: true,
      });

      if (allowAny) {
        // Include all configured agents (sorted alphabetically, excluding requester).
        for (const agent of agentList.toSorted((a, b) => a.id.localeCompare(b.id))) {
          if (seen.has(agent.id)) {
            continue;
          }
          seen.add(agent.id);
          agents.push({ id: agent.id, name: agent.name, configured: true });
        }
      } else {
        // Include only allowlisted agents.
        for (const allowedId of rawAllowlist) {
          if (seen.has(allowedId)) {
            continue;
          }
          seen.add(allowedId);
          const found = agentList.find((a) => a.id === allowedId);
          agents.push({
            id: allowedId,
            name: found?.name,
            configured: !!found,
          });
        }
      }

      return jsonResult({
        requester: requesterId,
        allowAny,
        agents,
      });
    },
  };
}
