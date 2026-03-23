import { summarizeReport } from "../../../commands/auto-maintenance.js";
import { ErrorCodes, errorShape } from "../../protocol/index.js";
import type { GatewayRequestHandlers } from "../types.js";

export const autoMaintenanceHandlers: GatewayRequestHandlers = {
  "maintenance.run": async ({ params, respond, context }) => {
    const autoMaintenance = context.autoMaintenance;
    if (!autoMaintenance) {
      respond(
        false,
        undefined,
        errorShape(ErrorCodes.FEATURE_DISABLED, "auto-maintenance service not available"),
      );
      return;
    }

    const dryRun =
      params && typeof params === "object" && "dryRun" in params ? Boolean(params.dryRun) : false;

    const report = await autoMaintenance.triggerNow({ dryRun });
    respond(true, report);
  },

  "maintenance.status": async ({ respond, context }) => {
    const autoMaintenance = context.autoMaintenance;
    if (!autoMaintenance) {
      respond(
        false,
        undefined,
        errorShape(ErrorCodes.FEATURE_DISABLED, "auto-maintenance service not available"),
      );
      return;
    }

    const lastReport = autoMaintenance.getLastReport();
    if (!lastReport) {
      respond(true, { hasReport: false, message: "No maintenance report available yet." });
      return;
    }

    respond(true, {
      hasReport: true,
      report: lastReport,
      summary: summarizeReport(lastReport),
    });
  },

  "maintenance.summary": async ({ respond, context }) => {
    const autoMaintenance = context.autoMaintenance;
    if (!autoMaintenance) {
      respond(
        false,
        undefined,
        errorShape(ErrorCodes.FEATURE_DISABLED, "auto-maintenance service not available"),
      );
      return;
    }

    const lastReport = autoMaintenance.getLastReport();
    if (!lastReport) {
      // Trigger a fresh run
      const report = await autoMaintenance.triggerNow({ dryRun: true });
      respond(true, {
        summary: summarizeReport(report),
        report,
      });
      return;
    }

    respond(true, {
      summary: summarizeReport(lastReport),
      report: lastReport,
    });
  },
};
