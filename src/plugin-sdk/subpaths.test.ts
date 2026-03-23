import * as channelReplyPipelineSdk from "deneb/plugin-sdk/channel-reply-pipeline";
import * as coreSdk from "deneb/plugin-sdk/core";
import type {
  ChannelMessageActionContext as CoreChannelMessageActionContext,
  DenebPluginApi as CoreDenebPluginApi,
  PluginRuntime as CorePluginRuntime,
} from "deneb/plugin-sdk/core";
import * as telegramSdk from "deneb/plugin-sdk/telegram";
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

describe("plugin-sdk subpath exports", () => {
  it("keeps the curated public list free of internal implementation subpaths", () => {
    expect(pluginSdkSubpaths).not.toContain("compat");
    expect(pluginSdkSubpaths).not.toContain("pairing-access");
    expect(pluginSdkSubpaths).not.toContain("reply-prefix");
    expect(pluginSdkSubpaths).not.toContain("typing");
    expect(pluginSdkSubpaths).not.toContain("provider-model-definitions");
  });

  it("keeps core focused on generic shared exports", () => {
    expect("runPassiveAccountLifecycle" in asExports(coreSdk)).toBe(false);
    expect("createLoggerBackedRuntime" in asExports(coreSdk)).toBe(false);
    expect("registerSandboxBackend" in asExports(coreSdk)).toBe(false);
  });

  it("keeps channel reply pipeline internals out of the public subpath", () => {
    expect("createTypingCallbacks" in asExports(channelReplyPipelineSdk)).toBe(false);
    expect("createReplyPrefixContext" in asExports(channelReplyPipelineSdk)).toBe(false);
    expect("createReplyPrefixOptions" in asExports(channelReplyPipelineSdk)).toBe(false);
  });

  it("exports shared core types used by bundled channels", () => {
    expectTypeOf<CoreDenebPluginApi>().toMatchTypeOf<DenebPluginApi>();
    expectTypeOf<CorePluginRuntime>().toMatchTypeOf<PluginRuntime>();
    expectTypeOf<CoreChannelMessageActionContext>().toMatchTypeOf<ChannelMessageActionContext>();
  });

  it("keeps core shared types aligned with the channel prelude", () => {
    expectTypeOf<CoreDenebPluginApi>().toMatchTypeOf<SharedDenebPluginApi>();
    expectTypeOf<CorePluginRuntime>().toMatchTypeOf<SharedPluginRuntime>();
    expectTypeOf<CoreChannelMessageActionContext>().toMatchTypeOf<SharedChannelMessageActionContext>();
  });

  it("keeps Telegram internals out of the public subpath", () => {
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
