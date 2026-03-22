import type { BrowserFormField } from "./client-actions-core.js";
import { DEFAULT_FILL_FIELD_TYPE } from "./form-fields.js";
import {
  ensurePageState,
  getPageForTargetId,
  refLocator,
  restoreRoleRefsForTarget,
} from "./pw-session.js";
import {
  normalizeTimeoutMs,
  requireRef,
  requireRefOrSelector,
  toAIFriendlyError,
} from "./pw-tools-core.shared.js";

type TargetOpts = {
  cdpUrl: string;
  targetId?: string;
};

const MAX_CLICK_DELAY_MS = 5_000;
const MAX_WAIT_TIME_MS = 30_000;

function resolveBoundedDelayMs(value: number | undefined, label: string, maxMs: number): number {
  const normalized = Math.floor(value ?? 0);
  if (!Number.isFinite(normalized) || normalized < 0) {
    throw new Error(`${label} must be >= 0`);
  }
  if (normalized > maxMs) {
    throw new Error(`${label} exceeds maximum of ${maxMs}ms`);
  }
  return normalized;
}

async function getRestoredPageForTarget(opts: TargetOpts) {
  const page = await getPageForTargetId(opts);
  ensurePageState(page);
  restoreRoleRefsForTarget({ cdpUrl: opts.cdpUrl, targetId: opts.targetId, page });
  return page;
}

function resolveInteractionTimeoutMs(timeoutMs?: number): number {
  return Math.max(500, Math.min(60_000, Math.floor(timeoutMs ?? 8000)));
}

export async function highlightViaPlaywright(opts: {
  cdpUrl: string;
  targetId?: string;
  ref: string;
}): Promise<void> {
  const page = await getRestoredPageForTarget(opts);
  const ref = requireRef(opts.ref);
  try {
    await refLocator(page, ref).highlight();
  } catch (err) {
    throw toAIFriendlyError(err, ref);
  }
}

export async function clickViaPlaywright(opts: {
  cdpUrl: string;
  targetId?: string;
  ref?: string;
  selector?: string;
  doubleClick?: boolean;
  button?: "left" | "right" | "middle";
  modifiers?: Array<"Alt" | "Control" | "ControlOrMeta" | "Meta" | "Shift">;
  delayMs?: number;
  timeoutMs?: number;
}): Promise<void> {
  const resolved = requireRefOrSelector(opts.ref, opts.selector);
  const page = await getRestoredPageForTarget(opts);
  const label = resolved.ref ?? resolved.selector!;
  const locator = resolved.ref
    ? refLocator(page, requireRef(resolved.ref))
    : page.locator(resolved.selector!);
  const timeout = resolveInteractionTimeoutMs(opts.timeoutMs);
  try {
    const delayMs = resolveBoundedDelayMs(opts.delayMs, "click delayMs", MAX_CLICK_DELAY_MS);
    if (delayMs > 0) {
      await locator.hover({ timeout });
      await new Promise((resolve) => setTimeout(resolve, delayMs));
    }
    if (opts.doubleClick) {
      await locator.dblclick({
        timeout,
        button: opts.button,
        modifiers: opts.modifiers,
      });
    } else {
      await locator.click({
        timeout,
        button: opts.button,
        modifiers: opts.modifiers,
      });
    }
  } catch (err) {
    throw toAIFriendlyError(err, label);
  }
}

export async function hoverViaPlaywright(opts: {
  cdpUrl: string;
  targetId?: string;
  ref?: string;
  selector?: string;
  timeoutMs?: number;
}): Promise<void> {
  const resolved = requireRefOrSelector(opts.ref, opts.selector);
  const page = await getRestoredPageForTarget(opts);
  const label = resolved.ref ?? resolved.selector!;
  const locator = resolved.ref
    ? refLocator(page, requireRef(resolved.ref))
    : page.locator(resolved.selector!);
  try {
    await locator.hover({
      timeout: resolveInteractionTimeoutMs(opts.timeoutMs),
    });
  } catch (err) {
    throw toAIFriendlyError(err, label);
  }
}

export async function dragViaPlaywright(opts: {
  cdpUrl: string;
  targetId?: string;
  startRef?: string;
  startSelector?: string;
  endRef?: string;
  endSelector?: string;
  timeoutMs?: number;
}): Promise<void> {
  const resolvedStart = requireRefOrSelector(opts.startRef, opts.startSelector);
  const resolvedEnd = requireRefOrSelector(opts.endRef, opts.endSelector);
  const page = await getRestoredPageForTarget(opts);
  const startLocator = resolvedStart.ref
    ? refLocator(page, requireRef(resolvedStart.ref))
    : page.locator(resolvedStart.selector!);
  const endLocator = resolvedEnd.ref
    ? refLocator(page, requireRef(resolvedEnd.ref))
    : page.locator(resolvedEnd.selector!);
  const startLabel = resolvedStart.ref ?? resolvedStart.selector!;
  const endLabel = resolvedEnd.ref ?? resolvedEnd.selector!;
  try {
    await startLocator.dragTo(endLocator, {
      timeout: resolveInteractionTimeoutMs(opts.timeoutMs),
    });
  } catch (err) {
    throw toAIFriendlyError(err, `${startLabel} -> ${endLabel}`);
  }
}

export async function selectOptionViaPlaywright(opts: {
  cdpUrl: string;
  targetId?: string;
  ref?: string;
  selector?: string;
  values: string[];
  timeoutMs?: number;
}): Promise<void> {
  const resolved = requireRefOrSelector(opts.ref, opts.selector);
  if (!opts.values?.length) {
    throw new Error("values are required");
  }
  const page = await getRestoredPageForTarget(opts);
  const label = resolved.ref ?? resolved.selector!;
  const locator = resolved.ref
    ? refLocator(page, requireRef(resolved.ref))
    : page.locator(resolved.selector!);
  try {
    await locator.selectOption(opts.values, {
      timeout: resolveInteractionTimeoutMs(opts.timeoutMs),
    });
  } catch (err) {
    throw toAIFriendlyError(err, label);
  }
}

export async function pressKeyViaPlaywright(opts: {
  cdpUrl: string;
  targetId?: string;
  key: string;
  delayMs?: number;
}): Promise<void> {
  const key = String(opts.key ?? "").trim();
  if (!key) {
    throw new Error("key is required");
  }
  const page = await getPageForTargetId(opts);
  ensurePageState(page);
  await page.keyboard.press(key, {
    delay: Math.max(0, Math.floor(opts.delayMs ?? 0)),
  });
}

export async function typeViaPlaywright(opts: {
  cdpUrl: string;
  targetId?: string;
  ref?: string;
  selector?: string;
  text: string;
  submit?: boolean;
  slowly?: boolean;
  timeoutMs?: number;
}): Promise<void> {
  const resolved = requireRefOrSelector(opts.ref, opts.selector);
  const text = String(opts.text ?? "");
  const page = await getRestoredPageForTarget(opts);
  const label = resolved.ref ?? resolved.selector!;
  const locator = resolved.ref
    ? refLocator(page, requireRef(resolved.ref))
    : page.locator(resolved.selector!);
  const timeout = resolveInteractionTimeoutMs(opts.timeoutMs);
  try {
    if (opts.slowly) {
      await locator.click({ timeout });
      await locator.type(text, { timeout, delay: 75 });
    } else {
      await locator.fill(text, { timeout });
    }
    if (opts.submit) {
      await locator.press("Enter", { timeout });
    }
  } catch (err) {
    throw toAIFriendlyError(err, label);
  }
}

export async function fillFormViaPlaywright(opts: {
  cdpUrl: string;
  targetId?: string;
  fields: BrowserFormField[];
  timeoutMs?: number;
}): Promise<void> {
  const page = await getRestoredPageForTarget(opts);
  const timeout = resolveInteractionTimeoutMs(opts.timeoutMs);
  for (const field of opts.fields) {
    const ref = field.ref.trim();
    const type = (field.type || DEFAULT_FILL_FIELD_TYPE).trim() || DEFAULT_FILL_FIELD_TYPE;
    const rawValue = field.value;
    const value =
      typeof rawValue === "string"
        ? rawValue
        : typeof rawValue === "number" || typeof rawValue === "boolean"
          ? String(rawValue)
          : "";
    if (!ref) {
      continue;
    }
    const locator = refLocator(page, ref);
    if (type === "checkbox" || type === "radio") {
      const checked =
        rawValue === true || rawValue === 1 || rawValue === "1" || rawValue === "true";
      try {
        await locator.setChecked(checked, { timeout });
      } catch (err) {
        throw toAIFriendlyError(err, ref);
      }
      continue;
    }
    try {
      await locator.fill(value, { timeout });
    } catch (err) {
      throw toAIFriendlyError(err, ref);
    }
  }
}

export async function scrollIntoViewViaPlaywright(opts: {
  cdpUrl: string;
  targetId?: string;
  ref?: string;
  selector?: string;
  timeoutMs?: number;
}): Promise<void> {
  const resolved = requireRefOrSelector(opts.ref, opts.selector);
  const page = await getRestoredPageForTarget(opts);
  const timeout = normalizeTimeoutMs(opts.timeoutMs, 20_000);

  const label = resolved.ref ?? resolved.selector!;
  const locator = resolved.ref
    ? refLocator(page, requireRef(resolved.ref))
    : page.locator(resolved.selector!);
  try {
    await locator.scrollIntoViewIfNeeded({ timeout });
  } catch (err) {
    throw toAIFriendlyError(err, label);
  }
}

export async function waitForViaPlaywright(opts: {
  cdpUrl: string;
  targetId?: string;
  timeMs?: number;
  text?: string;
  textGone?: string;
  selector?: string;
  url?: string;
  loadState?: "load" | "domcontentloaded" | "networkidle";
  fn?: string;
  timeoutMs?: number;
}): Promise<void> {
  const page = await getPageForTargetId(opts);
  ensurePageState(page);
  const timeout = normalizeTimeoutMs(opts.timeoutMs, 20_000);

  if (typeof opts.timeMs === "number" && Number.isFinite(opts.timeMs)) {
    await page.waitForTimeout(resolveBoundedDelayMs(opts.timeMs, "wait timeMs", MAX_WAIT_TIME_MS));
  }
  if (opts.text) {
    await page.getByText(opts.text).first().waitFor({
      state: "visible",
      timeout,
    });
  }
  if (opts.textGone) {
    await page.getByText(opts.textGone).first().waitFor({
      state: "hidden",
      timeout,
    });
  }
  if (opts.selector) {
    const selector = String(opts.selector).trim();
    if (selector) {
      await page.locator(selector).first().waitFor({ state: "visible", timeout });
    }
  }
  if (opts.url) {
    const url = String(opts.url).trim();
    if (url) {
      await page.waitForURL(url, { timeout });
    }
  }
  if (opts.loadState) {
    await page.waitForLoadState(opts.loadState, { timeout });
  }
  if (opts.fn) {
    const fn = String(opts.fn).trim();
    if (fn) {
      await page.waitForFunction(fn, { timeout });
    }
  }
}
