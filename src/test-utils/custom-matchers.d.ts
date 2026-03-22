import "vitest";
import type { FailoverReason } from "../agents/pi-embedded-helpers/types.js";

type ErrorConstructor = new (...args: never[]) => Error;

interface DenebCustomMatchers<R = unknown> {
  /**
   * Assert that the received value (Error or throwing function) is an instance of
   * the given error class and optionally has a matching `code` property.
   *
   * @example
   * expect(() => fn()).toThrowDomainError(AcpRuntimeError, "ACP_BACKEND_UNAVAILABLE");
   * await expect(promise).rejects.toThrowDomainError(FailoverError, "rate_limit");
   */
  toThrowDomainError(ErrorClass: ErrorConstructor, expectedCode?: string): R;

  /**
   * Assert that the received value is a FailoverError with a specific reason.
   *
   * @example
   * expect(err).toThrowFailoverReason("rate_limit");
   * await expect(promise).rejects.toThrowFailoverReason("auth");
   */
  toThrowFailoverReason(expectedReason: FailoverReason): R;
}

declare module "vitest" {
  // eslint-disable-next-line @typescript-eslint/no-empty-interface
  interface Assertion<T = unknown> extends DenebCustomMatchers<T> {}
  // eslint-disable-next-line @typescript-eslint/no-empty-interface
  interface AsymmetricMatchersContaining extends DenebCustomMatchers {}
}
