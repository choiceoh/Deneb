import { afterEach, describe, expect, it, vi } from "vitest";
import {
  TOKEN_HEADER,
  acceptCalendarProposal,
  analyzeMail,
  askMail,
  base,
  callRpc,
  chatStream,
  deleteSession,
  fetchGatewayBlob,
  filesDownloadUrl,
  listCalendarProposals,
  listPrompts,
  mailAttachmentUrl,
  approvalAttachmentUrl,
  recentSessions,
  sessionTranscript,
  setModel,
  streamFetch,
  syncPull,
} from "./gateway";

const CFG = { url: "http://gateway.example:18789/", token: "secret token" };

function jsonResponse(payload: unknown, ok = true): Response {
  return new Response(JSON.stringify(ok ? { ok: true, payload } : { ok: false, error: payload }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function byteStream(chunks: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  return new ReadableStream({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
      controller.close();
    },
  });
}

function requestAt(mock: ReturnType<typeof vi.fn>, index = 0) {
  const [url, init] = mock.mock.calls[index] as unknown as [string, RequestInit];
  const body = init.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : undefined;
  return { url, init, body };
}

afterEach(() => vi.unstubAllGlobals());

describe("gateway URL helpers", () => {
  it("normalizes one trailing slash without changing an internal path", () => {
    expect(base("http://host:18789/")).toBe("http://host:18789");
    expect(base("http://host/root/")).toBe("http://host/root");
    expect(base("http://host/root")).toBe("http://host/root");
  });

  it("builds an encoded mail attachment URL with legacy fallbacks", () => {
    const url = new URL(
      mailAttachmentUrl(CFG, "message/한글", {
        id: "attachment + 1",
        name: "report #1.pdf",
      }),
    );
    expect(url.pathname).toBe("/api/v1/miniapp/gmail/attachment");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      messageId: "message/한글",
      attachmentId: "attachment + 1",
      filename: "report #1.pdf",
      mimeType: "application/octet-stream",
      clientToken: CFG.token,
    });
  });

  it("builds a groupware approval attachment download URL", () => {
    const url = new URL(
      approvalAttachmentUrl(CFG, "99178", "1", { filename: "영수증.pdf", mimeType: "application/pdf" }),
    );
    expect(url.pathname).toBe("/api/v1/miniapp/groupware/approval/attachment");
    expect(url.searchParams.get("docId")).toBe("99178");
    expect(url.searchParams.get("attachment")).toBe("1");
    expect(url.searchParams.get("filename")).toBe("영수증.pdf");
    expect(url.searchParams.get("clientToken")).toBe(CFG.token);
  });

  it("when prefers explicit attachment wire fields", () => {
    const url = new URL(
      mailAttachmentUrl(CFG, "m1", {
        id: "legacy-id",
        attachmentId: "wire-id",
        name: "legacy.txt",
        filename: "wire.csv",
        mimeType: "text/csv",
      }),
    );
    expect(url.searchParams.get("attachmentId")).toBe("wire-id");
    expect(url.searchParams.get("filename")).toBe("wire.csv");
    expect(url.searchParams.get("mimeType")).toBe("text/csv");
  });

  it("builds a file download URL without double slashes", () => {
    const url = new URL(filesDownloadUrl(CFG, "프로젝트/a b.md"));
    expect(url.pathname).toBe("/api/v1/files/download");
    expect(url.searchParams.get("path")).toBe("프로젝트/a b.md");
    expect(url.searchParams.get("clientToken")).toBe(CFG.token);
  });
});

describe("callRpc", () => {
  it("sends the authenticated envelope and returns its payload", async () => {
    const fetchMock = vi.fn(async () => jsonResponse({ value: 7 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(callRpc<{ value: number }>(CFG, "miniapp.sample", { query: "hello" })).resolves.toEqual({
      value: 7,
    });
    const req = requestAt(fetchMock);
    expect(req.url).toBe("http://gateway.example:18789/api/v1/miniapp/rpc");
    expect(req.init.method).toBe("POST");
    expect(req.init.headers).toEqual({ "Content-Type": "application/json", [TOKEN_HEADER]: CFG.token });
    expect(req.body?.method).toBe("miniapp.sample");
    expect(req.body?.params).toEqual({ query: "hello" });
    expect(req.body?.id).toEqual(expect.any(String));
  });

  it("uses an empty params object by default", async () => {
    const fetchMock = vi.fn(async () => jsonResponse({ ok: true }));
    vi.stubGlobal("fetch", fetchMock);
    await callRpc(CFG, "miniapp.empty");
    expect(requestAt(fetchMock).body?.params).toEqual({});
  });

  it("surfaces HTTP and gateway envelope failures", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response("unavailable", { status: 503 }))
      .mockResolvedValueOnce(jsonResponse({ code: "BAD", message: "bad request" }, false))
      .mockResolvedValueOnce(new Response(JSON.stringify({ ok: false }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(callRpc(CFG, "miniapp.http")).rejects.toThrow("RPC miniapp.http: HTTP 503");
    await expect(callRpc(CFG, "miniapp.bad")).rejects.toThrow("bad request");
    await expect(callRpc(CFG, "miniapp.unknown")).rejects.toThrow("RPC miniapp.unknown failed");
  });
});

describe("RPC convenience methods", () => {
  it("uses stable names, defaults, and empty-list fallbacks", async () => {
    const payloads: Record<string, unknown> = {
      "miniapp.prompts.list": {},
      "miniapp.sessions.recent": {},
      "miniapp.sessions.transcript": {},
      "miniapp.sessions.delete": { deleted: 1 },
      "miniapp.models.set": { ok: true, role: "main", current: "model-a" },
      "miniapp.sync.pull": { events: [], cursor: 10, latestSeq: 10, hasMore: false, count: 0 },
      "miniapp.calendar.proposals.list": {},
      "miniapp.calendar.proposals.accept": { ok: true },
      "miniapp.mail.analyze": { id: "mail-1", analysis: "done", cached: false },
      "miniapp.mail.ask": { answer: "answer" },
    };
    const seen: Array<{ method: string; params: unknown }> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init?: RequestInit) => {
        const request = JSON.parse(String(init?.body)) as { method: string; params: unknown };
        seen.push({ method: request.method, params: request.params });
        return jsonResponse(payloads[request.method]);
      }),
    );

    await expect(listPrompts(CFG)).resolves.toEqual([]);
    // A payload with no rows and no `total` (an older gateway) must page to a
    // stop rather than offering a next page into nothing.
    await expect(recentSessions(CFG)).resolves.toEqual({ sessions: [], total: 0 });
    await expect(sessionTranscript(CFG, "client:one")).resolves.toEqual({ messages: [], total: 0, turnRunning: false });
    await expect(deleteSession(CFG, "client:one")).resolves.toBe(true);
    await setModel(CFG, "model-a");
    await syncPull(CFG);
    await expect(listCalendarProposals(CFG)).resolves.toEqual([]);
    await acceptCalendarProposal(CFG, "proposal-1");
    await analyzeMail(CFG, "mail-1", true);
    await expect(askMail(CFG, "mail-1", "question")).resolves.toBe("answer");

    expect(seen).toEqual([
      { method: "miniapp.prompts.list", params: {} },
      { method: "miniapp.sessions.recent", params: { limit: 20 } },
      { method: "miniapp.sessions.transcript", params: { sessionKey: "client:one", limit: 60 } },
      { method: "miniapp.sessions.delete", params: { sessionKey: "client:one" } },
      { method: "miniapp.models.set", params: { id: "model-a", role: "main" } },
      { method: "miniapp.sync.pull", params: { cursor: 0, limit: 100 } },
      { method: "miniapp.calendar.proposals.list", params: {} },
      { method: "miniapp.calendar.proposals.accept", params: { id: "proposal-1" } },
      { method: "miniapp.mail.analyze", params: { id: "mail-1", force: true } },
      { method: "miniapp.mail.ask", params: { id: "mail-1", question: "question", history: [] } },
    ]);
  });
});

describe("binary and stream transport", () => {
  it("returns blobs and rejects failed downloads", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response("file bytes", { status: 200 }))
      .mockResolvedValueOnce(new Response("missing", { status: 404 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(fetchGatewayBlob("http://files/one").then((blob) => blob.text())).resolves.toBe("file bytes");
    await expect(fetchGatewayBlob("http://files/missing")).rejects.toThrow("HTTP 404");
  });

  it("merges auth with caller headers and returns the response body", async () => {
    const body = byteStream(["data: ok\n"]);
    const fetchMock = vi.fn(async () => new Response(body, { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(streamFetch(CFG, "events", { method: "POST", headers: { "X-Trace": "trace" } })).resolves.toBe(body);
    const req = requestAt(fetchMock);
    expect(req.url).toBe("http://gateway.example:18789/api/v1/miniapp/events");
    expect(req.init.headers).toEqual({ [TOKEN_HEADER]: CFG.token, "X-Trace": "trace" });
  });

  it("rejects stream HTTP failures and missing bodies", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response("down", { status: 502 }))
      .mockResolvedValueOnce({ ok: true, body: null } as Response);
    vi.stubGlobal("fetch", fetchMock);

    await expect(streamFetch(CFG, "events")).rejects.toThrow("events: HTTP 502");
    await expect(streamFetch(CFG, "events")).rejects.toThrow("events: empty response body");
  });
});

describe("chatStream", () => {
  it("when composes workspace context and dispatches every typed frame", async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(
          byteStream([
            'event: delta\ndata: {"delta":"hello"}\n',
            'event: tool\ndata: {"state":"started","tool":"search","toolUseId":"t1","detail":"query","isError":false}\n',
            'event: thinking\ndata: {"preview":"reasoning"}\n',
            'event: done\ndata: {"text":"final","model":"m1","fellBack":true}\n',
            'event: error\ndata: {"error":"late error"}\n',
          ]),
          { status: 200 },
        ),
    );
    vi.stubGlobal("fetch", fetchMock);
    const handlers = {
      onDelta: vi.fn(),
      onTool: vi.fn(),
      onThinking: vi.fn(),
      onDone: vi.fn(),
      onError: vi.fn(),
    };
    const controller = new AbortController();

    await chatStream(CFG, "summarize", handlers, {
      sessionKey: "client:project",
      workspaceContext: "  current document  ",
      model: "m1",
      signal: controller.signal,
    });

    expect(handlers.onDelta).toHaveBeenCalledWith("hello");
    expect(handlers.onTool).toHaveBeenCalledWith({
      state: "started",
      tool: "search",
      toolUseId: "t1",
      detail: "query",
      isError: false,
    });
    expect(handlers.onThinking).toHaveBeenCalledWith("reasoning");
    expect(handlers.onDone).toHaveBeenCalledWith({ text: "final", model: "m1", fellBack: true });
    expect(handlers.onError).toHaveBeenCalledWith("late error");
    expect(requestAt(fetchMock).init.signal).toBe(controller.signal);
    expect(requestAt(fetchMock).body).toEqual({
      message: "[작업 영역 — 현재 내용]\n  current document  \n\n[요청]\nsummarize",
      sessionKey: "client:project",
      model: "m1",
    });
  });

  it("uses defaults and ignores malformed or untyped frames", async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(
          byteStream([
            "event: delta\ndata: not-json\n",
            'event: tool\ndata: {"state":"started"}\n',
            'event: delta\ndata: {"delta":7}\n',
            "event: error\ndata: {}\n",
            "event: done\ndata: {}\n",
          ]),
          { status: 200 },
        ),
    );
    vi.stubGlobal("fetch", fetchMock);
    const handlers = { onDelta: vi.fn(), onTool: vi.fn(), onDone: vi.fn(), onError: vi.fn() };

    await chatStream(CFG, "hello", handlers, { workspaceContext: "   ", model: "" });

    expect(handlers.onDelta).not.toHaveBeenCalled();
    expect(handlers.onTool).not.toHaveBeenCalled();
    expect(handlers.onError).toHaveBeenCalledWith("unknown error");
    expect(handlers.onDone).toHaveBeenCalledWith({ text: "", model: undefined, fellBack: false });
    expect(requestAt(fetchMock).body).toEqual({ message: "hello", sessionKey: "client:main" });
  });

  it("treats a terminal error frame as terminal, not as a dropped stream", async () => {
    // The gateway ends a failed turn with `event: error` and nothing else
    // (nativeapi/chat_stream.go). Throwing here too would make hooks.ts render
    // the error AND then poll the transcript for an answer that never comes.
    const fetchMock = vi.fn(
      async () => new Response(byteStream(['event: error\ndata: {"error":"boom"}\n\n']), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const handlers = { onError: vi.fn(), onDone: vi.fn() };

    await expect(chatStream(CFG, "hello", handlers)).resolves.toBeUndefined();
    expect(handlers.onError).toHaveBeenCalledWith("boom");
    expect(handlers.onDone).not.toHaveBeenCalled();
  });

  it("rejects a clean SSE EOF before a terminal done frame", async () => {
    const fetchMock = vi.fn(
      async () => new Response(byteStream(['event: delta\ndata: {"delta":"partial"}\n']), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const handlers = { onDelta: vi.fn(), onDone: vi.fn() };

    await expect(chatStream(CFG, "hello", handlers)).rejects.toThrow("chat stream ended before terminal event");
    expect(handlers.onDelta).toHaveBeenCalledWith("partial");
    expect(handlers.onDone).not.toHaveBeenCalled();
  });
});
