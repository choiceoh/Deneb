import { Type } from "@sinclair/typebox";
import { stringEnum } from "../schema/typebox.js";
import { type AnyAgentTool, jsonResult } from "./common.js";
import { callGatewayTool, readGatewayCallOptions } from "./gateway.js";

const AUTO_MAINTENANCE_ACTIONS = ["run", "status", "check"] as const;

const AutoMaintenanceToolSchema = Type.Object({
  action: stringEnum(AUTO_MAINTENANCE_ACTIONS),
  dryRun: Type.Optional(Type.Boolean()),
  gatewayUrl: Type.Optional(Type.String()),
  gatewayToken: Type.Optional(Type.String()),
  timeoutMs: Type.Optional(Type.Number()),
});

export function createAutoMaintenanceTool(): AnyAgentTool {
  return {
    label: "Auto Maintenance",
    name: "auto_maintenance",
    ownerOnly: true,
    description:
      "Run system health checks and automated cleanup. " +
      "Actions: " +
      '"run" applies maintenance (cleans stale sessions, logs, detects broken connections). ' +
      '"check" previews issues without fixing (dry-run). ' +
      '"status" returns the last maintenance report. ' +
      "Use this to proactively monitor system health and fix performance issues.",
    parameters: AutoMaintenanceToolSchema,
    execute: async (_toolCallId, args) => {
      const params = args as Record<string, unknown>;
      const action = typeof params.action === "string" ? params.action : "status";
      const gatewayOpts = readGatewayCallOptions(params);

      if (action === "run") {
        const result = await callGatewayTool("maintenance.run", gatewayOpts, {
          dryRun: false,
        });
        return jsonResult({ ok: true, action: "run", result });
      }

      if (action === "check") {
        const result = await callGatewayTool("maintenance.run", gatewayOpts, {
          dryRun: true,
        });
        return jsonResult({ ok: true, action: "check", result });
      }

      if (action === "status") {
        const result = await callGatewayTool("maintenance.summary", gatewayOpts, {});
        return jsonResult({ ok: true, action: "status", result });
      }

      throw new Error(`Unknown auto_maintenance action: ${action}`);
    },
  };
}
