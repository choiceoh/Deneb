import { redactSensitiveText } from "../logging/redact.js";

export function extractErrorCode(err: unknown): string | undefined {
  if (!err || typeof err !== "object") {
    return undefined;
  }
  const code = (err as { code?: unknown }).code;
  if (typeof code === "string") {
    return code;
  }
  if (typeof code === "number") {
    return String(code);
  }
  return undefined;
}

export function readErrorName(err: unknown): string {
  if (!err || typeof err !== "object") {
    return "";
  }
  const name = (err as { name?: unknown }).name;
  return typeof name === "string" ? name : "";
}

export function collectErrorGraphCandidates(
  err: unknown,
  resolveNested?: (current: Record<string, unknown>) => Iterable<unknown>,
): unknown[] {
  const queue: unknown[] = [err];
  const seen = new Set<unknown>();
  const candidates: unknown[] = [];

  while (queue.length > 0) {
    const current = queue.shift();
    if (current == null || seen.has(current)) {
      continue;
    }
    seen.add(current);
    candidates.push(current);

    if (!current || typeof current !== "object" || !resolveNested) {
      continue;
    }
    for (const nested of resolveNested(current as Record<string, unknown>)) {
      if (nested != null && !seen.has(nested)) {
        queue.push(nested);
      }
    }
  }

  return candidates;
}

/**
 * Type guard for NodeJS.ErrnoException (any error with a `code` property).
 */
export function isErrno(err: unknown): err is NodeJS.ErrnoException {
  return Boolean(err && typeof err === "object" && "code" in err);
}

/**
 * Check if an error has a specific errno code.
 */
export function hasErrnoCode(err: unknown, code: string): boolean {
  return isErrno(err) && err.code === code;
}

export function formatErrorMessage(err: unknown): string {
  let formatted: string;
  if (err instanceof Error) {
    formatted = err.message || err.name || "Error";
  } else if (typeof err === "string") {
    formatted = err;
  } else if (typeof err === "number" || typeof err === "boolean" || typeof err === "bigint") {
    formatted = String(err);
  } else {
    try {
      formatted = JSON.stringify(err);
    } catch {
      formatted = Object.prototype.toString.call(err);
    }
  }
  // Security: best-effort token redaction before returning/logging.
  return redactSensitiveText(formatted);
}

export function formatUncaughtError(err: unknown): string {
  if (extractErrorCode(err) === "INVALID_CONFIG") {
    return formatErrorMessage(err);
  }
  if (err instanceof Error) {
    const stack = err.stack ?? err.message ?? err.name;
    return redactSensitiveText(stack);
  }
  return formatErrorMessage(err);
}

/**
 * Error의 cause 체인을 "원인 → 원인의 원인 → ..." 형태로 포맷.
 * 에러 발생 원인의 원인까지 한눈에 파악할 수 있도록.
 */
export function formatErrorCauseChain(err: unknown, maxDepth = 10): string | undefined {
  if (!err || !(err instanceof Error) || !err.cause) {
    return undefined;
  }
  const causes: string[] = [];
  let current: unknown = err.cause;
  let depth = 0;
  while (current && depth < maxDepth) {
    depth++;
    if (current instanceof Error) {
      const code = extractErrorCode(current);
      const prefix = code ? `[${code}] ` : "";
      causes.push(`${prefix}${current.message}`);
      current = current.cause;
    } else if (typeof current === "string") {
      causes.push(current);
      break;
    } else {
      try {
        causes.push(JSON.stringify(current));
      } catch {
        causes.push(Object.prototype.toString.call(current));
      }
      break;
    }
  }
  return causes.length > 0 ? causes.join(" ← ") : undefined;
}
