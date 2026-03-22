import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { autoMaintenanceCommand } from "./auto-maintenance.js";

vi.mock("../config/config.js", () => ({
  loadConfig: vi.fn(() => ({
    gateway: { mode: "local" },
  })),
}));

vi.mock("../config/paths.js", () => ({
  resolveStateDir: vi.fn(() => ""),
}));

vi.mock("../gateway/call.js", () => ({
  buildGatewayConnectionDetails: vi.fn(() => ({ message: "test" })),
  callGateway: vi.fn(() => {
    throw new Error("gateway closed");
  }),
}));

vi.mock("../infra/channels-status-issues.js", () => ({
  collectChannelStatusIssues: vi.fn(() => []),
}));

vi.mock("../channels/plugins/index.js", () => ({
  listChannelPlugins: vi.fn(() => []),
}));

vi.mock("../config/sessions.js", () => ({
  loadSessionStore: vi.fn(() => ({})),
  resolveMaintenanceConfig: vi.fn(() => ({
    mode: "enforce",
    pruneAfterMs: 7 * 24 * 60 * 60 * 1000,
    maxEntries: 200,
    maxDiskBytes: null,
    highWaterBytes: null,
    rotateBytes: 5 * 1024 * 1024,
  })),
  resolveStorePath: vi.fn(() => ""),
  pruneStaleEntries: vi.fn(() => 0),
  capEntryCount: vi.fn(() => 0),
  enforceSessionDiskBudget: vi.fn(async () => null),
  updateSessionStore: vi.fn(async () => 0),
}));

vi.mock("../agents/agent-scope.js", () => ({
  resolveDefaultAgentId: vi.fn(() => "default"),
}));

describe("autoMaintenanceCommand", () => {
  let tmpDir: string;
  let logs: string[];
  let errors: string[];
  const runtime = {
    log: (...args: unknown[]) => logs.push(args.map(String).join(" ")),
    error: (...args: unknown[]) => errors.push(args.map(String).join(" ")),
    exit: vi.fn(),
  };

  beforeEach(async () => {
    tmpDir = await fs.promises.mkdtemp(path.join(os.tmpdir(), "auto-maint-test-"));
    logs = [];
    errors = [];

    const { resolveStateDir } = await import("../config/paths.js");
    (resolveStateDir as ReturnType<typeof vi.fn>).mockReturnValue(tmpDir);

    const { resolveStorePath } = await import("../config/sessions.js");
    (resolveStorePath as ReturnType<typeof vi.fn>).mockReturnValue(
      path.join(tmpDir, "agents", "default", "sessions", "sessions.json"),
    );
  });

  afterEach(async () => {
    await fs.promises.rm(tmpDir, { recursive: true, force: true });
    vi.restoreAllMocks();
  });

  it("runs in dry-run mode and outputs JSON", async () => {
    await autoMaintenanceCommand({ json: true, dryRun: true }, runtime);
    expect(logs.length).toBeGreaterThan(0);
    const report = JSON.parse(logs[0]);
    expect(report).toHaveProperty("ts");
    expect(report).toHaveProperty("diagnostics");
    expect(report).toHaveProperty("sessionCleanup");
    expect(report).toHaveProperty("logCleanup");
    expect(report).toHaveProperty("channelIssues");
    expect(report).toHaveProperty("gatewayReachable");
  });

  it("detects stale log files in dry-run mode", async () => {
    const logsDir = path.join(tmpDir, "logs");
    await fs.promises.mkdir(logsDir, { recursive: true });
    const staleLogPath = path.join(logsDir, "gateway.log");
    await fs.promises.writeFile(staleLogPath, "x".repeat(100));
    // Make the file appear old
    const oldTime = Date.now() - 15 * 24 * 60 * 60 * 1000;
    await fs.promises.utimes(staleLogPath, new Date(oldTime), new Date(oldTime));

    await autoMaintenanceCommand({ json: true, dryRun: true }, runtime);
    const report = JSON.parse(logs[0]);
    const logDiagnostics = report.diagnostics.filter(
      (d: { category: string }) => d.category === "Logs",
    );
    expect(logDiagnostics.length).toBeGreaterThan(0);
    expect(logDiagnostics[0].message).toContain("gateway.log");
  });

  it("reports gateway unreachable", async () => {
    await autoMaintenanceCommand({ json: true, dryRun: true }, runtime);
    const report = JSON.parse(logs[0]);
    expect(report.gatewayReachable).toBe(false);
    const gwDiag = report.diagnostics.find((d: { category: string }) => d.category === "Gateway");
    expect(gwDiag).toBeDefined();
    expect(gwDiag.severity).toBe("error");
  });

  it("reports missing gateway mode", async () => {
    const { loadConfig } = await import("../config/config.js");
    (loadConfig as ReturnType<typeof vi.fn>).mockReturnValue({});

    await autoMaintenanceCommand({ json: true, dryRun: true }, runtime);
    const report = JSON.parse(logs[0]);
    const configDiag = report.diagnostics.find(
      (d: { category: string; message: string }) =>
        d.category === "Config" && d.message.includes("gateway.mode"),
    );
    expect(configDiag).toBeDefined();
    expect(configDiag.severity).toBe("warn");
  });

  it("renders text output with summary", async () => {
    await autoMaintenanceCommand({ dryRun: true }, runtime);
    const output = logs.join("\n");
    expect(output).toContain("Auto-maintenance");
  });
});
