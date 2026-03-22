/**
 * Contextual assertion helpers for better failure diagnostics.
 *
 * Use these when a group of assertions needs a shared failure context
 * or when testing error shapes without verbose try-catch blocks.
 */
import { expect } from "vitest";

/**
 * Run a block of assertions with a descriptive context label.
 * On failure the label is prepended to the error message, making it easier
 * to identify which logical assertion group failed in a multi-step test.
 *
 * @example
 * assertWithContext("should reject unauthorized access", () => {
 *   expect(result.status).toBe(403);
 *   expect(result.code).toBe("AUTH_DENIED");
 * });
 */
export function assertWithContext(context: string, fn: () => void): void {
  try {
    fn();
  } catch (err) {
    if (err instanceof Error) {
      err.message = `[${context}] ${err.message}`;
    }
    throw err;
  }
}

type ErrorShapeSpec = {
  [key: string]: string | number | boolean | RegExp | undefined;
};

/**
 * Assert that an error matches a set of expected properties in one call.
 * Replaces verbose try-catch patterns where multiple error properties are
 * checked individually.
 *
 * String/number/boolean values use strict equality. RegExp values test against
 * the stringified property.
 *
 * @example
 * expectErrorShape(error, {
 *   name: "AcpRuntimeError",
 *   code: "ACP_BACKEND_UNAVAILABLE",
 *   message: /backend.*unavailable/i,
 * });
 */
export function expectErrorShape(error: unknown, shape: ErrorShapeSpec): void {
  expect(error).toBeTruthy();
  const errObj = error as Record<string, unknown>;
  for (const [key, expected] of Object.entries(shape)) {
    if (expected === undefined) {
      continue;
    }
    const actual = errObj[key];
    if (expected instanceof RegExp) {
      expect(String(actual), `error.${key} should match ${expected}`).toMatch(expected);
    } else {
      expect(actual, `error.${key}`).toBe(expected);
    }
  }
}
