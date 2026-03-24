import { describe, it, expect, afterEach, vi } from "vitest";
import type { HeadlessGatewayContext } from "./gateway-context.js";

// Mock heavy dependencies to keep tests fast and isolated.
vi.mock("../cron/service.js", () => ({
  CronService: class {
    stop() {}
  },
}));

vi.mock("../config/config.js", () => ({
  resolveStateDir: () => "/tmp/deneb-test-plugin-host",
}));

vi.mock("../gateway/server-model-catalog.js", () => ({
  loadGatewayModelCatalog: async () => [],
}));

vi.mock("../commands/health.js", () => ({
  getHealthSnapshot: async () => ({
    ok: true,
    channels: [],
    version: "test",
    ts: Date.now(),
  }),
}));

vi.mock("../commands/status.js", () => ({
  getStatusSummary: async () => ({
    gateway: { running: true },
    version: "test",
  }),
}));

describe("createHeadlessGatewayContext", () => {
  let ctx: HeadlessGatewayContext | null = null;

  afterEach(() => {
    ctx?.shutdown();
    ctx = null;
  });

  it("creates a context with invoke function", async () => {
    const { createHeadlessGatewayContext } = await import("./gateway-context.js");
    ctx = await createHeadlessGatewayContext();

    expect(ctx).toBeDefined();
    expect(typeof ctx.invoke).toBe("function");
    expect(typeof ctx.shutdown).toBe("function");
    expect(ctx.context).toBeDefined();
  });

  it("invokes health method successfully", async () => {
    const { createHeadlessGatewayContext } = await import("./gateway-context.js");
    ctx = await createHeadlessGatewayContext();

    const result = await ctx.invoke("health", {}, "test-health-1");
    expect(result.ok).toBe(true);
    expect(result.payload).toBeDefined();
  });

  it("returns NOT_FOUND for unknown methods", async () => {
    const { createHeadlessGatewayContext } = await import("./gateway-context.js");
    ctx = await createHeadlessGatewayContext();

    const result = await ctx.invoke("nonexistent.method", {}, "test-unknown-1");
    expect(result.ok).toBe(false);
    expect(result.error?.code).toBe("NOT_FOUND");
  });

  it("invokes status method", async () => {
    const { createHeadlessGatewayContext } = await import("./gateway-context.js");
    ctx = await createHeadlessGatewayContext();

    const result = await ctx.invoke("status", {}, "test-status-1");
    expect(result.ok).toBe(true);
    expect(result.payload).toBeDefined();
  });

  it("invokes config.get method without NOT_FOUND", async () => {
    const { createHeadlessGatewayContext } = await import("./gateway-context.js");
    ctx = await createHeadlessGatewayContext();

    // config.get may fail in test environment (no config file) but should
    // be a recognized handler, not a NOT_FOUND error.
    const result = await ctx.invoke("config.get", {}, "test-config-1");
    if (!result.ok) {
      expect(result.error?.code).not.toBe("NOT_FOUND");
    }
  });
});
