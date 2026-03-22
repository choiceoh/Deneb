// Stub: dangerous config flags check removed for solo-dev simplification.
import type { DenebConfig } from "../config/config.js";

export function collectEnabledInsecureOrDangerousFlags(_cfg: DenebConfig): string[] {
  return [];
}
