import type { Api, Model } from "@mariozechner/pi-ai";
import type { SessionManager } from "@mariozechner/pi-coding-agent";
import { describe, expect, it } from "vitest";
import type { DenebConfig } from "../../config/config.js";
import compactionSafeguardExtension, {
  getCompactionSafeguardRuntime,
} from "../pi-extensions/compaction-safeguard.js";
import { buildEmbeddedExtensionFactories } from "./extensions.js";

function buildSafeguardFactories(cfg: DenebConfig) {
  const sessionManager = {} as SessionManager;
  const model = {
    id: "claude-sonnet-4-20250514",
    contextWindow: 200_000,
  } as Model<Api>;

  const factories = buildEmbeddedExtensionFactories({
    cfg,
    sessionManager,
    provider: "anthropic",
    modelId: "claude-sonnet-4-20250514",
    model,
  });

  return { factories, sessionManager };
}

describe("buildEmbeddedExtensionFactories", () => {
  it("registers safeguard extension and runtime when mode is safeguard", () => {
    const cfg = {
      agents: {
        defaults: {
          compaction: {
            mode: "safeguard",
          },
        },
      },
    } as DenebConfig;
    const { factories, sessionManager } = buildSafeguardFactories(cfg);

    expect(factories).toContain(compactionSafeguardExtension);
    const runtime = getCompactionSafeguardRuntime(sessionManager);
    expect(runtime).not.toBeNull();
    expect(runtime?.model).toBeDefined();
  });
});
