import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

type PackageManifest = {
  dependencies?: Record<string, string>;
};

function readJson<T>(relativePath: string): T {
  const absolutePath = path.resolve(process.cwd(), relativePath);
  return JSON.parse(fs.readFileSync(absolutePath, "utf8")) as T;
}

function extensionExists(extensionName: string): boolean {
  return fs.existsSync(path.resolve(process.cwd(), `extensions/${extensionName}/package.json`));
}

describe("bundled plugin runtime dependencies", () => {
  function expectPluginOwnsRuntimeDep(pluginPath: string, dependencyName: string) {
    const rootManifest = readJson<PackageManifest>("package.json");
    const pluginManifest = readJson<PackageManifest>(pluginPath);
    const pluginSpec = pluginManifest.dependencies?.[dependencyName];
    const rootSpec = rootManifest.dependencies?.[dependencyName];

    expect(pluginSpec).toBeTruthy();
    expect(rootSpec).toBeUndefined();
  }

  it.skipIf(!extensionExists("feishu"))(
    "keeps bundled Feishu runtime deps plugin-local instead of mirroring them into the root package",
    () => {
      expectPluginOwnsRuntimeDep("extensions/feishu/package.json", "@larksuiteoapi/node-sdk");
    },
  );

  it.skipIf(!extensionExists("discord"))(
    "keeps bundled Discord runtime deps plugin-local instead of mirroring them into the root package",
    () => {
      expectPluginOwnsRuntimeDep("extensions/discord/package.json", "@buape/carbon");
    },
  );

  it.skipIf(!extensionExists("slack"))(
    "keeps bundled Slack runtime deps plugin-local instead of mirroring them into the root package",
    () => {
      expectPluginOwnsRuntimeDep("extensions/slack/package.json", "@slack/bolt");
    },
  );

  it.skipIf(!extensionExists("telegram"))(
    "keeps bundled Telegram runtime deps plugin-local instead of mirroring them into the root package",
    () => {
      expectPluginOwnsRuntimeDep("extensions/telegram/package.json", "grammy");
    },
  );

  it.skipIf(!extensionExists("whatsapp"))(
    "keeps WhatsApp runtime deps plugin-local so packaged installs fetch them on demand",
    () => {
      expectPluginOwnsRuntimeDep("extensions/whatsapp/package.json", "@whiskeysockets/baileys");
    },
  );

  it.skipIf(!extensionExists("discord"))(
    "keeps bundled proxy-agent deps plugin-local instead of mirroring them into the root package",
    () => {
      expectPluginOwnsRuntimeDep("extensions/discord/package.json", "https-proxy-agent");
    },
  );

  it.skipIf(!extensionExists("line"))(
    "keeps bundled LINE runtime deps plugin-local instead of mirroring them into the root package",
    () => {
      expectPluginOwnsRuntimeDep("extensions/line/package.json", "@line/bot-sdk");
    },
  );

  it.skipIf(!extensionExists("matrix"))(
    "keeps bundled Matrix runtime deps plugin-local instead of mirroring them into the root package",
    () => {
      expectPluginOwnsRuntimeDep("extensions/matrix/package.json", "matrix-js-sdk");
    },
  );

  it.skipIf(!extensionExists("twitch"))(
    "keeps bundled Twitch runtime deps plugin-local instead of mirroring them into the root package",
    () => {
      expectPluginOwnsRuntimeDep("extensions/twitch/package.json", "@twurple/chat");
    },
  );

  it.skipIf(!extensionExists("diagnostics-otel"))(
    "keeps bundled OTel runtime deps plugin-local instead of mirroring them into the root package",
    () => {
      expectPluginOwnsRuntimeDep(
        "extensions/diagnostics-otel/package.json",
        "@opentelemetry/sdk-node",
      );
    },
  );
});
