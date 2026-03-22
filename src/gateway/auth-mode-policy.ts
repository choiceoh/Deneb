// Stub: auth mode policy removed for solo-dev simplification.
import type { DenebConfig } from "../config/config.js";

export const EXPLICIT_GATEWAY_AUTH_MODE_REQUIRED_ERROR = "";

export function hasAmbiguousGatewayAuthModeConfig(_cfg: DenebConfig): boolean {
  return false;
}

export function assertExplicitGatewayAuthModeWhenBothConfigured(_cfg: DenebConfig): void {}
