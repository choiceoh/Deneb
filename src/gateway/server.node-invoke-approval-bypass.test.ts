import crypto from "node:crypto";
import { afterAll, beforeAll, describe, expect, test } from "vitest";
import { WebSocket } from "ws";
import {
  deriveDeviceIdFromPublicKey,
  type DeviceIdentity,
  publicKeyRawBase64UrlFromPem,
} from "../infra/device-identity.js";
import { sleep } from "../utils.js";
import { GATEWAY_CLIENT_MODES, GATEWAY_CLIENT_NAMES } from "../utils/message-channel.js";
import { GatewayClient } from "./client.js";
import {
  connectReq,
  installGatewayTestHooks,
  onceMessage,
  rpcReq,
  startServerWithClient,
  trackConnectChallengeNonce,
} from "./test-helpers.js";

installGatewayTestHooks({ scope: "suite" });
const NODE_CONNECT_TIMEOUT_MS = 3_000;
const CONNECT_REQ_TIMEOUT_MS = 2_000;

function createDeviceIdentity(): DeviceIdentity {
  const { publicKey, privateKey } = crypto.generateKeyPairSync("ed25519");
  const publicKeyPem = publicKey.export({ type: "spki", format: "pem" }).toString();
  const privateKeyPem = privateKey.export({ type: "pkcs8", format: "pem" }).toString();
  const publicKeyRaw = publicKeyRawBase64UrlFromPem(publicKeyPem);
  const deviceId = deriveDeviceIdFromPublicKey(publicKeyRaw);
  if (!deviceId) {
    throw new Error("failed to create test device identity");
  }
  return {
    deviceId,
    publicKeyPem,
    privateKeyPem,
  };
}

async function expectNoForwardedInvoke(hasInvoke: () => boolean): Promise<void> {
  // Yield a couple of macrotasks so any accidental async forwarding would fire.
  await new Promise<void>((resolve) => setImmediate(resolve));
  await new Promise<void>((resolve) => setImmediate(resolve));
  expect(hasInvoke()).toBe(false);
}

async function getConnectedNodeId(ws: WebSocket): Promise<string> {
  const nodes = await rpcReq<{ nodes?: Array<{ nodeId: string; connected?: boolean }> }>(
    ws,
    "node.list",
    {},
  );
  expect(nodes.ok).toBe(true);
  const nodeId = nodes.payload?.nodes?.find((n) => n.connected)?.nodeId ?? "";
  expect(nodeId).toBeTruthy();
  return nodeId;
}

describe("node.invoke approval bypass", () => {
  let server: Awaited<ReturnType<typeof startServerWithClient>>["server"];
  let port: number;

  beforeAll(async () => {
    const started = await startServerWithClient("secret", { controlUiEnabled: true });
    server = started.server;
    port = started.port;
    started.ws.close();
  });

  afterAll(async () => {
    await server.close();
  });

  const approveAllPendingPairings = async () => {
    const { approveDevicePairing, listDevicePairing } = await import("../infra/device-pairing.js");
    const list = await listDevicePairing();
    for (const pending of list.pending) {
      await approveDevicePairing(pending.requestId);
    }
  };

  const connectOperatorWithRetry = async (
    scopes: string[],
    resolveDevice?: (nonce: string) => NonNullable<Parameters<typeof connectReq>[1]>["device"],
  ) => {
    const connectOnce = async () => {
      const ws = new WebSocket(`ws://127.0.0.1:${port}`);
      trackConnectChallengeNonce(ws);
      const challengePromise = resolveDevice
        ? onceMessage<{
            type?: string;
            event?: string;
            payload?: Record<string, unknown> | null;
          }>(ws, (o) => o.type === "event" && o.event === "connect.challenge")
        : null;
      await new Promise<void>((resolve) => ws.once("open", resolve));
      const nonce = (() => {
        if (!challengePromise) {
          return Promise.resolve("");
        }
        return challengePromise.then((challenge) => {
          const value = (challenge.payload as { nonce?: unknown } | undefined)?.nonce;
          expect(typeof value).toBe("string");
          return String(value);
        });
      })();
      const res = await connectReq(ws, {
        token: "secret",
        scopes,
        ...(resolveDevice ? { device: resolveDevice(await nonce) } : {}),
        timeoutMs: CONNECT_REQ_TIMEOUT_MS,
      });
      return { ws, res };
    };

    let { ws, res } = await connectOnce();
    const message =
      res && typeof res === "object" && "error" in res
        ? ((res as { error?: { message?: string } }).error?.message ?? "")
        : "";
    if (!res.ok && message.includes("pairing required")) {
      ws.close();
      await approveAllPendingPairings();
      ({ ws, res } = await connectOnce());
    }
    expect(res.ok).toBe(true);
    return ws;
  };

  const connectOperator = async (scopes: string[]) => {
    return await connectOperatorWithRetry(scopes);
  };

  const connectLinuxNode = async (
    onInvoke: (payload: unknown) => void,
    deviceIdentity?: DeviceIdentity,
  ) => {
    let readyResolve: (() => void) | null = null;
    const ready = new Promise<void>((resolve) => {
      readyResolve = resolve;
    });

    const resolvedDeviceIdentity = deviceIdentity ?? createDeviceIdentity();
    const client = new GatewayClient({
      url: `ws://127.0.0.1:${port}`,
      // Keep challenge timeout realistic in tests; 0 maps to a 250ms timeout and can
      // trigger reconnect backoff loops under load.
      connectDelayMs: 2_000,
      token: "secret",
      role: "node",
      clientName: GATEWAY_CLIENT_NAMES.NODE_HOST,
      clientVersion: "1.0.0",
      platform: "linux",
      mode: GATEWAY_CLIENT_MODES.NODE,
      scopes: [],
      commands: ["system.run"],
      deviceIdentity: resolvedDeviceIdentity,
      onHelloOk: () => readyResolve?.(),
      onEvent: (evt) => {
        if (evt.event !== "node.invoke.request") {
          return;
        }
        onInvoke(evt.payload);
        const payload = evt.payload as {
          id?: string;
          nodeId?: string;
        };
        const id = typeof payload?.id === "string" ? payload.id : "";
        const nodeId = typeof payload?.nodeId === "string" ? payload.nodeId : "";
        if (!id || !nodeId) {
          return;
        }
        void client.request("node.invoke.result", {
          id,
          nodeId,
          ok: true,
          payloadJSON: JSON.stringify({ ok: true }),
        });
      },
    });
    client.start();
    await Promise.race([
      ready,
      sleep(NODE_CONNECT_TIMEOUT_MS).then(() => {
        throw new Error("timeout waiting for node to connect");
      }),
    ]);
    return client;
  };

  test("rejects malformed/forbidden node.invoke payloads before forwarding", async () => {
    let sawInvoke = false;
    const node = await connectLinuxNode(() => {
      sawInvoke = true;
    });
    const ws = await connectOperator(["operator.write"]);
    try {
      const nodeId = await getConnectedNodeId(ws);
      const cases = [
        {
          name: "rawCommand mismatch",
          payload: {
            nodeId,
            command: "system.run",
            params: {
              command: ["uname", "-a"],
              rawCommand: "echo hi",
            },
            idempotencyKey: crypto.randomUUID(),
          },
          expectedError: "rawCommand does not match command",
        },
        {
          name: "approval flags without runId",
          payload: {
            nodeId,
            command: "system.run",
            params: {
              command: ["echo", "hi"],
              rawCommand: "echo hi",
              approved: true,
              approvalDecision: "allow-once",
            },
            idempotencyKey: crypto.randomUUID(),
          },
          expectedError: "params.runId",
        },
        {
          name: "forbidden execApprovals tool",
          payload: {
            nodeId,
            command: "system.execApprovals.set",
            params: { file: { version: 1, agents: {} }, baseHash: "nope" },
            idempotencyKey: crypto.randomUUID(),
          },
          expectedError: "exec.approvals.node",
        },
      ] as const;

      for (const testCase of cases) {
        const res = await rpcReq(ws, "node.invoke", testCase.payload);
        expect(res.ok, testCase.name).toBe(false);
        expect(res.error?.message ?? "", testCase.name).toContain(testCase.expectedError);
        await expectNoForwardedInvoke(() => sawInvoke);
      }
    } finally {
      ws.close();
      node.stop();
    }
  });
});
