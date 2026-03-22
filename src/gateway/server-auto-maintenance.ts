import { runAutoMaintenance } from "../commands/auto-maintenance.js";
import type { AutoMaintenanceRunOptions } from "../commands/auto-maintenance.js";
import type { DenebConfig } from "../config/config.js";
import { loadConfig } from "../config/config.js";
import { createSubsystemLogger } from "../logging/subsystem.js";

const log = createSubsystemLogger("auto-maintenance");

// Run auto-maintenance every 6 hours
const AUTO_MAINTENANCE_INTERVAL_MS = 6 * 60 * 60 * 1000;
// Delay initial run by 2 minutes after gateway start to avoid startup contention
const AUTO_MAINTENANCE_INITIAL_DELAY_MS = 2 * 60 * 1000;

type AutoMaintenanceReport = Awaited<ReturnType<typeof runAutoMaintenance>>;

export type AutoMaintenanceServiceHandle = {
  stop: () => void;
  /** Trigger an immediate run (returns the report). */
  triggerNow: (opts?: AutoMaintenanceRunOptions) => Promise<AutoMaintenanceReport>;
  /** Get the last cached report. */
  getLastReport: () => AutoMaintenanceReport | null;
};

export function startAutoMaintenanceService(_params: {
  cfg: DenebConfig;
}): AutoMaintenanceServiceHandle {
  let timer: ReturnType<typeof setTimeout> | null = null;
  let interval: ReturnType<typeof setInterval> | null = null;
  let running = false;
  let stopped = false;
  let lastReport: AutoMaintenanceReport | null = null;

  const executeRun = async (opts?: AutoMaintenanceRunOptions): Promise<AutoMaintenanceReport> => {
    if (running) {
      log.debug("auto-maintenance already running, skipping");
      if (lastReport) {
        return lastReport;
      }
      // Return a placeholder if somehow no last report
      return {
        ts: Date.now(),
        diagnostics: [],
        sessionCleanup: null,
        logCleanup: null,
        channelIssues: [],
        gatewayReachable: null,
      };
    }

    running = true;
    try {
      const cfg = opts?.cfg ?? loadConfig();
      const report = await runAutoMaintenance({
        cfg,
        dryRun: false,
        skipConnectivityCheck: true, // we're inside the gateway
        ...opts,
      });

      lastReport = report;

      const errors = report.diagnostics.filter((d) => d.severity === "error");
      const warnings = report.diagnostics.filter((d) => d.severity === "warn");
      const applied = report.diagnostics.filter((d) => d.applied);

      if (errors.length > 0 || warnings.length > 0 || applied.length > 0) {
        log.info("auto-maintenance completed", {
          errors: errors.length,
          warnings: warnings.length,
          applied: applied.length,
          sessionsPruned: report.sessionCleanup?.pruned ?? 0,
          logFilesRemoved: report.logCleanup?.removedFiles ?? 0,
        });

        // Log errors at warn level for visibility
        for (const e of errors) {
          log.warn(`[${e.category}] ${e.message}${e.action ? ` -> ${e.action}` : ""}`);
        }
      } else {
        log.debug("auto-maintenance: system healthy, no action needed");
      }

      return report;
    } catch (err) {
      log.error(`auto-maintenance failed: ${err instanceof Error ? err.message : String(err)}`);
      return {
        ts: Date.now(),
        diagnostics: [],
        sessionCleanup: null,
        logCleanup: null,
        channelIssues: [],
        gatewayReachable: null,
      };
    } finally {
      running = false;
    }
  };

  // Initial delayed run
  timer = setTimeout(() => {
    if (stopped) {
      return;
    }
    void executeRun();

    // Schedule periodic runs
    interval = setInterval(() => {
      if (stopped) {
        return;
      }
      void executeRun();
    }, AUTO_MAINTENANCE_INTERVAL_MS);
  }, AUTO_MAINTENANCE_INITIAL_DELAY_MS);

  return {
    stop: () => {
      stopped = true;
      if (timer) {
        clearTimeout(timer);
        timer = null;
      }
      if (interval) {
        clearInterval(interval);
        interval = null;
      }
    },
    triggerNow: async (opts) => {
      return await executeRun(opts);
    },
    getLastReport: () => lastReport,
  };
}
