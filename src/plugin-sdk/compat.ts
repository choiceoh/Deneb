// Legacy compat surface for external plugins that still depend on older
// broad plugin-sdk imports. Keep this file intentionally small.

const shouldWarnCompatImport =
  process.env.VITEST !== "true" &&
  process.env.NODE_ENV !== "test" &&
  process.env.DENEB_SUPPRESS_PLUGIN_SDK_COMPAT_WARNING !== "1";

if (shouldWarnCompatImport) {
  process.emitWarning(
    "deneb/plugin-sdk/compat is deprecated for new plugins. Migrate to focused deneb/plugin-sdk/<subpath> imports.",
    {
      code: "DENEB_PLUGIN_SDK_COMPAT_DEPRECATED",
      detail:
        "Bundled plugins must use scoped plugin-sdk subpaths. External plugins may keep compat temporarily while migrating.",
    },
  );
}

export { emptyPluginConfigSchema } from "../plugins/config-schema.js";
export { resolveControlCommandGate } from "../channels/command-gating.js";
export { delegateCompactionToRuntime } from "../context-engine/delegate.js";

export { createAccountStatusSink } from "./channel-lifecycle.js";
export { createPluginRuntimeStore } from "./runtime-store.js";
export { KeyedAsyncQueue } from "./keyed-async-queue.js";

export {
  createHybridChannelConfigAdapter,
  createHybridChannelConfigBase,
  createScopedAccountConfigAccessors,
  createScopedChannelConfigAdapter,
  createScopedChannelConfigBase,
  createScopedDmSecurityResolver,
  createTopLevelChannelConfigAdapter,
  createTopLevelChannelConfigBase,
  mapAllowFromEntries,
} from "./channel-config-helpers.js";
// Solo-dev stubs for removed allow-from module.
export function formatAllowFromLowercase(): unknown[] {
  return [];
}
export function formatNormalizedAllowFromEntries(): unknown[] {
  return [];
}
export * from "./channel-config-schema.js";
// Solo-dev stubs for removed channel-policy module.
// Re-export nothing; consumers may need individual stubs.
export * from "./reply-history.js";
export * from "./directory-runtime.js";
// Solo-dev stub for removed allowlist-resolution module.
export function mapAllowlistResolutionInputs(): unknown[] {
  return [];
}
