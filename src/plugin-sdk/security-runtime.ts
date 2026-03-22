// Public security/policy helpers for plugins that need shared trust and DM gating logic.

export * from "../security/channel-metadata.js";
export * from "../security/external-content.js";
export * from "../security/safe-regex.js";

// Solo-dev stub: removed allowlist pinning.
export function resolvePinnedMainDmOwnerFromAllowlist(_params: {
  dmScope?: string;
  allowFrom?: Array<string | number>;
  normalizeEntry?: (entry: string) => string | undefined;
}): string | null {
  return null;
}
