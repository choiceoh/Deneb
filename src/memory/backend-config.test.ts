import { describe, expect, it } from "vitest";
import type { DenebConfig } from "../config/config.js";
import { resolveMemoryBackendConfig } from "./backend-config.js";

describe("resolveMemoryBackendConfig", () => {
  it("defaults to builtin backend when config missing", () => {
    const cfg = { agents: { defaults: { workspace: "/tmp/memory-test" } } } as DenebConfig;
    const resolved = resolveMemoryBackendConfig({ cfg, agentId: "main" });
    expect(resolved.backend).toBe("builtin");
    expect(resolved.citations).toBe("auto");
    expect(resolved.vega).toBeUndefined();
  });

  it("resolves vega backend with defaults", () => {
    const cfg = {
      agents: { defaults: { workspace: "/tmp/memory-test" } },
      memory: {
        backend: "vega",
        vega: {},
      },
    } as DenebConfig;
    const resolved = resolveMemoryBackendConfig({ cfg, agentId: "main" });
    expect(resolved.backend).toBe("vega");
    expect(resolved.vega?.searchMode).toBe("query");
    expect(resolved.vega?.update.intervalMs).toBeGreaterThan(0);
    expect(resolved.vega?.update.onBoot).toBe(true);
  });

  it("resolves vega search mode override", () => {
    const cfg = {
      agents: { defaults: { workspace: "/tmp/memory-test" } },
      memory: {
        backend: "vega",
        vega: {
          searchMode: "vsearch",
        },
      },
    } as DenebConfig;
    const resolved = resolveMemoryBackendConfig({ cfg, agentId: "main" });
    expect(resolved.vega?.searchMode).toBe("vsearch");
  });

  it("resolves vega limits overrides", () => {
    const cfg = {
      agents: { defaults: { workspace: "/tmp/memory-test" } },
      memory: {
        backend: "vega",
        vega: {
          limits: {
            maxResults: 20,
            maxSnippetChars: 5_000,
            maxInjectedChars: 15_000,
            timeoutMs: 30_000,
          },
        },
      },
    } as DenebConfig;
    const resolved = resolveMemoryBackendConfig({ cfg, agentId: "main" });
    expect(resolved.vega?.limits.maxResults).toBe(20);
    expect(resolved.vega?.limits.maxSnippetChars).toBe(5_000);
    expect(resolved.vega?.limits.maxInjectedChars).toBe(15_000);
    expect(resolved.vega?.limits.timeoutMs).toBe(30_000);
  });

  it("sanitizes vega env entries", () => {
    const cfg = {
      agents: { defaults: { workspace: "/tmp/memory-test" } },
      memory: {
        backend: "vega",
        vega: {
          env: { VEGA_MODEL: "custom-model", "  ": "empty-key", VALID: "ok" },
        },
      },
    } as DenebConfig;
    const resolved = resolveMemoryBackendConfig({ cfg, agentId: "main" });
    expect(resolved.vega?.env).toEqual({ VEGA_MODEL: "custom-model", VALID: "ok" });
  });
});
