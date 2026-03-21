/**
 * LCM tool resolution for native (core-provided) tool registration.
 *
 * Replaces the lossless-claw plugin's tool registration with direct
 * instantiation using the native LCM engine.
 */

import type { DenebConfig } from "../../config/config.js";
import { createNativeLcmDependencies } from "../../context-engine/lcm/native-bridge.js";
import { LcmContextEngine } from "../../context-engine/lcm/src/engine.js";
import type { AnyAgentTool } from "../../context-engine/lcm/src/tools/common.js";
import { createLcmDescribeTool } from "../../context-engine/lcm/src/tools/lcm-describe-tool.js";
import { createLcmExpandQueryTool } from "../../context-engine/lcm/src/tools/lcm-expand-query-tool.js";
import { createLcmExpandTool } from "../../context-engine/lcm/src/tools/lcm-expand-tool.js";
import { createLcmGrepTool } from "../../context-engine/lcm/src/tools/lcm-grep-tool.js";
/** Lazy singleton — engine is expensive to create, share across tool invocations. */
import type { LcmDependencies } from "../../context-engine/lcm/src/types.js";

let _sharedEngine: { deps: LcmDependencies; lcm: LcmContextEngine } | undefined;

function getSharedEngine(_config?: DenebConfig) {
  if (!_sharedEngine) {
    const deps = createNativeLcmDependencies();
    const lcm = new LcmContextEngine(deps);
    _sharedEngine = { deps, lcm };
  }
  return _sharedEngine;
}

export function resolveLcmTools(params: {
  sessionKey?: string;
  config?: DenebConfig;
}): AnyAgentTool[] {
  try {
    const { deps, lcm } = getSharedEngine(params.config);
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
