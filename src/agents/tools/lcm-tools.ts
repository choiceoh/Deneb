/**
 * LCM tool resolution for native (core-provided) tool registration.
 *
 * Replaces the lossless-claw plugin's tool registration with direct
 * instantiation using the native LCM engine.
 */

import { createNativeLcmDependencies } from "../../context-engine/lcm/native-bridge.js";
import { LcmContextEngine } from "../../context-engine/lcm/src/engine.js";
import { createLcmGrepTool } from "../../context-engine/lcm/src/tools/lcm-grep-tool.js";
import { createLcmDescribeTool } from "../../context-engine/lcm/src/tools/lcm-describe-tool.js";
import { createLcmExpandTool } from "../../context-engine/lcm/src/tools/lcm-expand-tool.js";
import { createLcmExpandQueryTool } from "../../context-engine/lcm/src/tools/lcm-expand-query-tool.js";
import type { AnyAgentTool } from "../../context-engine/lcm/src/tools/common.js";
import type { OpenClawConfig } from "../../config/io.js";

/** Lazy singleton — engine is expensive to create, share across tool invocations. */
let _sharedEngine: { deps: unknown; lcm: LcmContextEngine } | undefined;

function getSharedEngine(_config?: OpenClawConfig) {
  if (!_sharedEngine) {
    const deps = createNativeLcmDependencies();
    const lcm = new LcmContextEngine(deps);
    _sharedEngine = { deps, lcm };
  }
  return _sharedEngine;
}

export function resolveLcmTools(params: {
  sessionKey?: string;
  config?: OpenClawConfig;
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
