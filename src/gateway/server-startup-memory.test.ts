import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DenebConfig } from "../config/config.js";

const { getMemorySearchManagerMock } = vi.hoisted(() => ({
  getMemorySearchManagerMock: vi.fn(),
}));

vi.mock("../memory/index.js", () => ({
  getMemorySearchManager: getMemorySearchManagerMock,
}));

import { startGatewayMemoryBackend } from "./server-startup-memory.js";

function createVegaConfig(agents: DenebConfig["agents"]): DenebConfig {
  return {
    agents,
    memory: { backend: "vega", vega: {} },
  } as DenebConfig;
}

function createGatewayLogMock() {
  return { info: vi.fn(), warn: vi.fn() };
}

describe("startGatewayMemoryBackend", () => {
  beforeEach(() => {
    getMemorySearchManagerMock.mockClear();
  });

  it("skips initialization when memory backend is not vega", async () => {
    const cfg = {
      agents: { list: [{ id: "main", default: true }] },
      memory: { backend: "builtin" },
    } as DenebConfig;
    const log = { info: vi.fn(), warn: vi.fn() };

    await startGatewayMemoryBackend({ cfg, log });

    expect(getMemorySearchManagerMock).not.toHaveBeenCalled();
    expect(log.info).not.toHaveBeenCalled();
    expect(log.warn).not.toHaveBeenCalled();
  });

  it("initializes vega backend for each configured agent", async () => {
    const cfg = createVegaConfig({ list: [{ id: "ops", default: true }, { id: "main" }] });
    const log = createGatewayLogMock();
    getMemorySearchManagerMock.mockResolvedValue({ manager: { search: vi.fn() } });

    await startGatewayMemoryBackend({ cfg, log });

    expect(getMemorySearchManagerMock).toHaveBeenCalledTimes(2);
    expect(getMemorySearchManagerMock).toHaveBeenNthCalledWith(1, { cfg, agentId: "ops" });
    expect(getMemorySearchManagerMock).toHaveBeenNthCalledWith(2, { cfg, agentId: "main" });
    expect(log.info).toHaveBeenNthCalledWith(
      1,
      'memory startup initialization armed for agent "ops"',
    );
    expect(log.info).toHaveBeenNthCalledWith(
      2,
      'memory startup initialization armed for agent "main"',
    );
    expect(log.warn).not.toHaveBeenCalled();
  });

  it("logs a warning when vega manager init fails and continues with other agents", async () => {
    const cfg = createVegaConfig({ list: [{ id: "main", default: true }, { id: "ops" }] });
    const log = createGatewayLogMock();
    getMemorySearchManagerMock
      .mockResolvedValueOnce({ manager: null, error: "vega missing" })
      .mockResolvedValueOnce({ manager: { search: vi.fn() } });

    await startGatewayMemoryBackend({ cfg, log });

    expect(log.warn).toHaveBeenCalledWith(
      'memory startup initialization failed for agent "main": vega missing',
    );
    expect(log.info).toHaveBeenCalledWith('memory startup initialization armed for agent "ops"');
  });

  it("skips agents with memory search disabled", async () => {
    const cfg = createVegaConfig({
      defaults: { memorySearch: { enabled: true } },
      list: [
        { id: "main", default: true },
        { id: "ops", memorySearch: { enabled: false } },
      ],
    });
    const log = createGatewayLogMock();
    getMemorySearchManagerMock.mockResolvedValue({ manager: { search: vi.fn() } });

    await startGatewayMemoryBackend({ cfg, log });

    expect(getMemorySearchManagerMock).toHaveBeenCalledTimes(1);
    expect(getMemorySearchManagerMock).toHaveBeenCalledWith({ cfg, agentId: "main" });
    expect(log.info).toHaveBeenCalledWith('memory startup initialization armed for agent "main"');
    expect(log.warn).not.toHaveBeenCalled();
  });
});
