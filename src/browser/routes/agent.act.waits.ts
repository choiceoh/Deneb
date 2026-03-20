/**
 * Wait/sleep utilities for browser agent actions.
 *
 * Extracted from agent.act.ts to reduce file size.
 */
import { evaluateChromeMcpScript } from "../chrome-mcp.js";
import { matchBrowserUrlPattern } from "../url-pattern.js";

export function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export function browserEvaluateDisabledMessage(action: "wait" | "evaluate"): string {
  return [
    action === "wait"
      ? "wait --fn is disabled by config (browser.evaluateEnabled=false)."
      : "act:evaluate is disabled by config (browser.evaluateEnabled=false).",
    "Docs: /gateway/configuration#browser-openclaw-managed-browser",
  ].join("\n");
}

export function buildExistingSessionWaitPredicate(params: {
  text?: string;
  textGone?: string;
  selector?: string;
  loadState?: "load" | "domcontentloaded" | "networkidle";
  fn?: string;
}): string | null {
  const checks: string[] = [];
  if (params.text) {
    checks.push(`Boolean(document.body?.innerText?.includes(${JSON.stringify(params.text)}))`);
  }
  if (params.textGone) {
    checks.push(`!document.body?.innerText?.includes(${JSON.stringify(params.textGone)})`);
  }
  if (params.selector) {
    checks.push(`Boolean(document.querySelector(${JSON.stringify(params.selector)}))`);
  }
  if (params.loadState === "domcontentloaded") {
    checks.push(`document.readyState === "interactive" || document.readyState === "complete"`);
  } else if (params.loadState === "load") {
    checks.push(`document.readyState === "complete"`);
  }
  if (params.fn) {
    checks.push(`Boolean(await (${params.fn})())`);
  }
  if (checks.length === 0) {
    return null;
  }
  return checks.length === 1 ? checks[0] : checks.map((check) => `(${check})`).join(" && ");
}

export async function waitForExistingSessionCondition(params: {
  profileName: string;
  userDataDir: string;
  targetId: string;
  timeMs?: number;
  text?: string;
  textGone?: string;
  selector?: string;
  url?: string;
  loadState?: "load" | "domcontentloaded" | "networkidle";
  fn?: string;
  timeoutMs?: number;
}): Promise<void> {
  const { profileName, userDataDir, targetId } = params;
  const timeoutMs = params.timeoutMs ?? 30_000;
  const deadline = Date.now() + timeoutMs;

  if (params.timeMs) {
    await sleep(params.timeMs);
    if (
      !params.text &&
      !params.textGone &&
      !params.selector &&
      !params.url &&
      !params.loadState &&
      !params.fn
    ) {
      return;
    }
  }

  const predicate = buildExistingSessionWaitPredicate({
    text: params.text,
    textGone: params.textGone,
    selector: params.selector,
    loadState: params.loadState,
    fn: params.fn,
  });

  while (Date.now() < deadline) {
    let predicateResult = true;
    if (predicate) {
      const evalResult = (await evaluateChromeMcpScript({
        profileName,
        userDataDir,
        targetId,
        fn: `(async () => { return ${predicate}; })()`,
      })) as { result?: unknown };
      predicateResult = evalResult.result === true || evalResult.result === "true";
    }
    if (params.url) {
      const urlResult = (await evaluateChromeMcpScript({
        profileName,
        userDataDir,
        targetId,
        fn: "window.location.href",
      })) as { result?: unknown };
      const currentUrl = typeof urlResult.result === "string" ? urlResult.result : "";
      if (!matchBrowserUrlPattern(currentUrl, params.url)) {
        predicateResult = false;
      }
    }
    if (predicateResult) {
      return;
    }
    await sleep(250);
  }
  throw new Error(`wait timed out after ${timeoutMs}ms`);
}
