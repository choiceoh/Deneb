/**
 * Aurora (Lossless Context Management) native registration.
 *
 * Registers the Aurora context engine and its 4 tools (aurora_grep, aurora_describe,
 * aurora_expand, aurora_expand_query) as core-provided entries.
 */

import { registerContextEngineForOwner } from "../registry.js";
import { createNativeAuroraDependencies } from "./native-bridge.js";
import { AuroraContextEngine } from "./src/engine.js";
import { createAuroraDescribeTool } from "./src/tools/aurora-describe-tool.js";
import { createAuroraExpandQueryTool } from "./src/tools/aurora-expand-query-tool.js";
import { createAuroraExpandTool } from "./src/tools/aurora-expand-tool.js";
import { createAuroraGrepTool } from "./src/tools/aurora-grep-tool.js";

let registered = false;
let sharedDeps: ReturnType<typeof createNativeAuroraDependencies> | null = null;
let sharedAurora: AuroraContextEngine | null = null;

/** Lazily create or return the singleton Aurora engine + deps. */
export function getOrCreateAuroraSingleton() {
  if (!sharedDeps || !sharedAurora) {
    sharedDeps = createNativeAuroraDependencies();
    sharedAurora = new AuroraContextEngine(sharedDeps);
  }
  return { deps: sharedDeps, aurora: sharedAurora };
}

/**
 * Register the Aurora context engine with the core registry.
 * Safe to call multiple times — subsequent calls are no-ops.
 */
export function registerAuroraContextEngine(): void {
  if (registered) {
    return;
  }

  let deps: ReturnType<typeof createNativeAuroraDependencies>;
  let aurora: AuroraContextEngine;
  try {
    const singleton = getOrCreateAuroraSingleton();
    deps = singleton.deps;
    aurora = singleton.aurora;
  } catch (err) {
    // Log but don't set registered=true so a retry is possible after config fix.
    console.error(
      `[aurora] Failed to initialize Aurora dependencies: ${err instanceof Error ? err.message : String(err)}`,
    );
    return;
  }

  registered = true;

  // Register as core-owned engine with id "aurora"
  const result = registerContextEngineForOwner("aurora", () => aurora, "core", {
    allowSameOwnerRefresh: true,
  });

  if (!result.ok) {
    deps.log.warn(
      `Failed to register Aurora context engine: existing owner="${result.existingOwner}"`,
    );
    return;
  }

  deps.log.info(
    `[aurora] Native engine registered (db=${deps.config.databasePath}, threshold=${deps.config.contextThreshold})`,
  );
}

// ---------------------------------------------------------------------------
// Tool factories — used by the tool registration system
// ---------------------------------------------------------------------------

/**
 * Create Aurora tool definitions for native registration.
 *
 * Each factory returns an AnyAgentTool-compatible object.
 * The sessionKey is provided per-invocation by the tool runtime.
 *
 * Uses the same singleton AuroraContextEngine as registerAuroraContextEngine() to
 * ensure a single per-session operation queue protects the shared SQLite DB.
 */
export function createAuroraToolFactories() {
  const { deps, aurora } = getOrCreateAuroraSingleton();

  return [
    {
      name: "aurora_grep",
      factory: (ctx: { sessionKey: string }) =>
        createAuroraGrepTool({ deps, aurora, sessionKey: ctx.sessionKey }),
    },
    {
      name: "aurora_describe",
      factory: (ctx: { sessionKey: string }) =>
        createAuroraDescribeTool({ deps, aurora, sessionKey: ctx.sessionKey }),
    },
    {
      name: "aurora_expand",
      factory: (ctx: { sessionKey: string }) =>
        createAuroraExpandTool({ deps, aurora, sessionKey: ctx.sessionKey }),
    },
    {
      name: "aurora_expand_query",
      factory: (ctx: { sessionKey: string }) =>
        createAuroraExpandQueryTool({
          deps,
          aurora,
          sessionKey: ctx.sessionKey,
          requesterSessionKey: ctx.sessionKey,
        }),
    },
  ];
}
