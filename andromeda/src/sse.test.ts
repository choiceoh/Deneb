import { describe, expect, it } from "vitest";
import { readSSE, type SSEFrame } from "./sse";

function stream(chunks: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  return new ReadableStream({
    start(controller) {
      for (const c of chunks) controller.enqueue(encoder.encode(c));
      controller.close();
    },
  });
}

describe("readSSE", () => {
  it("parses event/data frames", async () => {
    const frames: SSEFrame[] = [];
    await readSSE(stream(['event: delta\ndata: {"delta":"hi"}\n', 'data: {"x":1}\n']), (f) => frames.push(f));
    expect(frames).toEqual([
      { event: "delta", data: '{"delta":"hi"}' },
      { event: "", data: '{"x":1}' }, // event name applies to a single frame only
    ]);
  });

  it("reassembles frames split across chunks", async () => {
    const frames: SSEFrame[] = [];
    await readSSE(stream(["event: don", 'e\ndata: {"text":"', 'ok"}\n']), (f) => frames.push(f));
    expect(frames).toEqual([{ event: "done", data: '{"text":"ok"}' }]);
  });

  it("ignores blank data lines", async () => {
    const frames: SSEFrame[] = [];
    await readSSE(stream(['data:\ndata: {"a":1}\n']), (f) => frames.push(f));
    expect(frames).toEqual([{ event: "", data: '{"a":1}' }]);
  });
});
