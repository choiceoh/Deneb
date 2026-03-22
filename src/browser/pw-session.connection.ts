import type { Browser } from "playwright-core";
import { chromium } from "playwright-core";
import { formatErrorMessage } from "../infra/errors.js";
import { withNoProxyForCdpUrl } from "./cdp-proxy-bypass.js";
import {
  appendCdpPath,
  fetchJson,
  getHeadersWithAuth,
  normalizeCdpHttpBaseForJsonEndpoints,
  withCdpSocket,
} from "./cdp.helpers.js";
import { normalizeCdpWsUrl } from "./cdp.js";
import { getChromeWebSocketUrl } from "./chrome.js";

export type ConnectedBrowser = {
  browser: Browser;
  cdpUrl: string;
  onDisconnected?: () => void;
};

const cachedByCdpUrl = new Map<string, ConnectedBrowser>();
const connectingByCdpUrl = new Map<string, Promise<ConnectedBrowser>>();

function normalizeCdpUrl(raw: string) {
  return raw.replace(/\/$/, "");
}

export async function connectBrowser(
  cdpUrl: string,
  observeBrowser?: (browser: Browser) => void,
): Promise<ConnectedBrowser> {
  const normalized = normalizeCdpUrl(cdpUrl);
  const cached = cachedByCdpUrl.get(normalized);
  if (cached) {
    return cached;
  }
  const connecting = connectingByCdpUrl.get(normalized);
  if (connecting) {
    return await connecting;
  }

  const connectWithRetry = async (): Promise<ConnectedBrowser> => {
    let lastErr: unknown;
    for (let attempt = 0; attempt < 3; attempt += 1) {
      try {
        const timeout = 5000 + attempt * 2000;
        const wsUrl = await getChromeWebSocketUrl(normalized, timeout).catch(() => null);
        const endpoint = wsUrl ?? normalized;
        const headers = getHeadersWithAuth(endpoint);
        // Bypass proxy for loopback CDP connections (#31219)
        const browser = await withNoProxyForCdpUrl(endpoint, () =>
          chromium.connectOverCDP(endpoint, { timeout, headers }),
        );
        const onDisconnected = () => {
          const current = cachedByCdpUrl.get(normalized);
          if (current?.browser === browser) {
            cachedByCdpUrl.delete(normalized);
          }
        };
        const connected: ConnectedBrowser = { browser, cdpUrl: normalized, onDisconnected };
        cachedByCdpUrl.set(normalized, connected);
        browser.on("disconnected", onDisconnected);
        observeBrowser?.(browser);
        return connected;
      } catch (err) {
        lastErr = err;
        // Don't retry rate-limit errors; retrying worsens the 429.
        const errMsg = err instanceof Error ? err.message : String(err);
        if (errMsg.includes("rate limit")) {
          break;
        }
        const delay = 250 + attempt * 250;
        await new Promise((r) => setTimeout(r, delay));
      }
    }
    if (lastErr instanceof Error) {
      throw lastErr;
    }
    const message = lastErr ? formatErrorMessage(lastErr) : "CDP connect failed";
    throw new Error(message);
  };

  const pending = connectWithRetry().finally(() => {
    connectingByCdpUrl.delete(normalized);
  });
  connectingByCdpUrl.set(normalized, pending);

  return await pending;
}

export async function closePlaywrightBrowserConnection(opts?: { cdpUrl?: string }): Promise<void> {
  const normalized = opts?.cdpUrl ? normalizeCdpUrl(opts.cdpUrl) : null;

  if (normalized) {
    const cur = cachedByCdpUrl.get(normalized);
    cachedByCdpUrl.delete(normalized);
    connectingByCdpUrl.delete(normalized);
    if (!cur) {
      return;
    }
    if (cur.onDisconnected && typeof cur.browser.off === "function") {
      cur.browser.off("disconnected", cur.onDisconnected);
    }
    await cur.browser.close().catch(() => {});
    return;
  }

  const connections = Array.from(cachedByCdpUrl.values());
  cachedByCdpUrl.clear();
  connectingByCdpUrl.clear();
  for (const cur of connections) {
    if (cur.onDisconnected && typeof cur.browser.off === "function") {
      cur.browser.off("disconnected", cur.onDisconnected);
    }
    await cur.browser.close().catch(() => {});
  }
}

function cdpSocketNeedsAttach(wsUrl: string): boolean {
  try {
    const pathname = new URL(wsUrl).pathname;
    return (
      pathname === "/cdp" || pathname.endsWith("/cdp") || pathname.includes("/devtools/browser/")
    );
  } catch {
    return false;
  }
}

async function tryTerminateExecutionViaCdp(opts: {
  cdpUrl: string;
  targetId: string;
}): Promise<void> {
  const cdpHttpBase = normalizeCdpHttpBaseForJsonEndpoints(opts.cdpUrl);
  const listUrl = appendCdpPath(cdpHttpBase, "/json/list");

  const pages = await fetchJson<
    Array<{
      id?: string;
      webSocketDebuggerUrl?: string;
    }>
  >(listUrl, 2000).catch(() => null);
  if (!pages || pages.length === 0) {
    return;
  }

  const target = pages.find((p) => String(p.id ?? "").trim() === opts.targetId);
  const wsUrlRaw = String(target?.webSocketDebuggerUrl ?? "").trim();
  if (!wsUrlRaw) {
    return;
  }
  const wsUrl = normalizeCdpWsUrl(wsUrlRaw, cdpHttpBase);
  const needsAttach = cdpSocketNeedsAttach(wsUrl);

  const runWithTimeout = async <T>(work: Promise<T>, ms: number): Promise<T> => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const timeoutPromise = new Promise<never>((_, reject) => {
      timer = setTimeout(() => reject(new Error("CDP command timed out")), ms);
    });
    try {
      return await Promise.race([work, timeoutPromise]);
    } finally {
      if (timer) {
        clearTimeout(timer);
      }
    }
  };

  await withCdpSocket(
    wsUrl,
    async (send) => {
      let sessionId: string | undefined;
      try {
        if (needsAttach) {
          const attached = (await runWithTimeout(
            send("Target.attachToTarget", { targetId: opts.targetId, flatten: true }),
            1500,
          )) as { sessionId?: unknown };
          if (typeof attached?.sessionId === "string" && attached.sessionId.trim()) {
            sessionId = attached.sessionId;
          }
        }
        await runWithTimeout(send("Runtime.terminateExecution", undefined, sessionId), 1500);
        if (sessionId) {
          // Best-effort cleanup; not required for termination to take effect.
          void send("Target.detachFromTarget", { sessionId }).catch(() => {});
        }
      } catch {
        // Best-effort; ignore
      }
    },
    { handshakeTimeoutMs: 2000 },
  ).catch(() => {});
}

/**
 * Best-effort cancellation for stuck page operations.
 *
 * Playwright serializes CDP commands per page; a long-running or stuck operation (notably evaluate)
 * can block all subsequent commands. We cannot safely "cancel" an individual command, and we do
 * not want to close the actual Chromium tab. Instead, we disconnect Playwright's CDP connection
 * so in-flight commands fail fast and the next request reconnects transparently.
 *
 * IMPORTANT: We CANNOT call Connection.close() because Playwright shares a single Connection
 * across all objects (BrowserType, Browser, etc.). Closing it corrupts the entire Playwright
 * instance, preventing reconnection.
 *
 * Instead we:
 * 1. Null out `cached` so the next call triggers a fresh connectOverCDP
 * 2. Fire-and-forget browser.close() — it may hang but won't block us
 * 3. The next connectBrowser() creates a completely new CDP WebSocket connection
 *
 * The old browser.close() eventually resolves when the in-browser evaluate timeout fires,
 * or the old connection gets GC'd. Either way, it doesn't affect the fresh connection.
 */
export async function forceDisconnectPlaywrightForTarget(opts: {
  cdpUrl: string;
  targetId?: string;
  reason?: string;
}): Promise<void> {
  const normalized = normalizeCdpUrl(opts.cdpUrl);
  const cur = cachedByCdpUrl.get(normalized);
  if (!cur) {
    return;
  }
  cachedByCdpUrl.delete(normalized);
  // Also clear the per-url in-flight connect so the next call does a fresh connectOverCDP
  // rather than awaiting a stale promise.
  connectingByCdpUrl.delete(normalized);
  // Remove the "disconnected" listener to prevent the old browser's teardown
  // from racing with a fresh connection and nulling the new cached entry.
  if (cur.onDisconnected && typeof cur.browser.off === "function") {
    cur.browser.off("disconnected", cur.onDisconnected);
  }

  // Best-effort: kill any stuck JS to unblock the target's execution context before we
  // disconnect Playwright's CDP connection.
  const targetId = opts.targetId?.trim() || "";
  if (targetId) {
    await tryTerminateExecutionViaCdp({ cdpUrl: normalized, targetId }).catch(() => {});
  }

  // Fire-and-forget: don't await because browser.close() may hang on the stuck CDP pipe.
  cur.browser.close().catch(() => {});
}
