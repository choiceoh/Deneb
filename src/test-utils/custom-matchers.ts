import { expect } from "vitest";
import { FailoverError } from "../agents/failover-error.js";
import type { FailoverReason } from "../agents/pi-embedded-helpers/types.js";

type ErrorConstructor = new (...args: never[]) => Error;

function extractThrownError(received: unknown): Error {
  if (typeof received === "function") {
    try {
      received();
      throw new TypeError("Expected function to throw, but it did not");
    } catch (err) {
      if (
        err instanceof TypeError &&
        err.message === "Expected function to throw, but it did not"
      ) {
        throw err;
      }
      if (!(err instanceof Error)) {
        throw new TypeError(`Expected function to throw an Error, but it threw: ${String(err)}`, {
          cause: err,
        });
      }
      return err;
    }
  }
  if (received instanceof Error) {
    return received;
  }
  throw new TypeError(
    `Expected an Error instance or a function that throws, got ${typeof received}`,
  );
}

/**
 * Custom matcher: checks that the received value (Error or throwing function)
 * is an instance of the given error class and optionally has a matching `code` property.
 *
 * Usage:
 *   expect(() => fn()).toThrowDomainError(AcpRuntimeError, "ACP_BACKEND_UNAVAILABLE")
 *   expect(caughtError).toThrowDomainError(AcpRuntimeError)
 */
function toThrowDomainError(
  this: {
    isNot: boolean;
    utils: { printReceived: (v: unknown) => string; printExpected: (v: unknown) => string };
  },
  received: unknown,
  ErrorClass: ErrorConstructor,
  expectedCode?: string,
) {
  let error: Error;
  try {
    error = extractThrownError(received);
  } catch (extractionError) {
    return {
      pass: false,
      message: () => (extractionError as Error).message,
    };
  }

  const isInstance = error instanceof ErrorClass;
  const actualCode = (error as { code?: unknown }).code;
  const codeMatches = expectedCode === undefined || actualCode === expectedCode;

  const pass = isInstance && codeMatches;

  if (pass) {
    return {
      pass: true,
      message: () =>
        `Expected not to throw ${ErrorClass.name}${expectedCode ? ` with code ${this.utils.printExpected(expectedCode)}` : ""}, but it did`,
    };
  }

  if (!isInstance) {
    return {
      pass: false,
      message: () =>
        `Expected ${ErrorClass.name}${expectedCode ? ` with code ${this.utils.printExpected(expectedCode)}` : ""}, but got ${error.constructor.name}: ${this.utils.printReceived(error.message)}`,
    };
  }

  return {
    pass: false,
    message: () =>
      `Expected ${ErrorClass.name} with code ${this.utils.printExpected(expectedCode)}, but got code ${this.utils.printReceived(actualCode)}`,
  };
}

/**
 * Custom matcher: checks that the received value is a FailoverError with a specific reason.
 *
 * Usage:
 *   expect(err).toThrowFailoverReason("rate_limit")
 *   expect(() => fn()).toThrowFailoverReason("auth")
 */
function toThrowFailoverReason(
  this: {
    isNot: boolean;
    utils: { printReceived: (v: unknown) => string; printExpected: (v: unknown) => string };
  },
  received: unknown,
  expectedReason: FailoverReason,
) {
  let error: Error;
  try {
    error = extractThrownError(received);
  } catch (extractionError) {
    return {
      pass: false,
      message: () => (extractionError as Error).message,
    };
  }

  if (!(error instanceof FailoverError)) {
    return {
      pass: false,
      message: () =>
        `Expected FailoverError with reason ${this.utils.printExpected(expectedReason)}, but got ${error.constructor.name}: ${this.utils.printReceived(error.message)}`,
    };
  }

  const pass = error.reason === expectedReason;
  return {
    pass,
    message: () =>
      pass
        ? `Expected not to throw FailoverError with reason ${this.utils.printExpected(expectedReason)}, but it did`
        : `Expected FailoverError with reason ${this.utils.printExpected(expectedReason)}, but got reason ${this.utils.printReceived(error.reason)}`,
  };
}

export const customMatchers = {
  toThrowDomainError,
  toThrowFailoverReason,
};

/** Register all custom matchers with Vitest. Call once in test/setup.ts. */
export function installCustomMatchers(): void {
  expect.extend(customMatchers);
}
