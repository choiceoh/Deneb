import { afterEach, describe, expect, it, vi } from "vitest";
import { subscribeEvents, type ProactiveEvent } from "./events";

function streamResponse(chunks: string[]): Response {
  const encoder = new TextEncoder();
  const stream = new ReadableStream({
    start(controller) {
      for (const c of chunks) controller.enqueue(encoder.encode(c));
      controller.close();
    },
  });
  return new Response(stream, { status: 200 });
}

afterEach(() => vi.unstubAllGlobals());

describe("subscribeEvents", () => {
  it("parses SSE frames into proactive events", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        streamResponse([
          'event: nudge\ndata: {"id":"e1","title":"회의 10분 전","body":"기획 리뷰"}\n',
          'data: {"id":"e2","message":"새 메일 3건"}\n',
        ]),
      ),
    );
    const got: ProactiveEvent[] = [];
    let opened = false;
    await subscribeEvents(
      { url: "http://x", token: "t" },
      { onOpen: () => (opened = true), onEvent: (ev) => got.push(ev) },
    );
    expect(opened).toBe(true);
    expect(got).toHaveLength(2);
    expect(got[0]).toMatchObject({ id: "e1", kind: "nudge", title: "회의 10분 전", body: "기획 리뷰" });
    expect(got[1]).toMatchObject({ id: "e2", body: "새 메일 3건" });
  });

  it("throws on a non-ok response", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("nope", { status: 500 })),
    );
    await expect(subscribeEvents({ url: "http://x", token: "t" }, {})).rejects.toThrow(/HTTP 500/);
  });
});
