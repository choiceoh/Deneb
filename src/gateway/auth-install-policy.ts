// Stub: auth install policy removed for solo-dev simplification.
import type { DenebConfig } from "../config/config.js";

export function shouldRequireGatewayTokenForInstall(
  _cfg: DenebConfig,
  _env: NodeJS.ProcessEnv,
): boolean {
  return false;
}
