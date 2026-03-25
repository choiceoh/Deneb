import { emptyPluginConfigSchema } from "../plugins/config-schema.js";
import type {
  DenebPluginApi,
  DenebPluginCommandDefinition,
  DenebPluginConfigSchema,
  DenebPluginDefinition,
  PluginInteractiveTelegramHandlerContext,
} from "../plugins/types.js";

export type {
  AnyAgentTool,
  MediaUnderstandingProviderPlugin,
  DenebPluginApi,
  DenebPluginConfigSchema,
  ProviderDiscoveryContext,
  ProviderCatalogContext,
  ProviderCatalogResult,
  ProviderAugmentModelCatalogContext,
  ProviderBuiltInModelSuppressionContext,
  ProviderBuiltInModelSuppressionResult,
  ProviderBuildMissingAuthMessageContext,
  ProviderCacheTtlEligibilityContext,
  ProviderDefaultThinkingPolicyContext,
  ProviderFetchUsageSnapshotContext,
  ProviderModernModelPolicyContext,
  ProviderPreparedRuntimeAuth,
  ProviderResolvedUsageAuth,
  ProviderPrepareExtraParamsContext,
  ProviderPrepareDynamicModelContext,
  ProviderPrepareRuntimeAuthContext,
  ProviderResolveUsageAuthContext,
  ProviderResolveDynamicModelContext,
  ProviderNormalizeResolvedModelContext,
  ProviderRuntimeModel,
  ProviderThinkingPolicyContext,
  ProviderWrapStreamFnContext,
  DenebPluginService,
  DenebPluginServiceContext,
  ProviderAuthContext,
  ProviderAuthDoctorHintContext,
  ProviderAuthMethodNonInteractiveContext,
  ProviderAuthMethod,
  ProviderAuthResult,
  DenebPluginCommandDefinition,
  DenebPluginDefinition,
  PluginLogger,
  PluginInteractiveTelegramHandlerContext,
} from "../plugins/types.js";
export type { DenebConfig } from "../config/config.js";

export { emptyPluginConfigSchema } from "../plugins/config-schema.js";

type DefinePluginEntryOptions = {
  id: string;
  name: string;
  description: string;
  kind?: DenebPluginDefinition["kind"];
  configSchema?: DenebPluginConfigSchema | (() => DenebPluginConfigSchema);
  register: (api: DenebPluginApi) => void;
};

type DefinedPluginEntry = {
  id: string;
  name: string;
  description: string;
  configSchema: DenebPluginConfigSchema;
  register: NonNullable<DenebPluginDefinition["register"]>;
} & Pick<DenebPluginDefinition, "kind">;

function resolvePluginConfigSchema(
  configSchema: DefinePluginEntryOptions["configSchema"] = emptyPluginConfigSchema,
): DenebPluginConfigSchema {
  return typeof configSchema === "function" ? configSchema() : configSchema;
}

// Small entry surface for provider and command plugins that do not need channel helpers.
export function definePluginEntry({
  id,
  name,
  description,
  kind,
  configSchema = emptyPluginConfigSchema,
  register,
}: DefinePluginEntryOptions): DefinedPluginEntry {
  return {
    id,
    name,
    description,
    ...(kind ? { kind } : {}),
    configSchema: resolvePluginConfigSchema(configSchema),
    register,
  };
}
