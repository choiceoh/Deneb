import { describe, expect, it } from "vitest";

import { appendTextPart, upsertToolPart } from "./chatParts";
import type { ChatTurn } from "./hooks";

const base: ChatTurn = { id: "a", role: "assistant", text: "", parts: [], status: "streaming" };

describe("appendTextPart", () => {
  it("merges consecutive text into the trailing text part", () => {
    const after = appendTextPart(appendTextPart(base, "Hello"), " world");
    expect(after.parts).toEqual([{ kind: "text", text: "Hello world" }]);
    expect(after.text).toBe("Hello world");
  });

  it("opens a new text part after a tool chip", () => {
    const withTool = upsertToolPart(base, { state: "started", tool: "search", toolUseId: "t1" });
    const after = appendTextPart(withTool, "done");
    expect(after.parts?.length).toBe(2);
    expect(after.parts?.at(-1)).toEqual({ kind: "text", text: "done" });
  });
});

describe("upsertToolPart", () => {
  it("inserts on started, then flips the same id to its completed result", () => {
    const started = upsertToolPart(base, { state: "started", tool: "search", toolUseId: "t1" });
    expect(started.parts?.[0]).toMatchObject({ kind: "tool", id: "t1", tool: "search", state: "started" });

    const completed = upsertToolPart(started, {
      state: "completed",
      tool: "search",
      toolUseId: "t1",
      detail: "3 hits",
    });
    expect(completed.parts?.length).toBe(1);
    expect(completed.parts?.[0]).toMatchObject({ id: "t1", state: "completed", detail: "3 hits" });
  });
});
