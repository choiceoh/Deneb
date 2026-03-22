import {
  ensurePageState,
  forceDisconnectPlaywrightForTarget,
  getPageForTargetId,
  refLocator,
  restoreRoleRefsForTarget,
} from "./pw-session.js";
import { normalizeTimeoutMs } from "./pw-tools-core.shared.js";

type TargetOpts = {
  cdpUrl: string;
  targetId?: string;
};

async function getRestoredPageForTarget(opts: TargetOpts) {
  const page = await getPageForTargetId(opts);
  ensurePageState(page);
  restoreRoleRefsForTarget({ cdpUrl: opts.cdpUrl, targetId: opts.targetId, page });
  return page;
}

async function awaitEvalWithAbort<T>(
  evalPromise: Promise<T>,
  abortPromise?: Promise<never>,
): Promise<T> {
  if (!abortPromise) {
    return await evalPromise;
  }
  try {
    return await Promise.race([evalPromise, abortPromise]);
  } catch (err) {
    // If abort wins the race, evaluate may reject later; avoid unhandled rejections.
    void evalPromise.catch(() => {});
    throw err;
  }
}

export async function evaluateViaPlaywright(opts: {
  cdpUrl: string;
  targetId?: string;
  fn: string;
  ref?: string;
  timeoutMs?: number;
  signal?: AbortSignal;
}): Promise<unknown> {
  const fnText = String(opts.fn ?? "").trim();
  if (!fnText) {
    throw new Error("function is required");
  }
  const page = await getRestoredPageForTarget(opts);
  // Clamp evaluate timeout to prevent permanently blocking Playwright's command queue.
  // Without this, a long-running async evaluate blocks all subsequent page operations
  // because Playwright serializes CDP commands per page.
  //
  // NOTE: Playwright's { timeout } on evaluate only applies to installing the function,
  // NOT to its execution time. We must inject a Promise.race timeout into the browser
  // context itself so async functions are bounded.
  const outerTimeout = normalizeTimeoutMs(opts.timeoutMs, 20_000);
  // Leave headroom for routing/serialization overhead so the outer request timeout
  // doesn't fire first and strand a long-running evaluate.
  let evaluateTimeout = Math.max(1000, Math.min(120_000, outerTimeout - 500));
  evaluateTimeout = Math.min(evaluateTimeout, outerTimeout);

  const signal = opts.signal;
  let abortListener: (() => void) | undefined;
  let abortReject: ((reason: unknown) => void) | undefined;
  let abortPromise: Promise<never> | undefined;
  if (signal) {
    abortPromise = new Promise((_, reject) => {
      abortReject = reject;
    });
    // Ensure the abort promise never becomes an unhandled rejection if we throw early.
    void abortPromise.catch(() => {});
  }
  if (signal) {
    const disconnect = () => {
      void forceDisconnectPlaywrightForTarget({
        cdpUrl: opts.cdpUrl,
        targetId: opts.targetId,
        reason: "evaluate aborted",
      }).catch(() => {});
    };
    if (signal.aborted) {
      disconnect();
      throw signal.reason ?? new Error("aborted");
    }
    abortListener = () => {
      disconnect();
      abortReject?.(signal.reason ?? new Error("aborted"));
    };
    signal.addEventListener("abort", abortListener, { once: true });
    // If the signal aborted between the initial check and listener registration, handle it.
    if (signal.aborted) {
      abortListener();
      throw signal.reason ?? new Error("aborted");
    }
  }

  try {
    if (opts.ref) {
      const locator = refLocator(page, opts.ref);
      // eslint-disable-next-line @typescript-eslint/no-implied-eval -- required for browser-context eval
      const elementEvaluator = new Function(
        "el",
        "args",
        `
        "use strict";
        var fnBody = args.fnBody, timeoutMs = args.timeoutMs;
        try {
          var candidate = eval("(" + fnBody + ")");
          var result = typeof candidate === "function" ? candidate(el) : candidate;
          if (result && typeof result.then === "function") {
            return Promise.race([
              result,
              new Promise(function(_, reject) {
                setTimeout(function() { reject(new Error("evaluate timed out after " + timeoutMs + "ms")); }, timeoutMs);
              })
            ]);
          }
          return result;
        } catch (err) {
          throw new Error("Invalid evaluate function: " + (err && err.message ? err.message : String(err)));
        }
        `,
      ) as (el: Element, args: { fnBody: string; timeoutMs: number }) => unknown;
      const evalPromise = locator.evaluate(elementEvaluator, {
        fnBody: fnText,
        timeoutMs: evaluateTimeout,
      });
      return await awaitEvalWithAbort(evalPromise, abortPromise);
    }

    // eslint-disable-next-line @typescript-eslint/no-implied-eval -- required for browser-context eval
    const browserEvaluator = new Function(
      "args",
      `
        "use strict";
        var fnBody = args.fnBody, timeoutMs = args.timeoutMs;
        try {
          var candidate = eval("(" + fnBody + ")");
          var result = typeof candidate === "function" ? candidate() : candidate;
          if (result && typeof result.then === "function") {
            return Promise.race([
              result,
              new Promise(function(_, reject) {
                setTimeout(function() { reject(new Error("evaluate timed out after " + timeoutMs + "ms")); }, timeoutMs);
              })
            ]);
          }
          return result;
        } catch (err) {
          throw new Error("Invalid evaluate function: " + (err && err.message ? err.message : String(err)));
        }
      `,
    ) as (args: { fnBody: string; timeoutMs: number }) => unknown;
    const evalPromise = page.evaluate(browserEvaluator, {
      fnBody: fnText,
      timeoutMs: evaluateTimeout,
    });
    return await awaitEvalWithAbort(evalPromise, abortPromise);
  } finally {
    if (signal && abortListener) {
      signal.removeEventListener("abort", abortListener);
    }
  }
}
