import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const { fallbackRequireMock, readLoggingConfigMock } = vi.hoisted(() => ({
  readLoggingConfigMock: vi.fn(() => undefined),
  fallbackRequireMock: vi.fn(() => {
    throw new Error("config fallback should not be used in this test");
  }),
}));

vi.mock("./config.js", () => ({
  readLoggingConfig: readLoggingConfigMock,
}));

vi.mock("./node-require.js", () => ({
  resolveNodeRequireFromMeta: () => fallbackRequireMock,
}));

let originalTestFileLog: string | undefined;
let originalDenebLogLevel: string | undefined;
let logging: typeof import("../logging.js");

beforeAll(async () => {
  logging = await import("../logging.js");
});

beforeEach(() => {
  originalTestFileLog = process.env.DENEB_TEST_FILE_LOG;
  originalDenebLogLevel = process.env.DENEB_LOG_LEVEL;
  delete process.env.DENEB_TEST_FILE_LOG;
  delete process.env.DENEB_LOG_LEVEL;
  readLoggingConfigMock.mockClear();
  fallbackRequireMock.mockClear();
  logging.resetLogger();
  logging.setLoggerOverride(null);
});

afterEach(() => {
  if (originalTestFileLog === undefined) {
    delete process.env.DENEB_TEST_FILE_LOG;
  } else {
    process.env.DENEB_TEST_FILE_LOG = originalTestFileLog;
  }
  if (originalDenebLogLevel === undefined) {
    delete process.env.DENEB_LOG_LEVEL;
  } else {
    process.env.DENEB_LOG_LEVEL = originalDenebLogLevel;
  }
  logging.resetLogger();
  logging.setLoggerOverride(null);
  vi.restoreAllMocks();
});

describe("getResolvedLoggerSettings", () => {
  it("uses a silent fast path in default Vitest mode without config reads", () => {
    const settings = logging.getResolvedLoggerSettings();
    expect(settings.level).toBe("silent");
    expect(readLoggingConfigMock).not.toHaveBeenCalled();
    expect(fallbackRequireMock).not.toHaveBeenCalled();
  });

  it("reads logging config when test file logging is explicitly enabled", () => {
    process.env.DENEB_TEST_FILE_LOG = "1";
    const settings = logging.getResolvedLoggerSettings();
    expect(settings.level).toBe("info");
  });
});
