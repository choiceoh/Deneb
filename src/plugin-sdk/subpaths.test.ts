import * as channelReplyPipelineSdk from "deneb/plugin-sdk/channel-reply-pipeline";
import * as channelRuntimeSdk from "deneb/plugin-sdk/channel-runtime";
import * as channelSendResultSdk from "deneb/plugin-sdk/channel-send-result";
import * as channelSetupSdk from "deneb/plugin-sdk/channel-setup";
import * as coreSdk from "deneb/plugin-sdk/core";
import type {
  ChannelMessageActionContext as CoreChannelMessageActionContext,
  DenebPluginApi as CoreDenebPluginApi,
  PluginRuntime as CorePluginRuntime,
} from "deneb/plugin-sdk/core";
import * as directoryRuntimeSdk from "deneb/plugin-sdk/directory-runtime";
import * as lazyRuntimeSdk from "deneb/plugin-sdk/lazy-runtime";
import * as providerModelsSdk from "deneb/plugin-sdk/provider-models";
import * as providerSetupSdk from "deneb/plugin-sdk/provider-setup";
import * as replyPayloadSdk from "deneb/plugin-sdk/reply-payload";
import * as routingSdk from "deneb/plugin-sdk/routing";
import * as runtimeSdk from "deneb/plugin-sdk/runtime";
import * as sandboxSdk from "deneb/plugin-sdk/sandbox";
import * as secretInputSdk from "deneb/plugin-sdk/secret-input";
import * as selfHostedProviderSetupSdk from "deneb/plugin-sdk/self-hosted-provider-setup";
import * as setupSdk from "deneb/plugin-sdk/setup";
import * as telegramSdk from "deneb/plugin-sdk/telegram";
import * as testingSdk from "deneb/plugin-sdk/testing";
import * as webhookIngressSdk from "deneb/plugin-sdk/webhook-ingress";
import { describe, expect, expectTypeOf, it } from "vitest";
import type { ChannelMessageActionContext } from "../channels/plugins/types.js";
import type { PluginRuntime } from "../plugins/runtime/types.js";
import type { DenebPluginApi } from "../plugins/types.js";
import type {
  ChannelMessageActionContext as SharedChannelMessageActionContext,
  DenebPluginApi as SharedDenebPluginApi,
  PluginRuntime as SharedPluginRuntime,
} from "./channel-plugin-common.js";
import { pluginSdkSubpaths } from "./entrypoints.js";

const importPluginSdkSubpath = (specifier: string) => import(/* @vite-ignore */ specifier);

const bundledExtensionSubpathLoaders = pluginSdkSubpaths.map((id: string) => ({
  id,
  load: () => importPluginSdkSubpath(`deneb/plugin-sdk/${id}`),
}));

const asExports = (mod: object) => mod as Record<string, unknown>;
const accountHelpersSdk = await import("deneb/plugin-sdk/account-helpers");

describe("plugin-sdk subpath exports", () => {
  it("keeps the curated public list free of internal implementation subpaths", () => {
    expect(pluginSdkSubpaths).not.toContain("compat");
    expect(pluginSdkSubpaths).not.toContain("pairing-access");
    expect(pluginSdkSubpaths).not.toContain("reply-prefix");
    expect(pluginSdkSubpaths).not.toContain("typing");
    expect(pluginSdkSubpaths).not.toContain("provider-model-definitions");
  });

  it("keeps core focused on generic shared exports", () => {
    expect(typeof coreSdk.emptyPluginConfigSchema).toBe("function");
    expect(typeof coreSdk.definePluginEntry).toBe("function");
    expect(typeof coreSdk.defineChannelPluginEntry).toBe("function");
    expect(typeof coreSdk.defineSetupPluginEntry).toBe("function");
    expect(typeof coreSdk.createChannelPluginBase).toBe("function");
    expect(typeof coreSdk.isSecretRef).toBe("function");
    expect(typeof coreSdk.optionalStringEnum).toBe("function");
    expect("runPassiveAccountLifecycle" in asExports(coreSdk)).toBe(false);
    expect("createLoggerBackedRuntime" in asExports(coreSdk)).toBe(false);
    expect("registerSandboxBackend" in asExports(coreSdk)).toBe(false);
  });

  it("exports routing helpers from the dedicated subpath", () => {
    expect(typeof routingSdk.buildAgentSessionKey).toBe("function");
    expect(typeof routingSdk.resolveThreadSessionKeys).toBe("function");
  });

  it("exports reply payload helpers from the dedicated subpath", () => {
    expect(typeof replyPayloadSdk.deliverTextOrMediaReply).toBe("function");
    expect(typeof replyPayloadSdk.resolveOutboundMediaUrls).toBe("function");
    expect(typeof replyPayloadSdk.sendPayloadWithChunkedTextAndMedia).toBe("function");
  });

  it("exports account helper builders from the dedicated subpath", () => {
    expect(typeof accountHelpersSdk.createAccountListHelpers).toBe("function");
  });

  it("exports runtime helpers from the dedicated subpath", () => {
    expect(typeof runtimeSdk.createLoggerBackedRuntime).toBe("function");
  });

  it("exports directory runtime helpers from the dedicated subpath", () => {
    expect(typeof directoryRuntimeSdk.listDirectoryEntriesFromSources).toBe("function");
    expect(typeof directoryRuntimeSdk.listResolvedDirectoryEntriesFromSources).toBe("function");
  });

  it("exports channel runtime helpers from the dedicated subpath", () => {
    expect(typeof channelRuntimeSdk.createChannelDirectoryAdapter).toBe("function");
    expect(typeof channelRuntimeSdk.createRuntimeOutboundDelegates).toBe("function");
    expect(typeof channelRuntimeSdk.sendPayloadMediaSequenceOrFallback).toBe("function");
  });

  it("exports channel setup helpers from the dedicated subpath", () => {
    expect(typeof channelSetupSdk.createOptionalChannelSetupSurface).toBe("function");
    expect(typeof channelSetupSdk.createTopLevelChannelDmPolicy).toBe("function");
  });

  it("exports channel reply pipeline helpers from the dedicated subpath", () => {
    expect(typeof channelReplyPipelineSdk.createChannelReplyPipeline).toBe("function");
    expect("createTypingCallbacks" in asExports(channelReplyPipelineSdk)).toBe(false);
    expect("createReplyPrefixContext" in asExports(channelReplyPipelineSdk)).toBe(false);
    expect("createReplyPrefixOptions" in asExports(channelReplyPipelineSdk)).toBe(false);
  });

  it("exports channel send-result helpers from the dedicated subpath", () => {
    expect(typeof channelSendResultSdk.attachChannelToResult).toBe("function");
    expect(typeof channelSendResultSdk.buildChannelSendResult).toBe("function");
  });

  it("exports provider setup helpers from the dedicated subpath", () => {
    expect(typeof providerSetupSdk.discoverOpenAICompatibleSelfHostedProvider).toBe("function");
  });

  it("keeps provider models focused on shared provider primitives", () => {
    expect(typeof providerModelsSdk.applyOpenAIConfig).toBe("function");
    expect(typeof providerModelsSdk.buildKilocodeModelDefinition).toBe("function");
    expect(typeof providerModelsSdk.discoverHuggingfaceModels).toBe("function");
    expect("buildMinimaxModelDefinition" in asExports(providerModelsSdk)).toBe(false);
    expect("buildMoonshotProvider" in asExports(providerModelsSdk)).toBe(false);
    expect("QIANFAN_BASE_URL" in asExports(providerModelsSdk)).toBe(false);
    expect("resolveZaiBaseUrl" in asExports(providerModelsSdk)).toBe(false);
  });

  it("exports shared setup helpers from the dedicated subpath", () => {
    expect(typeof setupSdk.DEFAULT_ACCOUNT_ID).toBe("string");
    expect(typeof setupSdk.createAllowFromSection).toBe("function");
    expect(typeof setupSdk.createDelegatedSetupWizardProxy).toBe("function");
    expect(typeof setupSdk.createTopLevelChannelDmPolicy).toBe("function");
    expect(typeof setupSdk.mergeAllowFromEntries).toBe("function");
  });

  it("exports shared lazy runtime helpers from the dedicated subpath", () => {
    expect(typeof lazyRuntimeSdk.createLazyRuntimeSurface).toBe("function");
    expect(typeof lazyRuntimeSdk.createLazyRuntimeModule).toBe("function");
  });

  it("exports narrow self-hosted provider setup helpers", () => {
    expect(
      typeof selfHostedProviderSetupSdk.configureOpenAICompatibleSelfHostedProviderNonInteractive,
    ).toBe("function");
  });

  it("exports sandbox helpers from the dedicated subpath", () => {
    expect(typeof sandboxSdk.registerSandboxBackend).toBe("function");
    expect(typeof sandboxSdk.runPluginCommandWithTimeout).toBe("function");
  });

  it("exports secret input helpers from the dedicated subpath", () => {
    expect(typeof secretInputSdk.buildSecretInputSchema).toBe("function");
    expect(typeof secretInputSdk.buildOptionalSecretInputSchema).toBe("function");
    expect(typeof secretInputSdk.normalizeSecretInputString).toBe("function");
  });

  it("exports webhook ingress helpers from the dedicated subpath", () => {
    expect(typeof webhookIngressSdk.resolveWebhookPath).toBe("function");
    expect(typeof webhookIngressSdk.readJsonWebhookBodyOrReject).toBe("function");
    expect(typeof webhookIngressSdk.withResolvedWebhookRequestPipeline).toBe("function");
  });

  it("exports shared core types used by bundled channels", () => {
    expectTypeOf<CoreDenebPluginApi>().toMatchTypeOf<DenebPluginApi>();
    expectTypeOf<CorePluginRuntime>().toMatchTypeOf<PluginRuntime>();
    expectTypeOf<CoreChannelMessageActionContext>().toMatchTypeOf<ChannelMessageActionContext>();
  });

  it("exports the public testing surface", () => {
    expect(typeof testingSdk.removeAckReactionAfterReply).toBe("function");
    expect(typeof testingSdk.shouldAckReaction).toBe("function");
  });

  it("keeps core shared types aligned with the channel prelude", () => {
    expectTypeOf<CoreDenebPluginApi>().toMatchTypeOf<SharedDenebPluginApi>();
    expectTypeOf<CorePluginRuntime>().toMatchTypeOf<SharedPluginRuntime>();
    expectTypeOf<CoreChannelMessageActionContext>().toMatchTypeOf<SharedChannelMessageActionContext>();
  });

  it("exports Telegram helpers", () => {
    expect(typeof telegramSdk.buildChannelConfigSchema).toBe("function");
    expect(typeof telegramSdk.TelegramConfigSchema).toBe("object");
    expect(typeof telegramSdk.projectCredentialSnapshotFields).toBe("function");
    expect("resolveTelegramAccount" in asExports(telegramSdk)).toBe(false);
  });

  it("resolves every curated public subpath", async () => {
    for (const { id, load } of bundledExtensionSubpathLoaders) {
      const mod = await load();
      expect(typeof mod).toBe("object");
      expect(mod, `subpath ${id} should resolve`).toBeTruthy();
    }
  });
});
