import { afterEach, describe, expect, it } from "vitest";
import { createLogger, setLogLevel, setLogSink, type LogLevel } from "./log";

type Entry = { level: string; namespace: string; args: unknown[] };

function capture() {
  const entries: Entry[] = [];
  setLogSink((level, namespace, args) => entries.push({ level, namespace, args }));
  return entries;
}

afterEach(() => {
  setLogSink(); // reset to console
  setLogLevel("info");
});

describe("log", () => {
  it("namespaces output and nests via child()", () => {
    const entries = capture();
    setLogLevel("debug");
    const gw = createLogger("andromeda").child("gateway");
    gw.info("hello");
    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({ level: "info", namespace: "andromeda:gateway", args: ["hello"] });
  });

  it("filters messages below the configured level", () => {
    const entries = capture();
    setLogLevel("warn");
    const lg = createLogger("x");
    lg.debug("d");
    lg.info("i");
    lg.warn("w");
    lg.error("e");
    expect(entries.map((e) => e.level)).toEqual(["warn", "error"]);
  });

  it("silences everything at level silent", () => {
    const entries = capture();
    setLogLevel("silent" as LogLevel);
    createLogger("x").error("nope");
    expect(entries).toHaveLength(0);
  });
});
