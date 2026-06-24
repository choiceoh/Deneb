// Structured, namespaced logging — the observability backbone.
//
// Every module gets its own logger via createLogger("gateway"), so output is
// greppable by tag ([andromeda:gateway:rpc]) and filterable by level. An agent
// (or human) chasing a bug can raise the level at runtime without a rebuild:
//   localStorage.setItem("andromeda.logLevel", "debug")   // then reload
// or set VITE_LOG_LEVEL at build time. Tests can swap the sink via setLogSink().

export type LogLevel = "debug" | "info" | "warn" | "error" | "silent";

const ORDER: Record<LogLevel, number> = { debug: 10, info: 20, warn: 30, error: 40, silent: 100 };

export type LogSink = (level: Exclude<LogLevel, "silent">, namespace: string, args: unknown[]) => void;

const consoleSink: LogSink = (level, namespace, args) => {
  const fn =
    level === "debug"
      ? console.debug
      : level === "info"
        ? console.info
        : level === "warn"
          ? console.warn
          : console.error;
  fn(`[${namespace}]`, ...args);
};

function initialLevel(): LogLevel {
  try {
    const ls = typeof localStorage !== "undefined" ? localStorage.getItem("andromeda.logLevel") : null;
    if (ls && ls in ORDER) return ls as LogLevel;
  } catch {
    /* localStorage unavailable */
  }
  const fromEnv = import.meta.env.VITE_LOG_LEVEL;
  if (fromEnv && fromEnv in ORDER) return fromEnv as LogLevel;
  return import.meta.env.DEV ? "debug" : "info";
}

let minLevel: LogLevel = initialLevel();
let sink: LogSink = consoleSink;

export function setLogLevel(level: LogLevel): void {
  minLevel = level;
}
export function getLogLevel(): LogLevel {
  return minLevel;
}
// Redirect output (tests, an in-app log panel, telemetry). Pass nothing to reset.
export function setLogSink(next: LogSink = consoleSink): void {
  sink = next;
}

function emit(level: Exclude<LogLevel, "silent">, namespace: string, args: unknown[]): void {
  if (ORDER[level] >= ORDER[minLevel]) sink(level, namespace, args);
}

export interface Logger {
  debug(...args: unknown[]): void;
  info(...args: unknown[]): void;
  warn(...args: unknown[]): void;
  error(...args: unknown[]): void;
  child(sub: string): Logger;
}

export function createLogger(namespace: string): Logger {
  return {
    debug: (...args) => emit("debug", namespace, args),
    info: (...args) => emit("info", namespace, args),
    warn: (...args) => emit("warn", namespace, args),
    error: (...args) => emit("error", namespace, args),
    child: (sub) => createLogger(`${namespace}:${sub}`),
  };
}

// Root logger; modules call log.child("<module>").
export const log = createLogger("andromeda");
