import type { DenebConfig } from "../../config/config.js";

export function createPerSenderSessionConfig(
  overrides: Partial<NonNullable<DenebConfig["session"]>> = {},
): NonNullable<DenebConfig["session"]> {
  return {
    mainKey: "main",
    scope: "per-sender",
    ...overrides,
  };
}
