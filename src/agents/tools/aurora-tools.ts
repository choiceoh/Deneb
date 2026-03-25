/**
 * Aurora tool resolution for native (core-provided) tool registration.
 *
 * Uses the shared singleton Aurora engine from register.ts to ensure a single
 * per-session operation queue protects the shared SQLite DB.
 */

import type { DenebConfig } from "../../config/config.js";
import { getOrCreateAuroraSingleton } from "../../context-engine/aurora/index.js";
import { createAuroraDescribeTool } from "../../context-engine/aurora/src/tools/aurora-describe-tool.js";
import { createAuroraExpandQueryTool } from "../../context-engine/aurora/src/tools/aurora-expand-query-tool.js";
import { createAuroraExpandTool } from "../../context-engine/aurora/src/tools/aurora-expand-tool.js";
import { createAuroraGrepTool } from "../../context-engine/aurora/src/tools/aurora-grep-tool.js";
import type { AnyAgentTool } from "../../context-engine/aurora/src/tools/common.js";

export function resolveAuroraTools(params: {
  sessionKey?: string;
  config?: DenebConfig;
}): AnyAgentTool[] {
  try {
    const { deps, aurora } = getOrCreateAuroraSingleton();
    const sessionKey = params.sessionKey ?? "";

    return [
      createAuroraGrepTool({ deps, aurora, sessionKey }),
      createAuroraDescribeTool({ deps, aurora, sessionKey }),
      createAuroraExpandTool({ deps, aurora, sessionKey }),
      createAuroraExpandQueryTool({ deps, aurora, sessionKey, requesterSessionKey: sessionKey }),
    ];
  } catch {
    // Aurora unavailable — return empty (non-fatal)
    return [];
  }
}
