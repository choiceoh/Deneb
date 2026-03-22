import fs from "node:fs";
import path from "node:path";
import { resolveDefaultAgentId } from "../agents/agent-scope.js";
import { listChannelPlugins } from "../channels/plugins/index.js";
import { withProgress } from "../cli/progress.js";
import type { DenebConfig } from "../config/config.js";
import { loadConfig } from "../config/config.js";
import { resolveStateDir } from "../config/paths.js";
import {
  enforceSessionDiskBudget,
  loadSessionStore,
  pruneStaleEntries,
  capEntryCount,
  resolveMaintenanceConfig,
  resolveStorePath,
  updateSessionStore,
} from "../config/sessions.js";
import { callGateway } from "../gateway/call.js";
import { collectChannelStatusIssues } from "../infra/channels-status-issues.js";
import type { RuntimeEnv } from "../runtime.js";
import { note } from "../terminal/note.js";
import { isRich, theme } from "../terminal/theme.js";

export type AutoMaintenanceOptions = {
  json?: boolean;
  dryRun?: boolean;
  verbose?: boolean;
  nonInteractive?: boolean;
};

// -- Types ------------------------------------------------------------------

type DiagnosticSeverity = "info" | "warn" | "error";

type DiagnosticEntry = {
  category: string;
  severity: DiagnosticSeverity;
  message: string;
  action?: string;
  applied?: boolean;
};

type AutoMaintenanceReport = {
  ts: number;
  diagnostics: DiagnosticEntry[];
  sessionCleanup: SessionCleanupStats | null;
  logCleanup: LogCleanupStats | null;
  channelIssues: ChannelIssue[];
  gatewayReachable: boolean | null;
};

type SessionCleanupStats = {
  storePath: string;
  beforeCount: number;
  afterCount: number;
  pruned: number;
  capped: number;
  diskBudgetApplied: boolean;
  diskBudgetFreedBytes: number;
};

type LogCleanupStats = {
  scannedDirs: string[];
  removedFiles: number;
  freedBytes: number;
};

type ChannelIssue = {
  channel: string;
  accountId?: string;
  message: string;
  fix?: string;
};

// -- Constants --------------------------------------------------------------

const LOG_FILE_EXTENSIONS = new Set([".log", ".log.1", ".log.2", ".log.3"]);
const STALE_LOG_AGE_MS = 14 * 24 * 60 * 60 * 1000; // 14 days
const LARGE_LOG_BYTES = 50 * 1024 * 1024; // 50 MB
const LARGE_SESSION_STORE_ENTRIES = 500;
const LARGE_SESSION_DIR_BYTES = 200 * 1024 * 1024; // 200 MB

// -- Helpers ----------------------------------------------------------------

function formatBytes(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes}B`;
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)}KB`;
  }
  if (bytes < 1024 * 1024 * 1024) {
    return `${(bytes / (1024 * 1024)).toFixed(1)}MB`;
  }
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)}GB`;
}

function severityIcon(severity: DiagnosticSeverity, rich: boolean): string {
  if (!rich) {
    return severity === "error" ? "[ERR]" : severity === "warn" ? "[WRN]" : "[INF]";
  }
  return severity === "error"
    ? theme.error("[ERR]")
    : severity === "warn"
      ? theme.warn("[WRN]")
      : theme.muted("[INF]");
}

// -- Session cleanup --------------------------------------------------------

async function cleanupSessions(params: {
  cfg: DenebConfig;
  dryRun: boolean;
  diagnostics: DiagnosticEntry[];
}): Promise<SessionCleanupStats | null> {
  const { cfg, dryRun, diagnostics } = params;
  const maintenance = resolveMaintenanceConfig();
  const storePath = resolveStorePath(cfg.session?.store, {
    agentId: resolveDefaultAgentId(cfg),
  });

  if (!fs.existsSync(storePath)) {
    return null;
  }

  const beforeStore = loadSessionStore(storePath, { skipCache: true });
  const beforeCount = Object.keys(beforeStore).length;

  // Check if session count is high
  if (beforeCount > LARGE_SESSION_STORE_ENTRIES) {
    diagnostics.push({
      category: "Sessions",
      severity: "warn",
      message: `Session store has ${beforeCount} entries (threshold: ${LARGE_SESSION_STORE_ENTRIES}).`,
      action: "Pruning stale/overflow entries.",
    });
  }

  // Check sessions dir total size
  const sessionsDir = path.dirname(storePath);
  let dirTotalBytes = 0;
  try {
    const entries = await fs.promises.readdir(sessionsDir, { withFileTypes: true });
    for (const entry of entries) {
      if (!entry.isFile()) {
        continue;
      }
      try {
        const stat = await fs.promises.stat(path.join(sessionsDir, entry.name));
        dirTotalBytes += stat.size;
      } catch {
        // skip unreadable files
      }
    }
  } catch {
    // sessions dir unreadable
  }

  if (dirTotalBytes > LARGE_SESSION_DIR_BYTES) {
    diagnostics.push({
      category: "Sessions",
      severity: "warn",
      message: `Session directory is ${formatBytes(dirTotalBytes)} (threshold: ${formatBytes(LARGE_SESSION_DIR_BYTES)}).`,
      action: "Enforcing disk budget.",
    });
  }

  if (dryRun) {
    // Preview only
    const previewStore = structuredClone(beforeStore);
    const pruned = pruneStaleEntries(previewStore, maintenance.pruneAfterMs, { log: false });
    const capped = capEntryCount(previewStore, maintenance.maxEntries, { log: false });
    const diskBudget = await enforceSessionDiskBudget({
      store: previewStore,
      storePath,
      maintenance,
      warnOnly: false,
      dryRun: true,
    });

    return {
      storePath,
      beforeCount,
      afterCount: Object.keys(previewStore).length,
      pruned,
      capped,
      diskBudgetApplied:
        (diskBudget?.removedEntries ?? 0) > 0 || (diskBudget?.removedFiles ?? 0) > 0,
      diskBudgetFreedBytes: diskBudget?.freedBytes ?? 0,
    };
  }

  // Apply cleanup
  let pruned = 0;
  let capped = 0;
  let diskBudgetFreedBytes = 0;
  let diskBudgetApplied = false;

  await updateSessionStore(
    storePath,
    (store) => {
      pruned = pruneStaleEntries(store, maintenance.pruneAfterMs, { log: false });
      capped = capEntryCount(store, maintenance.maxEntries, { log: false });
      return pruned + capped;
    },
    {
      maintenanceOverride: { mode: "enforce" },
    },
  );

  const afterStore = loadSessionStore(storePath, { skipCache: true });
  const afterCount = Object.keys(afterStore).length;

  if (pruned > 0 || capped > 0) {
    diagnostics.push({
      category: "Sessions",
      severity: "info",
      message: `Cleaned ${pruned} stale + ${capped} overflow entries (${beforeCount} -> ${afterCount}).`,
      applied: true,
    });
  }

  return {
    storePath,
    beforeCount,
    afterCount,
    pruned,
    capped,
    diskBudgetApplied,
    diskBudgetFreedBytes,
  };
}

// -- Log cleanup ------------------------------------------------------------

async function cleanupLogs(params: {
  stateDir: string;
  dryRun: boolean;
  diagnostics: DiagnosticEntry[];
}): Promise<LogCleanupStats> {
  const { stateDir, dryRun, diagnostics } = params;
  const scannedDirs: string[] = [];
  let removedFiles = 0;
  let freedBytes = 0;
  const now = Date.now();

  // Scan the state directory for log files
  const logDirs = [stateDir, path.join(stateDir, "logs")];

  for (const dir of logDirs) {
    if (!fs.existsSync(dir)) {
      continue;
    }
    scannedDirs.push(dir);

    let entries: fs.Dirent[];
    try {
      entries = await fs.promises.readdir(dir, { withFileTypes: true });
    } catch {
      continue;
    }

    for (const entry of entries) {
      if (!entry.isFile()) {
        continue;
      }
      const ext = path.extname(entry.name);
      const isLogFile = LOG_FILE_EXTENSIONS.has(ext) || entry.name.endsWith(".log");
      if (!isLogFile) {
        continue;
      }

      const filePath = path.join(dir, entry.name);
      let stat: fs.Stats;
      try {
        stat = await fs.promises.stat(filePath);
      } catch {
        continue;
      }

      const ageMs = now - stat.mtimeMs;
      const isStale = ageMs > STALE_LOG_AGE_MS;
      const isLarge = stat.size > LARGE_LOG_BYTES;

      if (isStale || isLarge) {
        const reason = isStale
          ? `stale (${Math.round(ageMs / (24 * 60 * 60 * 1000))}d old)`
          : `large (${formatBytes(stat.size)})`;

        if (dryRun) {
          diagnostics.push({
            category: "Logs",
            severity: "warn",
            message: `Would remove ${entry.name}: ${reason}.`,
          });
        } else {
          try {
            await fs.promises.rm(filePath, { force: true });
            removedFiles += 1;
            freedBytes += stat.size;
            diagnostics.push({
              category: "Logs",
              severity: "info",
              message: `Removed ${entry.name}: ${reason}.`,
              applied: true,
            });
          } catch {
            diagnostics.push({
              category: "Logs",
              severity: "error",
              message: `Failed to remove ${entry.name}.`,
            });
          }
        }
      }
    }
  }

  return { scannedDirs, removedFiles, freedBytes };
}

// -- Channel connectivity check ---------------------------------------------

async function checkChannelConnectivity(params: {
  cfg: DenebConfig;
  diagnostics: DiagnosticEntry[];
  timeoutMs: number;
}): Promise<{ issues: ChannelIssue[]; gatewayReachable: boolean }> {
  const { cfg, diagnostics, timeoutMs } = params;
  const issues: ChannelIssue[] = [];

  // Check gateway reachability
  let gatewayReachable = false;
  try {
    await callGateway({
      method: "health",
      timeoutMs,
      config: cfg,
    });
    gatewayReachable = true;
  } catch {
    diagnostics.push({
      category: "Gateway",
      severity: "error",
      message: "Gateway is not reachable.",
      action: "Start the gateway or check connection settings.",
    });
    return { issues, gatewayReachable: false };
  }

  // Probe channels for issues
  try {
    const status = await callGateway({
      method: "channels.status",
      params: { probe: true, timeoutMs: Math.min(timeoutMs, 5000) },
      timeoutMs: Math.min(timeoutMs + 1000, 6000),
      config: cfg,
    });
    const channelIssues = collectChannelStatusIssues(status);
    for (const issue of channelIssues) {
      const entry: ChannelIssue = {
        channel: issue.channel,
        accountId: issue.accountId,
        message: issue.message,
        fix: issue.fix,
      };
      issues.push(entry);
      diagnostics.push({
        category: "Channels",
        severity: "warn",
        message: `${issue.channel}${issue.accountId ? `:${issue.accountId}` : ""}: ${issue.message}`,
        action: issue.fix,
      });
    }
  } catch {
    diagnostics.push({
      category: "Channels",
      severity: "warn",
      message: "Could not probe channel status.",
    });
  }

  // Check for unconfigured channels (channels that are registered but not set up)
  const plugins = listChannelPlugins();
  for (const plugin of plugins) {
    const accountIds = plugin.config.listAccountIds(cfg);
    if (accountIds.length === 0) {
      continue;
    }

    for (const accountId of accountIds) {
      try {
        const account = plugin.config.resolveAccount(cfg, accountId);
        if (!account) {
          diagnostics.push({
            category: "Channels",
            severity: "warn",
            message: `${plugin.meta.label ?? plugin.id}:${accountId}: account configured but could not resolve.`,
            action: `Run "deneb configure" to fix.`,
          });
        } else if (plugin.config.isConfigured) {
          const configured = await plugin.config.isConfigured(account, cfg);
          if (!configured) {
            diagnostics.push({
              category: "Channels",
              severity: "warn",
              message: `${plugin.meta.label ?? plugin.id}:${accountId}: incomplete configuration.`,
              action: `Run "deneb configure" to complete setup.`,
            });
          }
        }
      } catch {
        // ignore resolution errors during auto-maintenance
      }
    }
  }

  return { issues, gatewayReachable };
}

// -- State integrity checks -------------------------------------------------

function checkStateIntegrity(params: {
  stateDir: string;
  cfg: DenebConfig;
  diagnostics: DiagnosticEntry[];
}): void {
  const { stateDir, cfg, diagnostics } = params;

  // Check if state directory exists
  if (!fs.existsSync(stateDir)) {
    diagnostics.push({
      category: "State",
      severity: "error",
      message: `State directory does not exist: ${stateDir}`,
      action: `Run "deneb setup" to initialize.`,
    });
    return;
  }

  // Check config file validity
  const configPath = path.join(stateDir, "deneb.json");
  if (fs.existsSync(configPath)) {
    try {
      const raw = fs.readFileSync(configPath, "utf-8");
      JSON.parse(raw);
    } catch {
      diagnostics.push({
        category: "Config",
        severity: "error",
        message: "Config file is invalid JSON.",
        action: `Run "deneb doctor --fix" to repair.`,
      });
    }
  }

  // Check gateway mode
  if (!cfg.gateway?.mode) {
    diagnostics.push({
      category: "Config",
      severity: "warn",
      message: "gateway.mode is not set; gateway start will be blocked.",
      action: `Run "deneb config set gateway.mode local".`,
    });
  }

  // Check credentials directory
  const credsDir = path.join(stateDir, "credentials");
  if (fs.existsSync(credsDir)) {
    try {
      const stat = fs.statSync(credsDir);
      if (process.platform !== "win32") {
        const mode = stat.mode & 0o777;
        if ((mode & 0o077) !== 0) {
          diagnostics.push({
            category: "Security",
            severity: "warn",
            message: `Credentials directory has overly permissive permissions (${mode.toString(8)}).`,
            action: `Run: chmod 700 ${credsDir}`,
          });
        }
      }
    } catch {
      // skip
    }
  }

  // Check for lock files that might be stale
  const lockDir = path.join(stateDir, "locks");
  if (fs.existsSync(lockDir)) {
    try {
      const entries = fs.readdirSync(lockDir);
      const staleLockAge = 30 * 60 * 1000; // 30 minutes
      for (const entry of entries) {
        const lockPath = path.join(lockDir, entry);
        try {
          const stat = fs.statSync(lockPath);
          if (Date.now() - stat.mtimeMs > staleLockAge) {
            diagnostics.push({
              category: "State",
              severity: "warn",
              message: `Stale lock file: ${entry} (${Math.round((Date.now() - stat.mtimeMs) / 60000)}m old).`,
              action: "May indicate a crashed process. Safe to remove if gateway is not running.",
            });
          }
        } catch {
          // skip
        }
      }
    } catch {
      // skip
    }
  }
}

// -- Output formatting ------------------------------------------------------

function renderReport(params: {
  report: AutoMaintenanceReport;
  runtime: RuntimeEnv;
  dryRun: boolean;
  verbose: boolean;
}): void {
  const { report, runtime, dryRun, verbose } = params;
  const rich = isRich();
  const prefix = dryRun ? "[dry-run] " : "";

  if (report.diagnostics.length === 0) {
    runtime.log(
      rich
        ? theme.accent(`${prefix}Auto-maintenance: everything looks healthy.`)
        : `${prefix}Auto-maintenance: everything looks healthy.`,
    );
    return;
  }

  const grouped = new Map<string, DiagnosticEntry[]>();
  for (const entry of report.diagnostics) {
    const existing = grouped.get(entry.category) ?? [];
    existing.push(entry);
    grouped.set(entry.category, existing);
  }

  for (const [category, entries] of grouped) {
    const lines = entries
      .filter((e) => verbose || e.severity !== "info" || e.applied)
      .map((e) => {
        const icon = severityIcon(e.severity, rich);
        const msg = e.message;
        const action = e.action ? ` -> ${e.action}` : "";
        const appliedTag = e.applied ? " [applied]" : "";
        return `${icon} ${msg}${action}${appliedTag}`;
      });
    if (lines.length > 0) {
      note(lines.join("\n"), `${prefix}${category}`);
    }
  }

  // Summary
  const errors = report.diagnostics.filter((d) => d.severity === "error").length;
  const warnings = report.diagnostics.filter((d) => d.severity === "warn").length;
  const applied = report.diagnostics.filter((d) => d.applied).length;

  const parts: string[] = [];
  if (errors > 0) {
    parts.push(`${errors} error${errors > 1 ? "s" : ""}`);
  }
  if (warnings > 0) {
    parts.push(`${warnings} warning${warnings > 1 ? "s" : ""}`);
  }
  if (applied > 0) {
    parts.push(`${applied} fix${applied > 1 ? "es" : ""} applied`);
  }

  if (parts.length > 0) {
    runtime.log(
      rich
        ? theme.accent(`${prefix}Auto-maintenance summary: ${parts.join(", ")}.`)
        : `${prefix}Auto-maintenance summary: ${parts.join(", ")}.`,
    );
  }

  if (
    report.sessionCleanup &&
    (report.sessionCleanup.pruned > 0 || report.sessionCleanup.capped > 0)
  ) {
    runtime.log(
      `Sessions: ${report.sessionCleanup.beforeCount} -> ${report.sessionCleanup.afterCount} entries.`,
    );
  }
  if (report.logCleanup && report.logCleanup.removedFiles > 0) {
    runtime.log(
      `Logs: removed ${report.logCleanup.removedFiles} file${report.logCleanup.removedFiles > 1 ? "s" : ""}, freed ${formatBytes(report.logCleanup.freedBytes)}.`,
    );
  }
}

// -- Main command -----------------------------------------------------------

export async function autoMaintenanceCommand(
  opts: AutoMaintenanceOptions,
  runtime: RuntimeEnv,
): Promise<void> {
  const cfg = loadConfig();
  const stateDir = resolveStateDir();
  const dryRun = Boolean(opts.dryRun);
  const verbose = Boolean(opts.verbose);
  const timeoutMs = 10_000;

  const diagnostics: DiagnosticEntry[] = [];

  // Run all checks with progress
  const report = await withProgress(
    {
      label: "Running auto-maintenance...",
      indeterminate: true,
      enabled: opts.json !== true,
    },
    async (progress) => {
      // 1. State integrity
      progress.setLabel("Checking state integrity...");
      checkStateIntegrity({ stateDir, cfg, diagnostics });

      // 2. Session cleanup
      progress.setLabel("Cleaning up sessions...");
      const sessionCleanup = await cleanupSessions({ cfg, dryRun, diagnostics });

      // 3. Log cleanup
      progress.setLabel("Cleaning up logs...");
      const logCleanup = await cleanupLogs({ stateDir, dryRun, diagnostics });

      // 4. Channel + gateway connectivity
      progress.setLabel("Checking channel connectivity...");
      const { issues: channelIssues, gatewayReachable } = await checkChannelConnectivity({
        cfg,
        diagnostics,
        timeoutMs,
      });

      return {
        ts: Date.now(),
        diagnostics,
        sessionCleanup,
        logCleanup,
        channelIssues,
        gatewayReachable,
      } satisfies AutoMaintenanceReport;
    },
  );

  if (opts.json) {
    runtime.log(JSON.stringify(report, null, 2));
    return;
  }

  renderReport({ report, runtime, dryRun, verbose });
}
