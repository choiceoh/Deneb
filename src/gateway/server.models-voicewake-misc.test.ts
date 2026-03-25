import fs from "node:fs/promises";
import { createServer } from "node:net";
import path from "node:path";
import { afterAll, beforeAll, describe, expect, test } from "vitest";
import { WebSocket } from "ws";
import { getChannelPlugin } from "../channels/plugins/index.js";
import type { ChannelOutboundAdapter } from "../channels/plugins/types.js";
import { clearConfigCache } from "../config/config.js";
import { GatewayLockError } from "../infra/gateway-lock.js";
import { getActivePluginRegistry, setActivePluginRegistry } from "../plugins/runtime.js";
import { createOutboundTestPlugin } from "../test-utils/channel-plugins.js";
import { createRegistry } from "./server.e2e-registry-helpers.js";
import {
  getFreePort,
  installGatewayTestHooks,
  occupyPort,
  onceMessage,
  piSdkMock,
  rpcReq,
  startConnectedServerWithClient,
  startGatewayServer,
  startServerWithClient,
} from "./test-helpers.js";

installGatewayTestHooks({ scope: "suite" });

let server: Awaited<ReturnType<typeof startServerWithClient>>["server"];
let ws: WebSocket;
let _port: number;

afterAll(async () => {
  ws.close();
  await server.close();
});

beforeAll(async () => {
  const started = await startConnectedServerWithClient();
  server = started.server;
  ws = started.ws;
  _port = started.port;
});

const whatsappOutbound: ChannelOutboundAdapter = {
  deliveryMode: "direct",
  sendText: async ({ deps, to, text }) => {
    if (!deps?.["whatsapp"]) {
      throw new Error("Missing sendWhatsApp dep");
    }
    return {
      channel: "whatsapp",
      ...(await (deps["whatsapp"] as Function)(to, text, { verbose: false })),
    };
  },
  sendMedia: async ({ deps, to, text, mediaUrl }) => {
    if (!deps?.["whatsapp"]) {
      throw new Error("Missing sendWhatsApp dep");
    }
    return {
      channel: "whatsapp",
      ...(await (deps["whatsapp"] as Function)(to, text, { verbose: false, mediaUrl })),
    };
  },
};

const whatsappPlugin = createOutboundTestPlugin({
  id: "whatsapp",
  outbound: whatsappOutbound,
  label: "WhatsApp",
});

const whatsappRegistry = createRegistry([
  {
    pluginId: "whatsapp",
    source: "test",
    plugin: whatsappPlugin,
  },
]);
const emptyRegistry = createRegistry([]);

type ModelCatalogRpcEntry = {
  id: string;
  name: string;
  provider: string;
  contextWindow?: number;
};

type PiCatalogFixtureEntry = {
  id: string;
  provider: string;
  name?: string;
  contextWindow?: number;
};

const buildPiCatalogFixture = (): PiCatalogFixtureEntry[] => [
  { id: "gpt-test-z", provider: "openai", contextWindow: 0 },
  {
    id: "gpt-test-a",
    name: "A-Model",
    provider: "openai",
    contextWindow: 8000,
  },
  {
    id: "claude-test-b",
    name: "B-Model",
    provider: "anthropic",
    contextWindow: 1000,
  },
  {
    id: "claude-test-a",
    name: "A-Model",
    provider: "anthropic",
    contextWindow: 200_000,
  },
];

const expectedSortedCatalog = (): ModelCatalogRpcEntry[] => [
  {
    id: "claude-test-a",
    name: "A-Model",
    provider: "anthropic",
    contextWindow: 200_000,
  },
  {
    id: "claude-test-b",
    name: "B-Model",
    provider: "anthropic",
    contextWindow: 1000,
  },
  {
    id: "gpt-test-a",
    name: "A-Model",
    provider: "openai",
    contextWindow: 8000,
  },
  {
    id: "gpt-test-z",
    name: "gpt-test-z",
    provider: "openai",
  },
];

describe("gateway server models", () => {
  const listModels = async () => rpcReq<{ models: ModelCatalogRpcEntry[] }>(ws, "models.list");

  const seedPiCatalog = () => {
    piSdkMock.enabled = true;
    piSdkMock.models = buildPiCatalogFixture();
  };

  const withModelsConfig = async <T>(config: unknown, run: () => Promise<T>): Promise<T> => {
    const configPath = process.env.DENEB_CONFIG_PATH;
    if (!configPath) {
      throw new Error("Missing DENEB_CONFIG_PATH");
    }
    let previousConfig: string | undefined;
    try {
      previousConfig = await fs.readFile(configPath, "utf-8");
    } catch (err) {
      const code = (err as NodeJS.ErrnoException | undefined)?.code;
      if (code !== "ENOENT") {
        throw err;
      }
    }

    try {
      await fs.mkdir(path.dirname(configPath), { recursive: true });
      await fs.writeFile(configPath, JSON.stringify(config, null, 2), "utf-8");
      clearConfigCache();
      return await run();
    } finally {
      if (previousConfig === undefined) {
        await fs.rm(configPath, { force: true });
      } else {
        await fs.writeFile(configPath, previousConfig, "utf-8");
      }
      clearConfigCache();
    }
  };

  const expectAllowlistedModels = async (options: {
    primary: string;
    models: Record<string, object>;
    expected: ModelCatalogRpcEntry[];
  }): Promise<void> => {
    await withModelsConfig(
      {
        agents: {
          defaults: {
            model: { primary: options.primary },
            models: options.models,
          },
        },
      },
      async () => {
        seedPiCatalog();
        const res = await listModels();
        expect(res.ok).toBe(true);
        expect(res.payload?.models).toEqual(options.expected);
      },
    );
  };

  test("models.list returns model catalog", async () => {
    seedPiCatalog();

    const res1 = await listModels();
    const res2 = await listModels();

    expect(res1.ok).toBe(true);
    expect(res2.ok).toBe(true);

    const models = res1.payload?.models ?? [];
    expect(models).toEqual(expectedSortedCatalog());

    expect(piSdkMock.discoverCalls).toBe(1);
  });

  test("models.list filters to allowlisted configured models by default", async () => {
    await expectAllowlistedModels({
      primary: "openai/gpt-test-z",
      models: {
        "openai/gpt-test-z": {},
        "anthropic/claude-test-a": {},
      },
      expected: [
        {
          id: "claude-test-a",
          name: "A-Model",
          provider: "anthropic",
          contextWindow: 200_000,
        },
        {
          id: "gpt-test-z",
          name: "gpt-test-z",
          provider: "openai",
        },
      ],
    });
  });

  test("models.list includes synthetic entries for allowlist models absent from catalog", async () => {
    await expectAllowlistedModels({
      primary: "openai/not-in-catalog",
      models: {
        "openai/not-in-catalog": {},
      },
      expected: [
        {
          id: "not-in-catalog",
          name: "not-in-catalog",
          provider: "openai",
        },
      ],
    });
  });

  test("models.list rejects unknown params", async () => {
    piSdkMock.enabled = true;
    piSdkMock.models = [{ id: "gpt-test-a", name: "A", provider: "openai" }];

    const res = await rpcReq(ws, "models.list", { extra: true });
    expect(res.ok).toBe(false);
    expect(res.error?.message ?? "").toMatch(/invalid models\.list params/i);
  });
});

describe("gateway server misc", () => {
  test("send dedupes by idempotencyKey", { timeout: 15_000 }, async () => {
    const prevRegistry = getActivePluginRegistry() ?? emptyRegistry;
    try {
      setActivePluginRegistry(whatsappRegistry);
      expect(getChannelPlugin("whatsapp")).toBeDefined();

      const idem = "same-key";
      const res1P = onceMessage(ws, (o) => o.type === "res" && o.id === "a1");
      const res2P = onceMessage(ws, (o) => o.type === "res" && o.id === "a2");
      const sendReq = (id: string) =>
        ws.send(
          JSON.stringify({
            type: "req",
            id,
            method: "send",
            params: {
              to: "+15550000000",
              channel: "whatsapp",
              message: "hi",
              idempotencyKey: idem,
            },
          }),
        );
      sendReq("a1");
      sendReq("a2");

      const res1 = await res1P;
      const res2 = await res2P;
      expect(res1.ok).toBe(true);
      expect(res2.ok).toBe(true);
      expect(res1.payload).toEqual(res2.payload);
    } finally {
      setActivePluginRegistry(prevRegistry);
    }
  });

  test("auto-enables configured channel plugins on startup", async () => {
    const configPath = process.env.DENEB_CONFIG_PATH;
    if (!configPath) {
      throw new Error("Missing DENEB_CONFIG_PATH");
    }
    await fs.mkdir(path.dirname(configPath), { recursive: true });
    await fs.writeFile(
      configPath,
      JSON.stringify(
        {
          channels: {
            discord: {
              token: "token-123",
            },
          },
        },
        null,
        2,
      ),
      "utf-8",
    );

    const autoPort = await getFreePort();
    const autoServer = await startGatewayServer(autoPort);
    await autoServer.close();

    const updated = JSON.parse(await fs.readFile(configPath, "utf-8")) as Record<string, unknown>;
    const channels = updated.channels as Record<string, unknown> | undefined;
    const discord = channels?.discord as Record<string, unknown> | undefined;
    expect(discord).toMatchObject({
      token: "token-123",
      enabled: true,
    });
  });

  test("refuses to start when port already bound", async () => {
    const { server: blocker, port: blockedPort } = await occupyPort();
    const startup = startGatewayServer(blockedPort);
    await expect(startup).rejects.toBeInstanceOf(GatewayLockError);
    await expect(startup).rejects.toThrow(/already listening/i);
    blocker.close();
  });

  test("releases port after close", async () => {
    const releasePort = await getFreePort();
    const releaseServer = await startGatewayServer(releasePort);
    await releaseServer.close();

    const probe = createServer();
    await new Promise<void>((resolve, reject) => {
      probe.once("error", reject);
      probe.listen(releasePort, "127.0.0.1", () => resolve());
    });
    await new Promise<void>((resolve, reject) =>
      probe.close((err) => (err ? reject(err) : resolve())),
    );
  });
});
