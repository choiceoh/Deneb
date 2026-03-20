/**
 * LCM (Lossless Context Management) native registration.
 *
 * Registers the LCM context engine and its 4 tools (lcm_grep, lcm_describe,
 * lcm_expand, lcm_expand_query) as core-provided entries.
 *
 * Replaces the lossless-claw plugin's index.ts register() function.
 */

import { registerContextEngineForOwner } from "../registry.js";
import { createNativeLcmDependencies } from "./native-bridge.js";
import { LcmContextEngine } from "./src/engine.js";
import { createLcmDescribeTool } from "./src/tools/lcm-describe-tool.js";
import { createLcmExpandQueryTool } from "./src/tools/lcm-expand-query-tool.js";
import { createLcmExpandTool } from "./src/tools/lcm-expand-tool.js";
import { createLcmGrepTool } from "./src/tools/lcm-grep-tool.js";

let registered = false;
let sharedDeps: ReturnType<typeof createNativeLcmDependencies> | null = null;
let sharedLcm: LcmContextEngine | null = null;

/** Lazily create or return the singleton LCM engine + deps. */
function getOrCreateLcmSingleton() {
  if (!sharedDeps || !sharedLcm) {
    sharedDeps = createNativeLcmDependencies();
    sharedLcm = new LcmContextEngine(sharedDeps);
  }
  return { deps: sharedDeps, lcm: sharedLcm };
}

/**
 * Register the LCM context engine with the core registry.
 * Safe to call multiple times — subsequent calls are no-ops.
 */
export function registerLcmContextEngine(): void {
  if (registered) {
    return;
  }
  registered = true;

  const { deps, lcm } = getOrCreateLcmSingleton();

  // Register as core-owned engine with id "lcm"
  const result = registerContextEngineForOwner("lcm", () => lcm, "core", {
    allowSameOwnerRefresh: true,
  });

  if (!result.ok) {
    deps.log.warn(
      `Failed to register LCM context engine: existing owner="${result.existingOwner}"`,
    );
    return;
  }

  deps.log.info(
    `[lcm] Native engine registered (db=${deps.config.databasePath}, threshold=${deps.config.contextThreshold})`,
  );
}

// ---------------------------------------------------------------------------
// Tool factories — used by the tool registration system
// ---------------------------------------------------------------------------

/**
 * Create LCM tool definitions for native registration.
 *
 * Each factory returns an AnyAgentTool-compatible object.
 * The sessionKey is provided per-invocation by the tool runtime.
 *
 * Uses the same singleton LcmContextEngine as registerLcmContextEngine() to
 * ensure a single per-session operation queue protects the shared SQLite DB.
 */
export function createLcmToolFactories() {
  const { deps, lcm } = getOrCreateLcmSingleton();

  return [
    {
      name: "lcm_grep",
      factory: (ctx: { sessionKey: string }) =>
        createLcmGrepTool({ deps, lcm, sessionKey: ctx.sessionKey }),
    },
    {
      name: "lcm_describe",
      factory: (ctx: { sessionKey: string }) =>
        createLcmDescribeTool({ deps, lcm, sessionKey: ctx.sessionKey }),
    },
    {
      name: "lcm_expand",
      factory: (ctx: { sessionKey: string }) =>
        createLcmExpandTool({ deps, lcm, sessionKey: ctx.sessionKey }),
    },
    {
      name: "lcm_expand_query",
      factory: (ctx: { sessionKey: string }) =>
        createLcmExpandQueryTool({
          deps,
          lcm,
          sessionKey: ctx.sessionKey,
          requesterSessionKey: ctx.sessionKey,
        }),
    },
  ];
}
