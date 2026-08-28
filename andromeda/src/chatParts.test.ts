import { describe, expect, it } from "vitest";

import { appendTextPart, upsertToolPart } from "./chatParts";
import type { ChatTurn } from "./hooks";

const base: ChatTurn = { id: "a", role: "assistant", text: "", parts: [], status: "streaming" };

describe("appendTextPart", () => {
  it("preserves merged text when appending consecutive parts", () => {
    const after = appendTextPart(appendTextPart(base, "Hello"), " world");
    expect(after.parts).toEqual([{ kind: "text", text: "Hello world" }]);
    expect(after.text).toBe("Hello world");
  });

  it("creates new text part when appending after tool chip", () => {
    const withTool = upsertToolPart(base, { state: "started", tool: "search", toolUseId: "t1" });
    const after = appendTextPart(withTool, "done");
    expect(after.parts?.length).toBe(2);
    expect(after.parts?.at(-1)).toEqual({ kind: "text", text: "done" });
  });
});

describe("upsertToolPart", () => {
  it("returns updated tool part when same id completes", () => {
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

  it("keeps the started hint when the completed frame carries none", () => {
    // The gateway sends the human hint (command/query/path) only on `started`;
    // `completed` carries the result. Overwriting with undefined left finished
    // chips reading as a bare tool name ("exec" with no command).
    const started = upsertToolPart(base, {
      state: "started",
      tool: "exec",
      toolUseId: "t2",
      detail: "ls andromeda/src/cygnus",
    });
    const completed = upsertToolPart(started, { state: "completed", tool: "exec", toolUseId: "t2" });
    expect(completed.parts?.[0]).toMatchObject({
      id: "t2",
      state: "completed",
      detail: "ls andromeda/src/cygnus",
    });
  });
});
