/**
 * LCM tool resolution for native (core-provided) tool registration.
 *
 * Uses the shared singleton LCM engine from register.ts to ensure a single
 * per-session operation queue protects the shared SQLite DB.
 */

import type { DenebConfig } from "../../config/config.js";
import { getOrCreateLcmSingleton } from "../../context-engine/lcm/index.js";
import type { AnyAgentTool } from "../../context-engine/lcm/src/tools/common.js";
import { createLcmDescribeTool } from "../../context-engine/lcm/src/tools/lcm-describe-tool.js";
import { createLcmExpandQueryTool } from "../../context-engine/lcm/src/tools/lcm-expand-query-tool.js";
import { createLcmExpandTool } from "../../context-engine/lcm/src/tools/lcm-expand-tool.js";
import { createLcmGrepTool } from "../../context-engine/lcm/src/tools/lcm-grep-tool.js";

export function resolveLcmTools(params: {
  sessionKey?: string;
  config?: DenebConfig;
}): AnyAgentTool[] {
  try {
    const { deps, lcm } = getOrCreateLcmSingleton();
    const sessionKey = params.sessionKey ?? "";

    return [
      createLcmGrepTool({ deps, lcm, sessionKey }),
      createLcmDescribeTool({ deps, lcm, sessionKey }),
      createLcmExpandTool({ deps, lcm, sessionKey }),
      createLcmExpandQueryTool({ deps, lcm, sessionKey, requesterSessionKey: sessionKey }),
    ];
  } catch {
    // LCM unavailable — return empty (non-fatal)
    return [];
  }
}
