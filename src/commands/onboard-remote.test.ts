import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DenebConfig } from "../config/config.js";
import { captureEnv } from "../test-utils/env.js";
import type { WizardPrompter } from "../wizard/prompts.js";
import { createWizardPrompter } from "./test-wizard-helpers.js";

const { promptRemoteGatewayConfig } = await import("./onboard-remote.js");

function createPrompter(overrides: Partial<WizardPrompter>): WizardPrompter {
  return createWizardPrompter(overrides, { defaultSelect: "" });
}

function createSelectPrompter(
  responses: Partial<Record<string, string>>,
): WizardPrompter["select"] {
  return vi.fn(async (params) => {
    const value = responses[params.message];
    if (value !== undefined) {
      return value as never;
    }
    return (params.options[0]?.value ?? "") as never;
  });
}

describe("promptRemoteGatewayConfig", () => {
  const envSnapshot = captureEnv(["DENEB_ALLOW_INSECURE_PRIVATE_WS"]);

  async function runRemotePrompt(params: {
    text: WizardPrompter["text"];
    selectResponses: Partial<Record<string, string>>;
    confirm: boolean;
  }) {
    const cfg = {} as DenebConfig;
    const prompter = createPrompter({
      confirm: vi.fn(async () => params.confirm),
      select: createSelectPrompter(params.selectResponses),
      text: params.text,
    });
    const next = await promptRemoteGatewayConfig(cfg, prompter);
    return { next, prompter };
  }

  beforeEach(() => {
    vi.clearAllMocks();
    envSnapshot.restore();
  });

  it("validates insecure ws:// remote URLs and allows only loopback ws:// by default", async () => {
    const text: WizardPrompter["text"] = vi.fn(async (params) => {
      if (params.message === "Gateway WebSocket URL") {
        // ws:// to public IPs is rejected
        expect(params.validate?.("ws://203.0.113.10:18789")).toContain("Use wss://");
        // ws:// to private IPs remains blocked by default
        expect(params.validate?.("ws://10.0.0.8:18789")).toContain("Use wss://");
        expect(params.validate?.("ws://127.0.0.1:18789")).toBeUndefined();
        expect(params.validate?.("wss://remote.example.com:18789")).toBeUndefined();
        return "wss://remote.example.com:18789";
      }
      return "";
    }) as WizardPrompter["text"];

    const { next } = await runRemotePrompt({
      text,
      confirm: false,
      selectResponses: { "Gateway auth": "off" },
    });

    expect(next.gateway?.mode).toBe("remote");
    expect(next.gateway?.remote?.url).toBe("wss://remote.example.com:18789");
    expect(next.gateway?.remote?.token).toBeUndefined();
  });

  it("allows ws:// hostname remote URLs when DENEB_ALLOW_INSECURE_PRIVATE_WS=1", async () => {
    process.env.DENEB_ALLOW_INSECURE_PRIVATE_WS = "1";
    const text: WizardPrompter["text"] = vi.fn(async (params) => {
      if (params.message === "Gateway WebSocket URL") {
        expect(params.validate?.("ws://deneb-gateway.ai:18789")).toBeUndefined();
        expect(params.validate?.("ws://1.1.1.1:18789")).toContain("Use wss://");
        return "ws://deneb-gateway.ai:18789";
      }
      return "";
    }) as WizardPrompter["text"];

    const { next } = await runRemotePrompt({
      text,
      confirm: false,
      selectResponses: { "Gateway auth": "off" },
    });

    expect(next.gateway?.mode).toBe("remote");
    expect(next.gateway?.remote?.url).toBe("ws://deneb-gateway.ai:18789");
  });

  it("supports storing remote auth as an external env secret ref", async () => {
    process.env.DENEB_GATEWAY_TOKEN = "remote-token-value";
    const text: WizardPrompter["text"] = vi.fn(async (params) => {
      if (params.message === "Gateway WebSocket URL") {
        return "wss://remote.example.com:18789";
      }
      if (params.message === "Environment variable name") {
        return "DENEB_GATEWAY_TOKEN";
      }
      return "";
    }) as WizardPrompter["text"];

    const select: WizardPrompter["select"] = vi.fn(async (params) => {
      if (params.message === "Gateway auth") {
        return "token" as never;
      }
      if (params.message === "How do you want to provide this gateway token?") {
        return "ref" as never;
      }
      if (params.message === "Where is this gateway token stored?") {
        return "env" as never;
      }
      return (params.options[0]?.value ?? "") as never;
    });

    const cfg = {} as DenebConfig;
    const prompter = createPrompter({
      confirm: vi.fn(async () => false),
      select,
      text,
    });

    const next = await promptRemoteGatewayConfig(cfg, prompter);

    expect(next.gateway?.mode).toBe("remote");
    expect(next.gateway?.remote?.url).toBe("wss://remote.example.com:18789");
    expect(next.gateway?.remote?.token).toEqual({
      source: "env",
      provider: "default",
      id: "DENEB_GATEWAY_TOKEN",
    });
  });
});
