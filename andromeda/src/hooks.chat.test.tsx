import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  chatStream: vi.fn(),
  callRpc: vi.fn(),
  invalidate: vi.fn(),
  clearCachedResource: vi.fn(),
}));

vi.mock("@/crud", () => ({ useInvalidate: () => mocks.invalidate }));
vi.mock("./gateway", () => ({
  chatStream: mocks.chatStream,
  callRpc: mocks.callRpc,
  ping: vi.fn(),
}));
vi.mock("./cachedList", () => ({ clearCachedResource: mocks.clearCachedResource }));

import type { ChatHandlers, ChatStreamOpts } from "./gateway";
import { useChat } from "./hooks";

const CFG = { url: "http://gateway", token: "token" };

type StreamImplementation = (
  cfg: typeof CFG,
  message: string,
  handlers: ChatHandlers,
  opts: ChatStreamOpts,
) => Promise<void>;

function streamWith(run: (handlers: ChatHandlers, opts: ChatStreamOpts) => void): StreamImplementation {
  return async (_cfg, _message, handlers, opts) => run(handlers, opts);
}

beforeEach(() => {
  mocks.chatStream.mockReset();
  mocks.callRpc.mockReset();
  mocks.invalidate.mockReset();
  mocks.clearCachedResource.mockReset();
});

afterEach(() => vi.restoreAllMocks());

describe("useChat send", () => {
  it("ignores blank input", async () => {
    const { result } = renderHook(() => useChat(CFG));
    await act(async () => result.current.send("   "));
    expect(mocks.chatStream).not.toHaveBeenCalled();
    expect(result.current.turns).toEqual([]);
  });

  it("streams ordered text and tool parts into a completed answer", async () => {
    mocks.chatStream.mockImplementation(
      streamWith((handlers) => {
        handlers.onThinking?.("reasoning preview");
        handlers.onDelta?.("before ");
        handlers.onTool?.({ state: "started", tool: "calendar", toolUseId: "tool-1", detail: "lookup" });
        handlers.onDelta?.("after");
        handlers.onTool?.({ state: "completed", tool: "calendar", toolUseId: "tool-1", detail: "done" });
        handlers.onDone?.({ text: "fallback", model: "model-a" });
      }),
    );
    const { result } = renderHook(() => useChat(CFG));

    await act(async () =>
      result.current.send("  summarize  ", {
        activeResource: "wiki",
        workspaceContext: "page context",
        model: "model-a",
        sessionKey: "client:project",
      }),
    );

    expect(mocks.chatStream).toHaveBeenCalledWith(
      CFG,
      "summarize",
      expect.any(Object),
      expect.objectContaining({
        workspaceContext: "page context",
        model: "model-a",
        sessionKey: "client:project",
        signal: expect.any(AbortSignal),
      }),
    );
    expect(result.current.busy).toBe(false);
    expect(result.current.stoppable).toBe(false);
    expect(result.current.thinking).toBe("");
    expect(result.current.turns).toHaveLength(2);
    expect(result.current.turns[0]).toMatchObject({ role: "user", text: "summarize", status: "done" });
    expect(result.current.turns[1]).toMatchObject({
      role: "assistant",
      text: "before after",
      status: "done",
      model: "model-a",
      canRegenerate: true,
      parts: [
        { kind: "text", text: "before " },
        { kind: "tool", id: "tool-1", tool: "calendar", state: "completed", detail: "done" },
        { kind: "text", text: "after" },
      ],
    });
    for (const resource of ["wiki", "search", "notebook", "calendar", "calendar-range"]) {
      expect(mocks.clearCachedResource).toHaveBeenCalledWith(resource);
      expect(mocks.invalidate).toHaveBeenCalledWith({ resource, invalidates: ["list"] });
    }
  });

  it("uses the final text when a stream produced no delta", async () => {
    mocks.chatStream.mockImplementation(streamWith((handlers) => handlers.onDone?.({ text: "complete answer" })));
    const { result } = renderHook(() => useChat(CFG));
    await act(async () => result.current.send("question", { activeResource: "todo" }));

    expect(result.current.turns[1]).toMatchObject({
      text: "complete answer",
      status: "done",
      parts: [{ kind: "text", text: "complete answer" }],
    });
    expect(mocks.clearCachedResource).not.toHaveBeenCalled();
    expect(mocks.invalidate).toHaveBeenCalledWith({ resource: "todo", invalidates: ["list"] });
  });

  it("records a streamed error without the finally block overwriting it", async () => {
    mocks.chatStream.mockImplementation(
      streamWith((handlers) => {
        handlers.onDelta?.("partial");
        handlers.onError?.("backend failed");
      }),
    );
    const { result } = renderHook(() => useChat(CFG));
    await act(async () => result.current.send("question"));

    expect(result.current.turns[1]).toMatchObject({
      text: "partial\n[오류] backend failed",
      status: "error",
    });
    expect(result.current.turns[1].parts?.at(-1)).toEqual({ kind: "text", text: "\n[오류] backend failed" });
  });

  it("converts a thrown transport failure into an error turn", async () => {
    mocks.chatStream.mockRejectedValue(new Error("network down"));
    const { result } = renderHook(() => useChat(CFG));
    await act(async () => result.current.send("question"));

    expect(result.current.turns[1]).toMatchObject({
      text: "[오류] network down",
      status: "error",
      parts: [{ kind: "text", text: "[오류] network down" }],
    });
  });

  it("aborts an in-flight stream and marks the answer stopped", async () => {
    mocks.chatStream.mockImplementation(
      (_cfg: typeof CFG, _message: string, _handlers: ChatHandlers, opts: ChatStreamOpts) =>
        new Promise<void>((_resolve, reject) => {
          opts.signal?.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")), {
            once: true,
          });
        }),
    );
    const { result } = renderHook(() => useChat(CFG));
    let sending!: Promise<void>;
    act(() => {
      sending = result.current.send("long task");
    });
    await waitFor(() => expect(result.current.stoppable).toBe(true));

    act(() => result.current.stop());
    await act(async () => sending);

    expect(result.current.busy).toBe(false);
    expect(result.current.turns[1].status).toBe("stopped");
  });

  it("when regenerates by replacing the trailing user and assistant pair", async () => {
    let answer = 0;
    mocks.chatStream.mockImplementation(streamWith((handlers) => handlers.onDone?.({ text: `answer-${++answer}` })));
    const { result } = renderHook(() => useChat(CFG));
    await act(async () => result.current.send("question", { model: "model-a" }));
    expect(result.current.turns[1].text).toBe("answer-1");

    act(() => result.current.regenerate());
    await waitFor(() => expect(mocks.chatStream).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(result.current.busy).toBe(false));

    expect(result.current.turns).toHaveLength(2);
    expect(result.current.turns[0].text).toBe("question");
    expect(result.current.turns[1]).toMatchObject({ text: "answer-2", model: "model-a" });
  });

  it("clears an idle conversation and forgets regeneration state", async () => {
    mocks.chatStream.mockImplementation(streamWith((handlers) => handlers.onDone?.({ text: "answer" })));
    const { result } = renderHook(() => useChat(CFG));
    await act(async () => result.current.send("question"));
    act(() => result.current.clear());

    expect(result.current.turns).toEqual([]);
    act(() => result.current.regenerate());
    expect(mocks.chatStream).toHaveBeenCalledTimes(1);
  });
});

describe("useChat capture", () => {
  const captureWireCases = [
    {
      name: "photo.png",
      mimeType: "image/png",
      expectedKind: "image",
      expectedParams: { image: "base64", mimeType: "image/png", sessionKey: "client:one", caption: "inspect" },
      expectedLabel: "이미지 첨부: photo.png\ninspect",
      expectedCaption: "inspect",
    },
    {
      name: "voice.m4a",
      mimeType: "audio/mp4",
      expectedKind: "audio",
      expectedParams: { audio: "base64", mimeType: "audio/mp4", sessionKey: "client:one" },
      expectedLabel: "녹음 첨부: voice.m4a",
      expectedCaption: undefined,
    },
    {
      name: "report.pdf",
      mimeType: "application/pdf",
      expectedKind: "document",
      expectedParams: {
        document: "base64",
        mimeType: "application/pdf",
        sessionKey: "client:one",
        filename: "report.pdf",
        caption: "inspect",
      },
      expectedLabel: "문서 첨부: report.pdf\ninspect",
      expectedCaption: "inspect",
    },
  ];

  it.each(captureWireCases)("when capturing $expectedKind attachments with the correct wire shape", async (tt) => {
    mocks.callRpc.mockResolvedValue({ text: " extracted text " });
    const { result, unmount } = renderHook(() => useChat(CFG));

    await act(async () =>
      result.current.capture(
        { name: tt.name, mimeType: tt.mimeType, base64: "base64" },
        { sessionKey: "client:one", caption: " inspect ", model: "model-a" },
      ),
    );

    expect(mocks.callRpc).toHaveBeenCalledWith(CFG, `miniapp.capture.${tt.expectedKind}`, tt.expectedParams);
    expect(result.current.turns[0]).toMatchObject({ role: "user", text: tt.expectedLabel });
    expect(result.current.turns[1]).toMatchObject({
      role: "assistant",
      text: "extracted text",
      status: "done",
      model: "model-a",
      canRegenerate: false,
      parts: [
        expect.objectContaining({
          kind: "attachment",
          filename: tt.name,
          mimeType: tt.mimeType,
          captureKind: tt.expectedKind,
          caption: tt.expectedCaption,
          text: "extracted text",
          state: "completed",
        }),
      ],
    });
    for (const resource of ["workfeed", "wiki", "search"]) {
      expect(mocks.clearCachedResource).toHaveBeenCalledWith(resource);
    }
    expect(mocks.invalidate).toHaveBeenCalledWith({ resource: "workfeed", invalidates: ["list"] });
    unmount();
    mocks.callRpc.mockReset();
    mocks.clearCachedResource.mockReset();
    mocks.invalidate.mockReset();
  });

  it("uses a safe MIME default and fallback text", async () => {
    mocks.callRpc.mockResolvedValue({});
    const { result } = renderHook(() => useChat(CFG));
    await act(async () => result.current.capture({ name: "unknown.bin", mimeType: "", base64: "data" }));

    expect(mocks.callRpc).toHaveBeenCalledWith(CFG, "miniapp.capture.document", {
      document: "data",
      mimeType: "application/octet-stream",
      sessionKey: undefined,
      filename: "unknown.bin",
    });
    expect(result.current.turns[1].text).toBe("첨부에서 내용을 추출하지 못했거나 분석에 실패했습니다.");
  });

  it("renders capture failures as typed attachment errors", async () => {
    mocks.callRpc.mockRejectedValue(new Error("extract failed"));
    const { result } = renderHook(() => useChat(CFG));
    await act(async () => result.current.capture({ name: "broken.pdf", mimeType: "application/pdf", base64: "data" }));

    expect(result.current.turns[1]).toMatchObject({
      text: "[오류] extract failed",
      status: "error",
      parts: [
        expect.objectContaining({
          kind: "attachment",
          filename: "broken.pdf",
          captureKind: "document",
          state: "error",
          isError: true,
        }),
      ],
    });
  });

  it("ignores an empty attachment payload", async () => {
    const { result } = renderHook(() => useChat(CFG));
    await act(async () => result.current.capture({ name: "empty.pdf", mimeType: "application/pdf", base64: "" }));
    expect(mocks.callRpc).not.toHaveBeenCalled();
    expect(result.current.turns).toEqual([]);
  });
});

describe("useChat variants & editResend", () => {
  function answerWith(text: string) {
    return streamWith((handlers) => {
      handlers.onDelta?.(text);
      handlers.onDone?.({ text });
    });
  }

  it("stashes the replaced answer on regenerate and pages back with ‹/›", async () => {
    mocks.chatStream.mockImplementation(answerWith("첫 번째 답"));
    const { result } = renderHook(() => useChat(CFG));
    await act(async () => result.current.send("질문"));
    expect(result.current.variants).toBeNull();

    mocks.chatStream.mockImplementation(answerWith("두 번째 답"));
    await act(async () => result.current.regenerate());
    await waitFor(() => expect(result.current.variants).toEqual({ index: 1, count: 2 }));
    expect(result.current.turns.at(-1)?.text).toBe("두 번째 답");

    act(() => result.current.selectVariant(-1));
    expect(result.current.turns.at(-1)?.text).toBe("첫 번째 답");
    expect(result.current.variants).toEqual({ index: 0, count: 2 });

    act(() => result.current.selectVariant(1));
    expect(result.current.turns.at(-1)?.text).toBe("두 번째 답");
  });

  it("clears the variant pager when a fresh user message is sent", async () => {
    mocks.chatStream.mockImplementation(answerWith("A"));
    const { result } = renderHook(() => useChat(CFG));
    await act(async () => result.current.send("질문"));
    mocks.chatStream.mockImplementation(answerWith("B"));
    await act(async () => result.current.regenerate());
    await waitFor(() => expect(result.current.variants).toEqual({ index: 1, count: 2 }));

    mocks.chatStream.mockImplementation(answerWith("새 답"));
    await act(async () => result.current.send("완전히 새 질문"));
    expect(result.current.variants).toBeNull();
  });

  it("editResend replaces the trailing exchange with the edited message", async () => {
    mocks.chatStream.mockImplementation(answerWith("원래 답"));
    const { result } = renderHook(() => useChat(CFG));
    await act(async () => result.current.send("원래 질문"));

    mocks.chatStream.mockImplementation(answerWith("고친 답"));
    await act(async () => result.current.editResend("고친 질문"));

    const userTurns = result.current.turns.filter((t) => t.role === "user");
    expect(userTurns).toHaveLength(1);
    expect(userTurns[0]?.text).toBe("고친 질문");
    expect(result.current.turns.at(-1)?.text).toBe("고친 답");
    expect(result.current.variants).toBeNull();
  });
});
