import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  type GatewayConfig,
  type SessionRow,
  deleteSession,
  focusSession,
  pinSession,
  recentSessions,
  sessionTranscript,
  setModel,
} from "@/gateway";
import type { ChatTurn } from "@/hooks";
import { useSessions } from "./useSessions";

vi.mock("@/gateway", () => ({
  TRANSCRIPT_MAX: 200,
  deleteSession: vi.fn(),
  focusSession: vi.fn(),
  pinSession: vi.fn(),
  recentSessions: vi.fn(),
  renameSession: vi.fn(),
  searchSessions: vi.fn(),
  sessionTranscript: vi.fn(),
  setModel: vi.fn(),
}));

const cfg: GatewayConfig = { url: "http://test", token: "token" };
const recent = vi.mocked(recentSessions);
// The RPC returns a page (rows + the size of the whole scoped set) so the
// drawer can page past the gateway's per-response cap.
const page = (sessions: SessionRow[], total = sessions.length, focus?: string) => ({
  sessions,
  total,
  ...(focus ? { focus } : {}),
});
const transcript = vi.mocked(sessionTranscript);
const remove = vi.mocked(deleteSession);
const focus = vi.mocked(focusSession);
const pin = vi.mocked(pinSession);
const persist = vi.mocked(setModel);

function chatDouble() {
  return { clear: vi.fn(), setTurns: vi.fn() };
}

beforeEach(() => {
  recent.mockResolvedValue(page([]));
  transcript.mockResolvedValue({ messages: [], total: 0, turnRunning: false });
  remove.mockResolvedValue(true);
  focus.mockResolvedValue("");
  pin.mockResolvedValue(true);
  persist.mockResolvedValue({ ok: true, role: "main", current: "" });
  localStorage.removeItem("andromeda.chat.lastSession");
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("useSessions", () => {
  it("returns namespace-filtered sessions when gateway lists recent", async () => {
    recent.mockResolvedValue(
      page([
        { key: "client:main:a", label: "A" },
        { key: "client:main:b", label: "B" },
        { key: "system:heartbeat", label: "system" },
      ]),
    );
    const chat = chatDouble();
    const { result } = renderHook(() => useSessions(cfg, true, false, chat, { filter: "client:main:" }));

    await waitFor(() => expect(result.current.sessions.map((s) => s.key)).toEqual(["client:main:a", "client:main:b"]));
    expect(recent).toHaveBeenCalledWith(cfg, 20, undefined, 0);
  });

  it("scopes the recent fetch to a channel server-side when given one", async () => {
    // Regression: the CHAT tab must ask the gateway for its OWN channel, else the
    // newest-N window is filled by autonomous sessions (heartbeat/cron/mail) and
    // the client-side filter leaves the drawer looking empty.
    recent.mockResolvedValue(page([{ key: "client:main", label: "업무" }]));
    const chat = chatDouble();
    renderHook(() => useSessions(cfg, true, false, chat, { channel: "client", filter: "client:" }));
    await waitFor(() => expect(recent).toHaveBeenCalledWith(cfg, 20, "client", 0));
  });

  it("refetches on window focus so a mid-restore partial list self-heals", async () => {
    // First fetch lands while the gateway is still restoring sessions in the
    // background (server_lifecycle.go) — only client:main is back yet.
    recent.mockResolvedValueOnce(page([{ key: "client:main", label: "업무" }]));
    const chat = chatDouble();
    const { result } = renderHook(() => useSessions(cfg, true, false, chat, { filter: "client:" }));
    await waitFor(() => expect(result.current.sessions.map((s) => s.key)).toEqual(["client:main"]));

    // Restore has since finished; a focus refresh must pick up the full list
    // rather than staying frozen on the mid-restore snapshot.
    recent.mockResolvedValue(
      page([
        { key: "client:main", label: "업무" },
        { key: "client:main:mrudefc16xpd", label: "근거 확인 및 판정 기록 완료" },
      ]),
    );
    await act(async () => {
      window.dispatchEvent(new Event("focus"));
    });
    await waitFor(() =>
      expect(result.current.sessions.map((s) => s.key)).toEqual(["client:main", "client:main:mrudefc16xpd"]),
    );
  });

  it("when maps a selected transcript into stable chat turns", async () => {
    transcript.mockResolvedValue({
      messages: [
        { id: "u1", role: "user", content: "질문" },
        { role: "assistant", content: "답변" },
        { id: "sys", role: "system", content: "상태" },
      ],
      total: 3,
      turnRunning: false,
    });
    const chat = chatDouble();
    const { result } = renderHook(() => useSessions(cfg, true, false, chat));

    await act(async () => result.current.selectSession("client:main:abc"));

    expect(result.current.sessionKey).toBe("client:main:abc");
    expect(chat.setTurns).toHaveBeenCalledWith([
      { id: "u1", role: "user", text: "질문", status: "done", canRegenerate: false },
      { id: "tr-client:main:abc-1", role: "assistant", text: "답변", status: "done", canRegenerate: false },
      { id: "sys", role: "assistant", text: "상태", status: "done", canRegenerate: false },
    ]);
    expect(result.current.sessionErr).toBe("");
  });

  it("rebuilds tool chips from the server toolTrace on restore", async () => {
    transcript.mockResolvedValue({
      messages: [
        { id: "u1", role: "user", content: "폴더 보여줘" },
        {
          role: "assistant",
          content: "",
          toolTrace: [{ tool: "exec", detail: "ls -la", summary: "total 60 · 2줄", preview: "total 60\nfile.ts" }],
        },
        { role: "assistant", content: "6개 파일이 있습니다." },
      ],
      total: 3,
      turnRunning: false,
    });
    const chat = chatDouble();
    const { result } = renderHook(() => useSessions(cfg, true, false, chat));

    await act(async () => result.current.selectSession("client:main:abc"));

    const turns = chat.setTurns.mock.calls.at(-1)?.[0] as ChatTurn[];
    const chipTurn = turns[1];
    expect(chipTurn.parts).toEqual([
      {
        kind: "tool",
        id: "tr-client:main:abc-1-t0",
        tool: "exec",
        state: "completed",
        detail: "ls -la",
        isError: undefined,
        resultSummary: "total 60 · 2줄",
        resultPreview: "total 60\nfile.ts",
      },
    ]);
    // The final answer row has no trace — plain body path (no parts).
    expect(turns[2].parts).toBeUndefined();
  });

  it("when deleting the active session removes it and mints a fresh conversation", async () => {
    recent.mockResolvedValue(
      page([
        { key: "client:main", label: "업무" },
        { key: "client:main:old", label: "Old" },
      ]),
    );
    const chat = chatDouble();
    const newKey = vi.fn(() => "client:main:new");
    const { result } = renderHook(() => useSessions(cfg, true, false, chat, { newKey }));
    await waitFor(() => expect(result.current.sessions).toHaveLength(2));

    await act(async () => result.current.removeSession("client:main"));

    expect(remove).toHaveBeenCalledWith(cfg, "client:main");
    expect(result.current.sessions.map((s) => s.key)).toEqual(["client:main:old"]);
    expect(result.current.sessionKey).toBe("client:main:new");
    expect(newKey).toHaveBeenCalledTimes(1);
    expect(chat.clear).toHaveBeenCalledTimes(1);
  });

  it("when blocks session mutation while a turn or attachment is busy", async () => {
    const chat = chatDouble();
    const newKey = vi.fn(() => "client:main:new");
    const { result } = renderHook(() => useSessions(cfg, true, true, chat, { newKey }));

    act(() => result.current.newChat());
    await act(async () => result.current.selectSession("client:main:other"));
    await act(async () => result.current.removeSession("client:main"));

    expect(result.current.sessionKey).toBe("client:main");
    expect(newKey).not.toHaveBeenCalled();
    expect(chat.clear).not.toHaveBeenCalled();
    expect(transcript).not.toHaveBeenCalled();
    expect(remove).not.toHaveBeenCalled();
  });

  it("keeps retrying (and says so) when the cold fetch fails", async () => {
    // The gateway was down or hot-swapping through the whole staggered window
    // (app launched during an auto-deploy). The 채팅 탭 has no drawer-open
    // refresh, so a silently-swallowed failure left the list empty forever.
    vi.useFakeTimers();
    try {
      recent.mockRejectedValue(new Error("HTTP 502"));
      const chat = chatDouble();
      const { result } = renderHook(() => useSessions(cfg, true, false, chat, { channel: "client" }));
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5000);
      });
      // An unreachable gateway must not read as "최근 대화가 없습니다."
      expect(result.current.sessionErr).not.toBe("");

      recent.mockResolvedValue(page([{ key: "client:main", label: "업무" }]));
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10000);
      });
      expect(result.current.sessions.map((s) => s.key)).toEqual(["client:main"]);
      expect(result.current.sessionErr).toBe("");
    } finally {
      vi.useRealTimers();
    }
  });

  it("pages past the gateway's per-response cap instead of stopping at it", async () => {
    // Conversations are no longer GC'd, so history grows past the 100-row cap of
    // one response. A limit-only drawer would silently hide everything beyond it.
    const rows = (from: number, n: number) =>
      Array.from({ length: n }, (_, i) => ({ key: `client:main:c${from + i}`, label: `대화 ${from + i}` }));
    // Faithful server: never returns more than `total - offset` rows.
    recent.mockImplementation(async (_cfg, limit = 20, _channel, offset = 0) =>
      page(rows(offset, Math.max(0, Math.min(limit, 130 - offset))), 130),
    );
    const chat = chatDouble();
    const { result } = renderHook(() => useSessions(cfg, true, false, chat, { channel: "client" }));
    await waitFor(() => expect(result.current.sessions).toHaveLength(20));
    expect(result.current.canLoadMoreSessions).toBe(true);

    // Six pages in, the window is wider than one response — the hook must
    // assemble it from offset fetches rather than truncating at the cap.
    for (let i = 0; i < 6; i++) await act(async () => result.current.loadMoreSessions());

    expect(result.current.sessions).toHaveLength(130);
    expect(result.current.sessions.at(-1)?.key).toBe("client:main:c129");
    // The window past the cap was assembled from a second, offset fetch.
    expect(recent.mock.calls.some((c) => c[3] === 100)).toBe(true);
    expect(result.current.canLoadMoreSessions).toBe(false);
  });

  it("clears stale session rows immediately on disconnect", async () => {
    recent.mockResolvedValue(page([{ key: "client:main", label: "업무" }]));
    const chat = chatDouble();
    const { result, rerender } = renderHook(({ connected }) => useSessions(cfg, connected, false, chat), {
      initialProps: { connected: true },
    });
    await waitFor(() => expect(result.current.sessions).toHaveLength(1));

    rerender({ connected: false });

    expect(result.current.sessions).toEqual([]);
  });

  it("restores the last chat session and loads its transcript", async () => {
    localStorage.setItem("andromeda.chat.lastSession", "client:main:restored");
    transcript.mockResolvedValue({
      messages: [{ id: "u1", role: "user", content: "이어서" }],
      total: 1,
      turnRunning: false,
    });
    const chat = chatDouble();
    const { result } = renderHook(() => useSessions(cfg, true, false, chat, { newKey: () => "client:main:new" }));

    expect(result.current.sessionKey).toBe("client:main:restored");
    await waitFor(() => expect(chat.setTurns).toHaveBeenCalled());
    expect(transcript).toHaveBeenCalledWith(cfg, "client:main:restored", undefined);
  });

  it("adopts gateway focus when the chat tab is still on home", async () => {
    recent.mockResolvedValue(page([{ key: "client:main", label: "업무" }], 1, "client:main:from-phone"));
    transcript.mockResolvedValue({
      messages: [{ id: "u1", role: "user", content: "폰" }],
      total: 1,
      turnRunning: false,
    });
    const chat = chatDouble();
    const { result } = renderHook(() => useSessions(cfg, true, false, chat, { newKey: () => "client:main:new" }));

    await waitFor(() => expect(result.current.sessionKey).toBe("client:main:from-phone"));
    expect(localStorage.getItem("andromeda.chat.lastSession")).toBe("client:main:from-phone");
  });

  it("writes focus and lastSession when switching on the chat tab", async () => {
    const chat = chatDouble();
    const { result } = renderHook(() => useSessions(cfg, true, false, chat, { newKey: () => "client:main:new" }));

    await act(async () => result.current.selectSession("client:main:abc"));

    expect(focus).toHaveBeenCalledWith(cfg, "client:main:abc");
    expect(localStorage.getItem("andromeda.chat.lastSession")).toBe("client:main:abc");
  });

  it("pins a conversation to the top of the drawer", async () => {
    recent.mockResolvedValue(
      page([
        { key: "client:main:a", label: "A" },
        { key: "client:main:b", label: "B" },
      ]),
    );
    const chat = chatDouble();
    const { result } = renderHook(() => useSessions(cfg, true, false, chat));
    await waitFor(() => expect(result.current.sessions).toHaveLength(2));

    await act(async () => result.current.pinConversation("client:main:b", true));

    expect(pin).toHaveBeenCalledWith(cfg, "client:main:b", true);
    expect(result.current.sessions.map((s) => s.key)).toEqual(["client:main:b", "client:main:a"]);
  });

  it("clears a conversation model override", async () => {
    recent.mockResolvedValue(page([{ key: "client:main:a", label: "A", model: "gpt" }]));
    const chat = chatDouble();
    const { result } = renderHook(() => useSessions(cfg, true, false, chat));
    await waitFor(() => expect(result.current.sessions[0]?.model).toBe("gpt"));

    await act(async () => result.current.resetConversationModel("client:main:a"));

    expect(persist).toHaveBeenCalledWith(cfg, "", "main", "client:main:a");
    expect(result.current.sessions[0]?.model).toBe("");
  });
});

// The last-conversation restore slot: each key-minting surface (채팅 탭 · Cygnus)
// gets its own localStorage slot, plus a namespace guard on whatever it reads.
// Regression: both surfaces sharing one slot made either boot into (and load
// the transcript of) the other's conversation.
describe("useSessions last-session slots", () => {
  const cygnusOpts = {
    mainKey: "client:cygnus:main",
    filter: "client:cygnus:",
    newKey: () => "client:cygnus:new",
    lastKeyStore: "cygnus.chat.lastSession",
  };
  afterEach(() => {
    localStorage.removeItem("cygnus.chat.lastSession");
  });

  it("restores from its own slot and ignores another surface's slot", () => {
    localStorage.setItem("andromeda.chat.lastSession", "client:main:work");
    localStorage.setItem("cygnus.chat.lastSession", "client:cygnus:abc");
    const { result } = renderHook(() => useSessions(cfg, false, false, chatDouble(), cygnusOpts));
    expect(result.current.sessionKey).toBe("client:cygnus:abc");

    // The 채팅 탭 (default slot) still restores its own conversation.
    const tab = renderHook(() =>
      useSessions(cfg, false, false, chatDouble(), {
        mainKey: "client:main",
        filter: "client:",
        newKey: () => "client:main:new",
      }),
    );
    expect(tab.result.current.sessionKey).toBe("client:main:work");
  });

  it("falls back to its own main when the restored key is outside the namespace", () => {
    localStorage.setItem("cygnus.chat.lastSession", "client:main:leaked");
    const { result } = renderHook(() => useSessions(cfg, false, false, chatDouble(), cygnusOpts));
    expect(result.current.sessionKey).toBe("client:cygnus:main");
  });

  it("persists a selected conversation to its own slot only", async () => {
    const { result } = renderHook(() => useSessions(cfg, true, false, chatDouble(), cygnusOpts));
    await act(async () => result.current.selectSession("client:cygnus:x"));
    expect(localStorage.getItem("cygnus.chat.lastSession")).toBe("client:cygnus:x");
    expect(localStorage.getItem("andromeda.chat.lastSession")).toBeNull();
  });

  it("ignores a gateway focus session from another surface's namespace", async () => {
    // The phone/채팅 탭 just touched client:main:hot — the gateway reports it as
    // the focused session. Cygnus must NOT adopt it at boot.
    recent.mockResolvedValue(page([{ key: "client:cygnus:a", label: "A" }], 1, "client:main:hot"));
    const { result } = renderHook(() =>
      useSessions(cfg, true, false, chatDouble(), { ...cygnusOpts, followPrefix: "client:cygnus:" }),
    );
    await waitFor(() => expect(result.current.sessions.length).toBe(1));
    expect(result.current.sessionKey).toBe("client:cygnus:main");
  });

  it("follows a gateway focus session inside its own namespace", async () => {
    recent.mockResolvedValue(page([{ key: "client:cygnus:hot", label: "핫" }], 1, "client:cygnus:hot"));
    const { result } = renderHook(() =>
      useSessions(cfg, true, false, chatDouble(), { ...cygnusOpts, followPrefix: "client:cygnus:" }),
    );
    await waitFor(() => expect(result.current.sessionKey).toBe("client:cygnus:hot"));
  });
});
