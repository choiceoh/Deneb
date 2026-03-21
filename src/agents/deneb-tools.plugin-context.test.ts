import { beforeEach, describe, expect, it, vi } from "vitest";

const { resolvePluginToolsMock } = vi.hoisted(() => ({
  resolvePluginToolsMock: vi.fn((params?: unknown) => {
    void params;
    return [];
  }),
}));

vi.mock("../plugins/tools.js", () => ({
  resolvePluginTools: resolvePluginToolsMock,
  getPluginToolMeta: vi.fn(() => undefined),
}));

import { createDenebTools } from "./deneb-tools.js";
import { createDenebCodingTools } from "./pi-tools.js";

describe("createDenebTools plugin context", () => {
  beforeEach(() => {
    resolvePluginToolsMock.mockClear();
  });

  it("forwards trusted requester sender identity to plugin tool context", () => {
    createDenebTools({
      config: {} as never,
      requesterSenderId: "trusted-sender",
      senderIsOwner: true,
    });

    expect(resolvePluginToolsMock).toHaveBeenCalledWith(
      expect.objectContaining({
        context: expect.objectContaining({
          requesterSenderId: "trusted-sender",
          senderIsOwner: true,
        }),
      }),
    );
  });

  it("forwards ephemeral sessionId to plugin tool context", () => {
    createDenebTools({
      config: {} as never,
      agentSessionKey: "agent:main:telegram:direct:12345",
      sessionId: "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    });

    expect(resolvePluginToolsMock).toHaveBeenCalledWith(
      expect.objectContaining({
        context: expect.objectContaining({
          sessionKey: "agent:main:telegram:direct:12345",
          sessionId: "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
        }),
      }),
    );
  });

  it("forwards gateway subagent binding for plugin tools", () => {
    createDenebTools({
      config: {} as never,
      allowGatewaySubagentBinding: true,
    });

    expect(resolvePluginToolsMock).toHaveBeenCalledWith(
      expect.objectContaining({
        allowGatewaySubagentBinding: true,
      }),
    );
  });

  it("forwards gateway subagent binding through coding tools", () => {
    createDenebCodingTools({
      config: {} as never,
      allowGatewaySubagentBinding: true,
    });

    expect(resolvePluginToolsMock).toHaveBeenCalledWith(
      expect.objectContaining({
        allowGatewaySubagentBinding: true,
      }),
    );
  });
});
