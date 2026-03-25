import { describe, expect, it } from "vitest";
import { loadConfig } from "./config.js";
import { withTempHomeConfig } from "./test-helpers.js";

describe("config compaction settings", () => {
  it("accepts compaction config values including deprecated system-managed fields", async () => {
    await withTempHomeConfig(
      {
        agents: {
          defaults: {
            compaction: {
              // reserveTokensFloor is now system-managed (accepted but ignored at runtime).
              reserveTokensFloor: 12_345,
              // identifierPolicy, identifierInstructions, memoryFlush.enabled,
              // and truncateAfterCompaction are system-managed but accepted for backward compat.
              identifierPolicy: "custom",
              identifierInstructions: "Keep ticket IDs unchanged.",
              memoryFlush: {
                enabled: false,
                softThresholdTokens: 1234,
                prompt: "Write notes.",
                systemPrompt: "Flush memory now.",
              },
            },
          },
        },
      },
      async () => {
        const cfg = loadConfig();

        // Deprecated fields are stored but ignored at runtime.
        expect(cfg.agents?.defaults?.compaction?.reserveTokensFloor).toBe(12_345);
        expect(cfg.agents?.defaults?.compaction?.identifierPolicy).toBe("custom");
        expect(cfg.agents?.defaults?.compaction?.identifierInstructions).toBe(
          "Keep ticket IDs unchanged.",
        );
        expect(cfg.agents?.defaults?.compaction?.memoryFlush?.enabled).toBe(false);
        expect(cfg.agents?.defaults?.compaction?.memoryFlush?.softThresholdTokens).toBe(1234);
        expect(cfg.agents?.defaults?.compaction?.memoryFlush?.prompt).toBe("Write notes.");
        expect(cfg.agents?.defaults?.compaction?.memoryFlush?.systemPrompt).toBe(
          "Flush memory now.",
        );
      },
    );
  });

  it("accepts deprecated reserveTokens/keepRecentTokens (system-managed, ignored at runtime)", async () => {
    await withTempHomeConfig(
      {
        agents: {
          defaults: {
            compaction: {
              reserveTokens: 15_000,
              keepRecentTokens: 12_000,
            },
          },
        },
      },
      async () => {
        // Config loads without crashing — fields are accepted but ignored at runtime.
        const cfg = loadConfig();
        expect(cfg.agents?.defaults?.compaction?.reserveTokens).toBe(15_000);
        expect(cfg.agents?.defaults?.compaction?.keepRecentTokens).toBe(12_000);
      },
    );
  });

  it("accepts deprecated system-managed fields without crashing (backward compat)", async () => {
    await withTempHomeConfig(
      {
        agents: {
          defaults: {
            compaction: {
              mode: "safeguard",
              recentTurnsPreserve: 4,
              maxHistoryShare: 0.6,
              qualityGuard: {
                enabled: true,
                maxRetries: 2,
              },
              reserveTokensFloor: 9000,
            },
          },
        },
      },
      async () => {
        // Config loads without crashing — deprecated fields are accepted but ignored at runtime.
        const cfg = loadConfig();
        expect(cfg.agents?.defaults?.compaction?.reserveTokensFloor).toBe(9000);
      },
    );
  });
});
