import type { ChildProcessWithoutNullStreams } from "node:child_process";
import { EventEmitter } from "node:events";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./server-context.chrome-test-harness.js";
import * as chromeModule from "./chrome.js";
import type { RunningChrome } from "./chrome.js";
import type { BrowserServerState } from "./server-context.js";
import { createBrowserRouteContext } from "./server-context.js";

function makeBrowserState(): BrowserServerState {
  return {
    // oxlint-disable-next-line typescript/no-explicit-any
    server: null as any,
    port: 0,
    resolved: {
      enabled: true,
      controlPort: 18791,
      cdpProtocol: "http",
      cdpHost: "127.0.0.1",
      cdpIsLoopback: true,
      cdpPortRangeStart: 18800,
      cdpPortRangeEnd: 18810,
      evaluateEnabled: false,
      remoteCdpTimeoutMs: 1500,
      remoteCdpHandshakeTimeoutMs: 3000,
      extraArgs: [],
      color: "#FF4500",
      headless: true,
      noSandbox: false,
      attachOnly: false,
      ssrfPolicy: { allowPrivateNetwork: true },
      defaultProfile: "deneb",
      profiles: {
        deneb: { cdpPort: 18800, color: "#FF4500" },
      },
    },
    profiles: new Map(),
  };
}

function mockLaunchedChrome(
  launchDenebChrome: { mockResolvedValue: (value: RunningChrome) => unknown },
  pid: number,
) {
  const proc = new EventEmitter() as unknown as ChildProcessWithoutNullStreams;
  launchDenebChrome.mockResolvedValue({
    pid,
    exe: { kind: "chromium", path: "/usr/bin/chromium" },
    userDataDir: "/tmp/deneb-test",
    cdpPort: 18800,
    startedAt: Date.now(),
    proc,
  });
}

function setupEnsureBrowserAvailableHarness() {
  vi.useFakeTimers();

  const launchDenebChrome = vi.mocked(chromeModule.launchDenebChrome);
  const stopDenebChrome = vi.mocked(chromeModule.stopDenebChrome);
  const isChromeReachable = vi.mocked(chromeModule.isChromeReachable);
  const isChromeCdpReady = vi.mocked(chromeModule.isChromeCdpReady);
  isChromeReachable.mockResolvedValue(false);

  const state = makeBrowserState();
  const ctx = createBrowserRouteContext({ getState: () => state });
  const profile = ctx.forProfile("deneb");

  return { launchDenebChrome, stopDenebChrome, isChromeCdpReady, profile };
}

afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
  vi.restoreAllMocks();
});

describe("browser server-context ensureBrowserAvailable", () => {
  it("waits for CDP readiness after launching to avoid follow-up PortInUseError races (#21149)", async () => {
    const { launchDenebChrome, stopDenebChrome, isChromeCdpReady, profile } =
      setupEnsureBrowserAvailableHarness();
    isChromeCdpReady.mockResolvedValueOnce(false).mockResolvedValue(true);
    mockLaunchedChrome(launchDenebChrome, 123);

    const promise = profile.ensureBrowserAvailable();
    await vi.advanceTimersByTimeAsync(100);
    await expect(promise).resolves.toBeUndefined();

    expect(launchDenebChrome).toHaveBeenCalledTimes(1);
    expect(isChromeCdpReady).toHaveBeenCalled();
    expect(stopDenebChrome).not.toHaveBeenCalled();
  });

  it("stops launched chrome when CDP readiness never arrives", async () => {
    const { launchDenebChrome, stopDenebChrome, isChromeCdpReady, profile } =
      setupEnsureBrowserAvailableHarness();
    isChromeCdpReady.mockResolvedValue(false);
    mockLaunchedChrome(launchDenebChrome, 321);

    const promise = profile.ensureBrowserAvailable();
    const rejected = expect(promise).rejects.toThrow("not reachable after start");
    await vi.advanceTimersByTimeAsync(8100);
    await rejected;

    expect(launchDenebChrome).toHaveBeenCalledTimes(1);
    expect(stopDenebChrome).toHaveBeenCalledTimes(1);
  });
});
