import { describe, vi } from "vitest";
import { discordOutbound } from "../../../../test/channel-outbounds.js";
import { whatsappOutbound } from "../../../../test/channel-outbounds.js";
import { slackOutbound } from "../../../../test/channel-outbounds.js";
import type { ReplyPayload } from "../../../auto-reply/types.js";
import { createDirectTextMediaOutbound } from "../outbound/direct-text-media.js";
import {
  installChannelOutboundPayloadContractSuite,
  primeChannelOutboundSendMock,
} from "./suites.js";

type PayloadHarnessParams = {
  payload: ReplyPayload;
  sendResults?: Array<{ messageId: string }>;
};

function createSlackHarness(params: PayloadHarnessParams) {
  const sendSlack = vi.fn();
  primeChannelOutboundSendMock(
    sendSlack,
    { messageId: "sl-1", channelId: "C12345", ts: "1234.5678" },
    params.sendResults,
  );
  const ctx = {
    cfg: {},
    to: "C12345",
    text: "",
    payload: params.payload,
    deps: {
      sendSlack,
    },
  };
  return {
    run: async () => await slackOutbound.sendPayload!(ctx),
    sendMock: sendSlack,
    to: ctx.to,
  };
}

function createDiscordHarness(params: PayloadHarnessParams) {
  const sendDiscord = vi.fn();
  primeChannelOutboundSendMock(
    sendDiscord,
    { messageId: "dc-1", channelId: "123456" },
    params.sendResults,
  );
  const ctx = {
    cfg: {},
    to: "channel:123456",
    text: "",
    payload: params.payload,
    deps: {
      sendDiscord,
    },
  };
  return {
    run: async () => await discordOutbound.sendPayload!(ctx),
    sendMock: sendDiscord,
    to: ctx.to,
  };
}

function createWhatsAppHarness(params: PayloadHarnessParams) {
  const sendWhatsApp = vi.fn();
  primeChannelOutboundSendMock(sendWhatsApp, { messageId: "wa-1" }, params.sendResults);
  const ctx = {
    cfg: {},
    to: "5511999999999@c.us",
    text: "",
    payload: params.payload,
    deps: {
      sendWhatsApp,
    },
  };
  return {
    run: async () => await whatsappOutbound.sendPayload!(ctx),
    sendMock: sendWhatsApp,
    to: ctx.to,
  };
}

function createDirectTextMediaHarness(params: PayloadHarnessParams) {
  const sendFn = vi.fn();
  primeChannelOutboundSendMock(sendFn, { messageId: "m1" }, params.sendResults);
  const outbound = createDirectTextMediaOutbound({
    channel: "imessage",
    resolveSender: () => sendFn,
    resolveMaxBytes: () => undefined,
    buildTextOptions: (opts) => opts as never,
    buildMediaOptions: (opts) => opts as never,
  });
  const ctx = {
    cfg: {},
    to: "user1",
    text: "",
    payload: params.payload,
  };
  return {
    run: async () => await outbound.sendPayload!(ctx),
    sendMock: sendFn,
    to: ctx.to,
  };
}

describe("channel outbound payload contract", () => {
  describe("slack", () => {
    installChannelOutboundPayloadContractSuite({
      channel: "slack",
      chunking: { mode: "passthrough", longTextLength: 5000 },
      createHarness: createSlackHarness,
    });
  });

  describe("discord", () => {
    installChannelOutboundPayloadContractSuite({
      channel: "discord",
      chunking: { mode: "passthrough", longTextLength: 3000 },
      createHarness: createDiscordHarness,
    });
  });

  describe("whatsapp", () => {
    installChannelOutboundPayloadContractSuite({
      channel: "whatsapp",
      chunking: { mode: "split", longTextLength: 5000, maxChunkLength: 4000 },
      createHarness: createWhatsAppHarness,
    });
  });

  describe("direct-text-media", () => {
    installChannelOutboundPayloadContractSuite({
      channel: "imessage",
      chunking: { mode: "split", longTextLength: 5000, maxChunkLength: 4000 },
      createHarness: createDirectTextMediaHarness,
    });
  });
});
