import { AcpRuntimeError, type AcpRuntimeErrorCode } from "../acp/runtime/errors.js";
/**
 * Standardized test data builders.
 *
 * Each builder provides sensible defaults and accepts partial overrides.
 * Use these instead of inline object literals to keep tests concise and consistent.
 */
import { FailoverError } from "../agents/failover-error.js";
import type { FailoverReason } from "../agents/pi-embedded-helpers/types.js";
import type { DenebConfig } from "../config/types.deneb.js";

// -- Config builders --

/** Build a minimal DenebConfig with deep-merged overrides. */
export function buildTestConfig(overrides: Partial<DenebConfig> = {}): DenebConfig {
  return {
    ...overrides,
  };
}

/** Build a Telegram account config block. */
export function buildTelegramAccount(
  overrides: Partial<{
    botToken: string;
    allowList: string[];
    enabled: boolean;
  }> = {},
): { botToken: string; allowList?: string[]; enabled?: boolean } {
  return {
    botToken: "test-bot-token-000000000:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
    ...overrides,
  };
}

// -- Session builders --

/** Build a minimal test session descriptor. */
export function buildTestSession(
  overrides: Partial<{
    agentId: string;
    provider: string;
    model: string;
    sessionKey: string;
  }> = {},
): { agentId: string; provider: string; model: string; sessionKey: string } {
  return {
    agentId: "test-agent",
    provider: "openai",
    model: "gpt-4",
    sessionKey: "test-session-key",
    ...overrides,
  };
}

// -- Error builders --

/** Build a FailoverError with sensible defaults. */
export function buildFailoverError(
  overrides: Partial<{
    message: string;
    reason: FailoverReason;
    provider: string;
    model: string;
    profileId: string;
    status: number;
    code: string;
    cause: unknown;
  }> = {},
): FailoverError {
  const { message = "test failover error", reason = "unknown", ...rest } = overrides;
  return new FailoverError(message, { reason, ...rest });
}

/** Build an AcpRuntimeError with the given code and optional message/cause. */
export function buildAcpError(
  code: AcpRuntimeErrorCode = "ACP_TURN_FAILED",
  message = "test ACP error",
  cause?: unknown,
): AcpRuntimeError {
  return new AcpRuntimeError(code, message, cause !== undefined ? { cause } : undefined);
}
