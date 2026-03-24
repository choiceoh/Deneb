import type { PluginRegistry } from "./registry-types.js";

export function createEmptyPluginRegistry(): PluginRegistry {
  return {
    plugins: [],
    tools: [],
    hooks: [],
    typedHooks: [],
    channels: [],
    channelSetups: [],
    providers: [],
    speechProviders: [],
    mediaUnderstandingProviders: [],
    imageGenerationProviders: [],
    webSearchProviders: [],
    gatewayHandlers: {},
    httpRoutes: [],
    cliRegistrars: [],
    services: [],
    commands: [],
    conversationBindingResolvedHandlers: [],
    diagnostics: [],
  };
}
