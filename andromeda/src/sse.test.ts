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

  it("cancels an open reader when its signal aborts", async () => {
    let cancelled = false;
    const pending = new ReadableStream<Uint8Array>({
      cancel() {
        cancelled = true;
      },
    });
    const controller = new AbortController();

    const reading = readSSE(pending, () => {}, controller.signal);
    controller.abort();
    await reading;

    expect(cancelled).toBe(true);
    expect(pending.locked).toBe(false);
  });

  it("cancels and releases the reader when the signal is already aborted", async () => {
    let cancelled = false;
    const pending = new ReadableStream<Uint8Array>({
      cancel() {
        cancelled = true;
      },
    });
    const controller = new AbortController();
    controller.abort();

    await readSSE(pending, () => {}, controller.signal);

    expect(cancelled).toBe(true);
    expect(pending.locked).toBe(false);
  });
});
