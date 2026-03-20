/**
 * Batch action normalization and validation utilities.
 *
 * Extracted from agent.act.ts to reduce file size.
 * Pure functions — no route context, no side effects.
 */
import type { BrowserActRequest, BrowserFormField } from "../client-actions-core.js";
import { normalizeBrowserFormField } from "../form-fields.js";
import { isActKind, parseClickButton, parseClickModifiers } from "./agent.act.shared.js";
import { toBoolean, toNumber, toStringArray, toStringOrEmpty } from "./utils.js";

export const SELECTOR_ALLOWED_KINDS: ReadonlySet<string> = new Set([
  "batch",
  "click",
  "drag",
  "hover",
  "scrollIntoView",
  "select",
  "type",
  "wait",
]);
export const MAX_BATCH_ACTIONS = 100;
export const MAX_BATCH_CLICK_DELAY_MS = 5_000;
export const MAX_BATCH_WAIT_TIME_MS = 30_000;

export function normalizeBoundedNonNegativeMs(
  value: unknown,
  fieldName: string,
  maxMs: number,
): number | undefined {
  const ms = toNumber(value);
  if (ms === undefined) {
    return undefined;
  }
  if (ms < 0) {
    throw new Error(`${fieldName} must be >= 0`);
  }
  const normalized = Math.floor(ms);
  if (normalized > maxMs) {
    throw new Error(`${fieldName} exceeds maximum of ${maxMs}ms`);
  }
  return normalized;
}

export function countBatchActions(actions: BrowserActRequest[]): number {
  let count = 0;
  for (const action of actions) {
    count += 1;
    if (action.kind === "batch") {
      count += countBatchActions(action.actions);
    }
  }
  return count;
}

export function validateBatchTargetIds(actions: BrowserActRequest[], targetId: string): string | null {
  for (const action of actions) {
    if (action.targetId && action.targetId !== targetId) {
      return "batched action targetId must match request targetId";
    }
    if (action.kind === "batch") {
      const nestedError = validateBatchTargetIds(action.actions, targetId);
      if (nestedError) {
        return nestedError;
      }
    }
  }
  return null;
}

export function normalizeBatchAction(value: unknown): BrowserActRequest {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("batch actions must be objects");
  }
  const raw = value as Record<string, unknown>;
  const kind = toStringOrEmpty(raw.kind);
  if (!isActKind(kind)) {
    throw new Error("batch actions must use a supported kind");
  }

  switch (kind) {
    case "click": {
      const ref = toStringOrEmpty(raw.ref) || undefined;
      const selector = toStringOrEmpty(raw.selector) || undefined;
      if (!ref && !selector) {
        throw new Error("click requires ref or selector");
      }
      const buttonRaw = toStringOrEmpty(raw.button);
      const button = buttonRaw ? parseClickButton(buttonRaw) : undefined;
      if (buttonRaw && !button) {
        throw new Error("click button must be left|right|middle");
      }
      const modifiersRaw = toStringArray(raw.modifiers) ?? [];
      const parsedModifiers = parseClickModifiers(modifiersRaw);
      if (parsedModifiers.error) {
        throw new Error(parsedModifiers.error);
      }
      const doubleClick = toBoolean(raw.doubleClick);
      const delayMs = normalizeBoundedNonNegativeMs(
        raw.delayMs,
        "click delayMs",
        MAX_BATCH_CLICK_DELAY_MS,
      );
      const timeoutMs = toNumber(raw.timeoutMs);
      const targetId = toStringOrEmpty(raw.targetId) || undefined;
      return {
        kind,
        ...(ref ? { ref } : {}),
        ...(selector ? { selector } : {}),
        ...(targetId ? { targetId } : {}),
        ...(doubleClick !== undefined ? { doubleClick } : {}),
        ...(button ? { button } : {}),
        ...(parsedModifiers.modifiers ? { modifiers: parsedModifiers.modifiers } : {}),
        ...(delayMs !== undefined ? { delayMs } : {}),
        ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      };
    }
    case "type": {
      const ref = toStringOrEmpty(raw.ref) || undefined;
      const selector = toStringOrEmpty(raw.selector) || undefined;
      const text = raw.text;
      if (!ref && !selector) {
        throw new Error("type requires ref or selector");
      }
      if (typeof text !== "string") {
        throw new Error("type requires text");
      }
      const targetId = toStringOrEmpty(raw.targetId) || undefined;
      const submit = toBoolean(raw.submit);
      const slowly = toBoolean(raw.slowly);
      const timeoutMs = toNumber(raw.timeoutMs);
      return {
        kind,
        ...(ref ? { ref } : {}),
        ...(selector ? { selector } : {}),
        text,
        ...(targetId ? { targetId } : {}),
        ...(submit !== undefined ? { submit } : {}),
        ...(slowly !== undefined ? { slowly } : {}),
        ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      };
    }
    case "press": {
      const key = toStringOrEmpty(raw.key);
      if (!key) {
        throw new Error("press requires key");
      }
      const targetId = toStringOrEmpty(raw.targetId) || undefined;
      const delayMs = toNumber(raw.delayMs);
      return {
        kind,
        key,
        ...(targetId ? { targetId } : {}),
        ...(delayMs !== undefined ? { delayMs } : {}),
      };
    }
    case "hover":
    case "scrollIntoView": {
      const ref = toStringOrEmpty(raw.ref) || undefined;
      const selector = toStringOrEmpty(raw.selector) || undefined;
      if (!ref && !selector) {
        throw new Error(`${kind} requires ref or selector`);
      }
      const targetId = toStringOrEmpty(raw.targetId) || undefined;
      const timeoutMs = toNumber(raw.timeoutMs);
      return {
        kind,
        ...(ref ? { ref } : {}),
        ...(selector ? { selector } : {}),
        ...(targetId ? { targetId } : {}),
        ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      };
    }
    case "drag": {
      const startRef = toStringOrEmpty(raw.startRef) || undefined;
      const startSelector = toStringOrEmpty(raw.startSelector) || undefined;
      const endRef = toStringOrEmpty(raw.endRef) || undefined;
      const endSelector = toStringOrEmpty(raw.endSelector) || undefined;
      if (!startRef && !startSelector) {
        throw new Error("drag requires startRef or startSelector");
      }
      if (!endRef && !endSelector) {
        throw new Error("drag requires endRef or endSelector");
      }
      const targetId = toStringOrEmpty(raw.targetId) || undefined;
      const timeoutMs = toNumber(raw.timeoutMs);
      return {
        kind,
        ...(startRef ? { startRef } : {}),
        ...(startSelector ? { startSelector } : {}),
        ...(endRef ? { endRef } : {}),
        ...(endSelector ? { endSelector } : {}),
        ...(targetId ? { targetId } : {}),
        ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      };
    }
    case "select": {
      const ref = toStringOrEmpty(raw.ref) || undefined;
      const selector = toStringOrEmpty(raw.selector) || undefined;
      const values = toStringArray(raw.values);
      if ((!ref && !selector) || !values?.length) {
        throw new Error("select requires ref/selector and values");
      }
      const targetId = toStringOrEmpty(raw.targetId) || undefined;
      const timeoutMs = toNumber(raw.timeoutMs);
      return {
        kind,
        ...(ref ? { ref } : {}),
        ...(selector ? { selector } : {}),
        values,
        ...(targetId ? { targetId } : {}),
        ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      };
    }
    case "fill": {
      const rawFields = Array.isArray(raw.fields) ? raw.fields : [];
      const fields = rawFields
        .map((field) => {
          if (!field || typeof field !== "object") {
            return null;
          }
          return normalizeBrowserFormField(field as Record<string, unknown>);
        })
        .filter((field): field is BrowserFormField => field !== null);
      if (!fields.length) {
        throw new Error("fill requires fields");
      }
      const targetId = toStringOrEmpty(raw.targetId) || undefined;
      const timeoutMs = toNumber(raw.timeoutMs);
      return {
        kind,
        fields,
        ...(targetId ? { targetId } : {}),
        ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      };
    }
    case "resize": {
      const width = toNumber(raw.width);
      const height = toNumber(raw.height);
      if (width === undefined || height === undefined) {
        throw new Error("resize requires width and height");
      }
      const targetId = toStringOrEmpty(raw.targetId) || undefined;
      return {
        kind,
        width,
        height,
        ...(targetId ? { targetId } : {}),
      };
    }
    case "wait": {
      const loadStateRaw = toStringOrEmpty(raw.loadState);
      const loadState =
        loadStateRaw === "load" ||
        loadStateRaw === "domcontentloaded" ||
        loadStateRaw === "networkidle"
          ? loadStateRaw
          : undefined;
      const timeMs = normalizeBoundedNonNegativeMs(
        raw.timeMs,
        "wait timeMs",
        MAX_BATCH_WAIT_TIME_MS,
      );
      const text = toStringOrEmpty(raw.text) || undefined;
      const textGone = toStringOrEmpty(raw.textGone) || undefined;
      const selector = toStringOrEmpty(raw.selector) || undefined;
      const url = toStringOrEmpty(raw.url) || undefined;
      const fn = toStringOrEmpty(raw.fn) || undefined;
      if (timeMs === undefined && !text && !textGone && !selector && !url && !loadState && !fn) {
        throw new Error(
          "wait requires at least one of: timeMs, text, textGone, selector, url, loadState, fn",
        );
      }
      const targetId = toStringOrEmpty(raw.targetId) || undefined;
      const timeoutMs = toNumber(raw.timeoutMs);
      return {
        kind,
        ...(timeMs !== undefined ? { timeMs } : {}),
        ...(text ? { text } : {}),
        ...(textGone ? { textGone } : {}),
        ...(selector ? { selector } : {}),
        ...(url ? { url } : {}),
        ...(loadState ? { loadState } : {}),
        ...(fn ? { fn } : {}),
        ...(targetId ? { targetId } : {}),
        ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      };
    }
    case "evaluate": {
      const fn = toStringOrEmpty(raw.fn);
      if (!fn) {
        throw new Error("evaluate requires fn");
      }
      const ref = toStringOrEmpty(raw.ref) || undefined;
      const targetId = toStringOrEmpty(raw.targetId) || undefined;
      const timeoutMs = toNumber(raw.timeoutMs);
      return {
        kind,
        fn,
        ...(ref ? { ref } : {}),
        ...(targetId ? { targetId } : {}),
        ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      };
    }
    case "close": {
      const targetId = toStringOrEmpty(raw.targetId) || undefined;
      return {
        kind,
        ...(targetId ? { targetId } : {}),
      };
    }
    case "batch": {
      const actions = Array.isArray(raw.actions) ? raw.actions.map(normalizeBatchAction) : [];
      if (!actions.length) {
        throw new Error("batch requires actions");
      }
      if (countBatchActions(actions) > MAX_BATCH_ACTIONS) {
        throw new Error(`batch exceeds maximum of ${MAX_BATCH_ACTIONS} actions`);
      }
      const targetId = toStringOrEmpty(raw.targetId) || undefined;
      const stopOnError = toBoolean(raw.stopOnError);
      return {
        kind,
        actions,
        ...(targetId ? { targetId } : {}),
        ...(stopOnError !== undefined ? { stopOnError } : {}),
      };
    }
  }
}
