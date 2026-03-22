/**
 * Centralized test utilities barrel.
 *
 * Import from here instead of reaching into individual files:
 *   import { captureEnv, withEnv, useFrozenTime, withTempDir } from "../test-utils/index.js";
 *
 * Categories:
 *   - Environment: captureEnv, captureFullEnv, withEnv, withEnvAsync
 *   - Time: useFrozenTime, useRealTime
 *   - Temp dirs: withTempDir, createFixtureSuite, createTrackedTempDirs, createTempHomeEnv
 *   - Mocking: createMockServerResponse, withFetchPreconnect, createModelAuthMockModule
 *   - Channel/plugin: createTestRegistry, makeDirectPlugin, createIMessageTestPlugin
 *   - Assertions: expectGeneratedTokenPersistedToGatewayAuth, exec assertions
 *   - Types: MockFn, TempHomeEnv, FetchMock
 */

// Environment helpers
export { captureEnv, captureFullEnv, withEnv, withEnvAsync } from "./env.js";

// Time
export { useFrozenTime, useRealTime } from "./frozen-time.js";

// Temp directories / fixtures
export { withTempDir } from "./temp-dir.js";
export { createFixtureSuite } from "./fixture-suite.js";
export { createTrackedTempDirs } from "./tracked-temp-dirs.js";
export { createTempHomeEnv, withTempHome } from "./temp-home.js";
export type { TempHomeEnv } from "./temp-home.js";
export { withTempSecretFiles } from "./secret-file-fixture.js";
export { createRebindableDirectoryAlias } from "./symlink-rebind-race.js";

// Mocking
export { createMockServerResponse } from "./mock-http-response.js";
export { withFetchPreconnect } from "./fetch-mock.js";
export type { FetchMock } from "./fetch-mock.js";
export { createModelAuthMockModule } from "./model-auth-mock.js";
export { runWithModelFallback } from "./model-fallback.mock.js";

// Types
export type { MockFn } from "./vitest-mock-fn.js";

// Channel / plugin testing
export { createTestRegistry } from "./channel-plugins.js";
export { makeDirectPlugin } from "./channel-plugin-test-fixtures.js";
export { createIMessageTestPlugin } from "./imessage-test-plugin.js";
export {
  createCapturedPluginRegistration,
  registerSingleProviderPlugin,
} from "./plugin-registration.js";

// Assertions
export { expectGeneratedTokenPersistedToGatewayAuth } from "./auth-token-assertions.js";

// Chunk helpers
export { countLines, hasBalancedFences } from "./chunk-test-helpers.js";

// Command / exec
export { runRegisteredCli } from "./command-runner.js";

// Hook event payloads
export { createInternalHookEventPayload } from "./internal-hook-event-payload.js";

// Port allocation
export { getDeterministicFreePortBlock, getFreePortBlockWithPermissionFallback } from "./ports.js";

// Typed cases helper
export { typedCases } from "./typed-cases.js";

// Test vectors
export { VALID_EXEC_SECRET_REF_IDS } from "./secret-ref-test-vectors.js";

// Provider usage fetch mocking
export { makeResponse as makeUsageResponse } from "./provider-usage-fetch.js";

// Camera URL helpers
export { stubFetchResponse } from "./camera-url-test-helpers.js";

// Runtime env mock (for extension/plugin testing)
export { createRuntimeEnv } from "./runtime-env-mock.js";

// Telegram plugin command mocks
export { pluginCommandMocks, resetPluginCommandMocks } from "./telegram-plugin-command-mock.js";

// Custom Vitest matchers (auto-registered in test/setup.ts; re-exported for manual use)
export { customMatchers, installCustomMatchers } from "./custom-matchers.js";

// Test data builders
export {
  buildTestConfig,
  buildTelegramAccount,
  buildTestSession,
  buildFailoverError,
  buildAcpError,
} from "./builders.js";

// Contextual assertion helpers
export { assertWithContext, expectErrorShape } from "./context-assertions.js";
