/**
 * Gateway startup: Control UI root resolution.
 *
 * Resolves which directory to serve the Control UI from, handling the
 * override, auto-detect, and asset-build-on-demand flows.
 * Extracted from server.impl.ts.
 */

import path from "node:path";
import {
  ensureControlUiAssetsBuilt,
  isPackageProvenControlUiRootSync,
  resolveControlUiRootOverrideSync,
  resolveControlUiRootSync,
} from "../infra/control-ui-assets.js";
import type { RuntimeEnv } from "../runtime.js";
import type { ControlUiRootState } from "./dashboard/control-ui.js";

export type { ControlUiRootState };

export type ResolveControlUiRootOptions = {
  /** Override path from config (gateway.controlUi.root). */
  controlUiRootOverride: string | null | undefined;
  /** Whether the Control UI is enabled at all. */
  controlUiEnabled: boolean;
  /** Gateway runtime env (used for the asset-build step). */
  gatewayRuntime: RuntimeEnv;
  /** Logger for warnings. */
  log: { warn: (msg: string) => void };
  /** `import.meta.url` from the calling module (used by root resolvers). */
  moduleUrl: string;
};

/**
 * Determines which directory to serve the Control UI from.
 *
 * Returns `undefined` when the UI is disabled and no override is set.
 */
export async function resolveGatewayControlUiRoot(
  opts: ResolveControlUiRootOptions,
): Promise<ControlUiRootState | undefined> {
  const { controlUiRootOverride, controlUiEnabled, gatewayRuntime, log, moduleUrl } = opts;

  const resolverCtx = {
    moduleUrl,
    argv1: process.argv[1],
    cwd: process.cwd(),
  };

  if (controlUiRootOverride) {
    const resolvedOverride = resolveControlUiRootOverrideSync(controlUiRootOverride);
    const resolvedOverridePath = path.resolve(controlUiRootOverride);
    if (!resolvedOverride) {
      log.warn(`gateway: controlUi.root not found at ${resolvedOverridePath}`);
      return { kind: "invalid", path: resolvedOverridePath };
    }
    return { kind: "resolved", path: resolvedOverride };
  }

  if (!controlUiEnabled) {
    return undefined;
  }

  let resolvedRoot = resolveControlUiRootSync(resolverCtx);
  if (!resolvedRoot) {
    const ensureResult = await ensureControlUiAssetsBuilt(gatewayRuntime);
    if (!ensureResult.ok && ensureResult.message) {
      log.warn(`gateway: ${ensureResult.message}`);
    }
    resolvedRoot = resolveControlUiRootSync(resolverCtx);
  }

  if (!resolvedRoot) {
    return { kind: "missing" };
  }

  const kind = isPackageProvenControlUiRootSync(resolvedRoot, resolverCtx) ? "bundled" : "resolved";
  return { kind, path: resolvedRoot };
}
