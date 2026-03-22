import type { BrowserActRequest } from "./client-actions-core.js";
import { evaluateViaPlaywright } from "./pw-tools-core.evaluate.js";
import {
  clickViaPlaywright,
  dragViaPlaywright,
  fillFormViaPlaywright,
  hoverViaPlaywright,
  pressKeyViaPlaywright,
  scrollIntoViewViaPlaywright,
  selectOptionViaPlaywright,
  typeViaPlaywright,
  waitForViaPlaywright,
} from "./pw-tools-core.interactions.js";
import { closePageViaPlaywright, resizeViewportViaPlaywright } from "./pw-tools-core.snapshot.js";

const MAX_BATCH_DEPTH = 5;
const MAX_BATCH_ACTIONS = 100;

async function executeSingleAction(
  action: BrowserActRequest,
  cdpUrl: string,
  targetId?: string,
  evaluateEnabled?: boolean,
  depth = 0,
): Promise<void> {
  if (depth > MAX_BATCH_DEPTH) {
    throw new Error(`Batch nesting depth exceeds maximum of ${MAX_BATCH_DEPTH}`);
  }
  const effectiveTargetId = action.targetId ?? targetId;
  switch (action.kind) {
    case "click":
      await clickViaPlaywright({
        cdpUrl,
        targetId: effectiveTargetId,
        ref: action.ref,
        selector: action.selector,
        doubleClick: action.doubleClick,
        button: action.button as "left" | "right" | "middle" | undefined,
        modifiers: action.modifiers as Array<
          "Alt" | "Control" | "ControlOrMeta" | "Meta" | "Shift"
        >,
        delayMs: action.delayMs,
        timeoutMs: action.timeoutMs,
      });
      break;
    case "type":
      await typeViaPlaywright({
        cdpUrl,
        targetId: effectiveTargetId,
        ref: action.ref,
        selector: action.selector,
        text: action.text,
        submit: action.submit,
        slowly: action.slowly,
        timeoutMs: action.timeoutMs,
      });
      break;
    case "press":
      await pressKeyViaPlaywright({
        cdpUrl,
        targetId: effectiveTargetId,
        key: action.key,
        delayMs: action.delayMs,
      });
      break;
    case "hover":
      await hoverViaPlaywright({
        cdpUrl,
        targetId: effectiveTargetId,
        ref: action.ref,
        selector: action.selector,
        timeoutMs: action.timeoutMs,
      });
      break;
    case "scrollIntoView":
      await scrollIntoViewViaPlaywright({
        cdpUrl,
        targetId: effectiveTargetId,
        ref: action.ref,
        selector: action.selector,
        timeoutMs: action.timeoutMs,
      });
      break;
    case "drag":
      await dragViaPlaywright({
        cdpUrl,
        targetId: effectiveTargetId,
        startRef: action.startRef,
        startSelector: action.startSelector,
        endRef: action.endRef,
        endSelector: action.endSelector,
        timeoutMs: action.timeoutMs,
      });
      break;
    case "select":
      await selectOptionViaPlaywright({
        cdpUrl,
        targetId: effectiveTargetId,
        ref: action.ref,
        selector: action.selector,
        values: action.values,
        timeoutMs: action.timeoutMs,
      });
      break;
    case "fill":
      await fillFormViaPlaywright({
        cdpUrl,
        targetId: effectiveTargetId,
        fields: action.fields,
        timeoutMs: action.timeoutMs,
      });
      break;
    case "resize":
      await resizeViewportViaPlaywright({
        cdpUrl,
        targetId: effectiveTargetId,
        width: action.width,
        height: action.height,
      });
      break;
    case "wait":
      if (action.fn && !evaluateEnabled) {
        throw new Error("wait --fn is disabled by config (browser.evaluateEnabled=false)");
      }
      await waitForViaPlaywright({
        cdpUrl,
        targetId: effectiveTargetId,
        timeMs: action.timeMs,
        text: action.text,
        textGone: action.textGone,
        selector: action.selector,
        url: action.url,
        loadState: action.loadState,
        fn: action.fn,
        timeoutMs: action.timeoutMs,
      });
      break;
    case "evaluate":
      if (!evaluateEnabled) {
        throw new Error("act:evaluate is disabled by config (browser.evaluateEnabled=false)");
      }
      await evaluateViaPlaywright({
        cdpUrl,
        targetId: effectiveTargetId,
        fn: action.fn,
        ref: action.ref,
        timeoutMs: action.timeoutMs,
      });
      break;
    case "close":
      await closePageViaPlaywright({
        cdpUrl,
        targetId: effectiveTargetId,
      });
      break;
    case "batch":
      await batchViaPlaywright({
        cdpUrl,
        targetId: effectiveTargetId,
        actions: action.actions,
        stopOnError: action.stopOnError,
        evaluateEnabled,
        depth: depth + 1,
      });
      break;
    default:
      throw new Error(`Unsupported batch action kind: ${(action as { kind: string }).kind}`);
  }
}

export async function batchViaPlaywright(opts: {
  cdpUrl: string;
  targetId?: string;
  actions: BrowserActRequest[];
  stopOnError?: boolean;
  evaluateEnabled?: boolean;
  depth?: number;
}): Promise<{ results: Array<{ ok: boolean; error?: string }> }> {
  const depth = opts.depth ?? 0;
  if (depth > MAX_BATCH_DEPTH) {
    throw new Error(`Batch nesting depth exceeds maximum of ${MAX_BATCH_DEPTH}`);
  }
  if (opts.actions.length > MAX_BATCH_ACTIONS) {
    throw new Error(`Batch exceeds maximum of ${MAX_BATCH_ACTIONS} actions`);
  }
  const results: Array<{ ok: boolean; error?: string }> = [];
  for (const action of opts.actions) {
    try {
      await executeSingleAction(action, opts.cdpUrl, opts.targetId, opts.evaluateEnabled, depth);
      results.push({ ok: true });
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      results.push({ ok: false, error: message });
      if (opts.stopOnError !== false) {
        break;
      }
    }
  }
  return { results };
}
