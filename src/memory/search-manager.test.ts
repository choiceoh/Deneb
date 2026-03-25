import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DenebConfig } from "../config/config.js";

function createManagerStatus(params: {
  backend: "vega" | "builtin";
  provider: string;
  model: string;
  requestedProvider: string;
  withMemorySourceCounts?: boolean;
}) {
  const base = {
    backend: params.backend,
    provider: params.provider,
    model: params.model,
    requestedProvider: params.requestedProvider,
    files: 0,
    chunks: 0,
    dirty: false,
    workspaceDir: "/tmp",
    dbPath: "/tmp/index.sqlite",
  };
  if (!params.withMemorySourceCounts) {
    return base;
  }
  return {
    ...base,
    sources: ["memory" as const],
    sourceCounts: [{ source: "memory" as const, files: 0, chunks: 0 }],
  };
}

function createManagerMock(params: {
  backend: "vega" | "builtin";
  provider: string;
  model: string;
  requestedProvider: string;
  searchResults?: Array<{
    path: string;
    startLine: number;
    endLine: number;
    score: number;
    snippet: string;
    source: "memory";
  }>;
  withMemorySourceCounts?: boolean;
}) {
  return {
    search: vi.fn(async () => params.searchResults ?? []),
    readFile: vi.fn(async () => ({ text: "", path: "MEMORY.md" })),
    status: vi.fn(() =>
      createManagerStatus({
        backend: params.backend,
        provider: params.provider,
        model: params.model,
        requestedProvider: params.requestedProvider,
        withMemorySourceCounts: params.withMemorySourceCounts,
      }),
    ),
    sync: vi.fn(async () => {}),
    probeEmbeddingAvailability: vi.fn(async () => ({ ok: true })),
    probeVectorAvailability: vi.fn(async () => true),
    close: vi.fn(async () => {}),
  };
}

const mockPrimary = vi.hoisted(() => ({
  ...createManagerMock({
    backend: "vega",
    provider: "vega",
    model: "vega",
    requestedProvider: "vega",
    withMemorySourceCounts: true,
  }),
}));

const fallbackManager = vi.hoisted(() => ({
  ...createManagerMock({
    backend: "builtin",
    provider: "openai",
    model: "text-embedding-3-small",
    requestedProvider: "openai",
    searchResults: [
      {
        path: "MEMORY.md",
        startLine: 1,
        endLine: 1,
        score: 1,
        snippet: "fallback",
        source: "memory",
      },
    ],
  }),
}));

const fallbackSearch = fallbackManager.search;
const mockMemoryIndexGet = vi.hoisted(() => vi.fn(async () => fallbackManager));
const mockCloseAllMemoryIndexManagers = vi.hoisted(() => vi.fn(async () => {}));

vi.mock("./vega-manager.js", () => ({
  VegaMemoryManager: {
    create: vi.fn(async () => mockPrimary),
  },
}));

vi.mock("./manager-runtime.js", () => ({
  MemoryIndexManager: {
    get: mockMemoryIndexGet,
  },
  closeAllMemoryIndexManagers: mockCloseAllMemoryIndexManagers,
}));

import { closeAllMemorySearchManagers, getMemorySearchManager } from "./search-manager.js";
import { VegaMemoryManager } from "./vega-manager.js";
// oxlint-disable-next-line typescript/unbound-method -- mocked static function
const createVegaManagerMock = vi.mocked(VegaMemoryManager.create);

type SearchManagerResult = Awaited<ReturnType<typeof getMemorySearchManager>>;
type SearchManager = NonNullable<SearchManagerResult["manager"]>;

function createVegaCfg(agentId: string): DenebConfig {
  return {
    memory: { backend: "vega", vega: {} },
    agents: { list: [{ id: agentId, default: true, workspace: "/tmp/workspace" }] },
  };
}

function requireManager(result: SearchManagerResult): SearchManager {
  expect(result.manager).toBeTruthy();
  if (!result.manager) {
    throw new Error("manager missing");
  }
  return result.manager;
}

async function createFailedVegaSearchHarness(params: { agentId: string; errorMessage: string }) {
  const cfg = createVegaCfg(params.agentId);
  mockPrimary.search.mockRejectedValueOnce(new Error(params.errorMessage));
  const first = await getMemorySearchManager({ cfg, agentId: params.agentId });
  return { cfg, manager: requireManager(first), firstResult: first };
}

beforeEach(async () => {
  await closeAllMemorySearchManagers();
  mockPrimary.search.mockClear();
  mockPrimary.readFile.mockClear();
  mockPrimary.status.mockClear();
  mockPrimary.sync.mockClear();
  mockPrimary.probeEmbeddingAvailability.mockClear();
  mockPrimary.probeVectorAvailability.mockClear();
  mockPrimary.close.mockClear();
  fallbackSearch.mockClear();
  fallbackManager.readFile.mockClear();
  fallbackManager.status.mockClear();
  fallbackManager.sync.mockClear();
  fallbackManager.probeEmbeddingAvailability.mockClear();
  fallbackManager.probeVectorAvailability.mockClear();
  fallbackManager.close.mockClear();
  mockCloseAllMemoryIndexManagers.mockClear();
  mockMemoryIndexGet.mockClear();
  mockMemoryIndexGet.mockResolvedValue(fallbackManager);
  createVegaManagerMock.mockClear();
});

describe("getMemorySearchManager caching", () => {
  it("reuses the same Vega manager instance for repeated calls", async () => {
    const cfg = createVegaCfg("main");

    const first = await getMemorySearchManager({ cfg, agentId: "main" });
    const second = await getMemorySearchManager({ cfg, agentId: "main" });

    expect(first.manager).toBe(second.manager);
    // oxlint-disable-next-line typescript/unbound-method
    expect(createVegaManagerMock).toHaveBeenCalledTimes(1);
  });

  it("evicts failed vega wrapper so next call retries vega", async () => {
    const retryAgentId = "retry-agent";
    const {
      cfg,
      manager: firstManager,
      firstResult: first,
    } = await createFailedVegaSearchHarness({
      agentId: retryAgentId,
      errorMessage: "vega query failed",
    });

    const fallbackResults = await firstManager.search("hello");
    expect(fallbackResults).toHaveLength(1);
    expect(fallbackResults[0]?.path).toBe("MEMORY.md");

    const second = await getMemorySearchManager({ cfg, agentId: retryAgentId });
    requireManager(second);
    expect(second.manager).not.toBe(first.manager);
    // oxlint-disable-next-line typescript/unbound-method
    expect(createVegaManagerMock).toHaveBeenCalledTimes(2);
  });

  it("does not cache status-only vega managers", async () => {
    const agentId = "status-agent";
    const cfg = createVegaCfg(agentId);

    const first = await getMemorySearchManager({ cfg, agentId, purpose: "status" });
    const second = await getMemorySearchManager({ cfg, agentId, purpose: "status" });

    requireManager(first);
    requireManager(second);
    // oxlint-disable-next-line typescript/unbound-method
    expect(createVegaManagerMock).toHaveBeenCalledTimes(2);
    // oxlint-disable-next-line typescript/unbound-method
    expect(createVegaManagerMock).toHaveBeenNthCalledWith(1, expect.objectContaining({ agentId }));
    // oxlint-disable-next-line typescript/unbound-method
    expect(createVegaManagerMock).toHaveBeenNthCalledWith(2, expect.objectContaining({ agentId }));
  });

  it("does not evict a newer cached wrapper when closing an older failed wrapper", async () => {
    const retryAgentId = "retry-agent-close";
    const {
      cfg,
      manager: firstManager,
      firstResult: first,
    } = await createFailedVegaSearchHarness({
      agentId: retryAgentId,
      errorMessage: "vega query failed",
    });
    await firstManager.search("hello");

    const second = await getMemorySearchManager({ cfg, agentId: retryAgentId });
    const secondManager = requireManager(second);
    expect(second.manager).not.toBe(first.manager);

    await firstManager.close?.();

    const third = await getMemorySearchManager({ cfg, agentId: retryAgentId });
    expect(third.manager).toBe(secondManager);
    // oxlint-disable-next-line typescript/unbound-method
    expect(createVegaManagerMock).toHaveBeenCalledTimes(2);
  });

  it("falls back to builtin search when vega fails with sqlite busy", async () => {
    const retryAgentId = "retry-agent-busy";
    const { manager: firstManager } = await createFailedVegaSearchHarness({
      agentId: retryAgentId,
      errorMessage: "vega index busy while reading results: SQLITE_BUSY: database is locked",
    });

    const results = await firstManager.search("hello");
    expect(results).toHaveLength(1);
    expect(results[0]?.path).toBe("MEMORY.md");
    expect(fallbackSearch).toHaveBeenCalledTimes(1);
  });

  it("keeps original vega error when fallback manager initialization fails", async () => {
    const retryAgentId = "retry-agent-no-fallback-auth";
    const { manager: firstManager } = await createFailedVegaSearchHarness({
      agentId: retryAgentId,
      errorMessage: "vega query failed",
    });
    mockMemoryIndexGet.mockRejectedValueOnce(new Error("No API key found for provider openai"));

    await expect(firstManager.search("hello")).rejects.toThrow("vega query failed");
  });

  it("closes cached managers on global teardown", async () => {
    const cfg = createVegaCfg("teardown-agent");
    const first = await getMemorySearchManager({ cfg, agentId: "teardown-agent" });
    const firstManager = requireManager(first);

    await closeAllMemorySearchManagers();

    expect(mockPrimary.close).toHaveBeenCalledTimes(1);
    expect(mockCloseAllMemoryIndexManagers).toHaveBeenCalledTimes(1);

    const second = await getMemorySearchManager({ cfg, agentId: "teardown-agent" });
    expect(second.manager).toBeTruthy();
    expect(second.manager).not.toBe(firstManager);
    // oxlint-disable-next-line typescript/unbound-method
    expect(createVegaManagerMock).toHaveBeenCalledTimes(2);
  });

  it("closes builtin index managers on teardown after runtime is loaded", async () => {
    const retryAgentId = "teardown-with-fallback";
    const { manager } = await createFailedVegaSearchHarness({
      agentId: retryAgentId,
      errorMessage: "vega query failed",
    });
    await manager.search("hello");

    await closeAllMemorySearchManagers();

    expect(mockCloseAllMemoryIndexManagers).toHaveBeenCalledTimes(1);
  });
});
