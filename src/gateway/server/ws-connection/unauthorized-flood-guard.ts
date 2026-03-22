// Stub: unauthorized flood guard removed for solo-dev simplification.
import type { ErrorShape } from "../../protocol/index.js";

export type UnauthorizedFloodGuardOptions = {
  closeAfter?: number;
  logEvery?: number;
};

export type UnauthorizedFloodDecision = {
  shouldClose: boolean;
  shouldLog: boolean;
  count: number;
  suppressedSinceLastLog: number;
};

export class UnauthorizedFloodGuard {
  registerUnauthorized(): UnauthorizedFloodDecision {
    return { shouldClose: false, shouldLog: false, count: 0, suppressedSinceLastLog: 0 };
  }

  reset(): void {}
}

export function isUnauthorizedRoleError(_error?: ErrorShape): boolean {
  return false;
}
