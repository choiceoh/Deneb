import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { type GatewayConfig, deleteSession, recentSessions, sessionTranscript } from "@/gateway";
import { useSessions } from "./useSessions";

vi.mock("@/gateway", () => ({
  TRANSCRIPT_MAX: 200,
  deleteSession: vi.fn(),
  recentSessions: vi.fn(),
  renameSession: vi.fn(),
  sessionTranscript: vi.fn(),
}));

const cfg: GatewayConfig = { url: "http://test", token: "token" };
const recent = vi.mocked(recentSessions);
const transcript = vi.mocked(sessionTranscript);
const remove = vi.mocked(deleteSession);

function chatDouble() {
  return { clear: vi.fn(), setTurns: vi.fn() };
}

beforeEach(() => {
  recent.mockResolvedValue([]);
  transcript.mockResolvedValue({ messages: [], total: 0 });
  remove.mockResolvedValue(true);
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("useSessions", () => {
  it("returns namespace-filtered sessions when gateway lists recent", async () => {
    recent.mockResolvedValue([
      { key: "client:main:a", label: "A" },
      { key: "client:main:b", label: "B" },
      { key: "system:heartbeat", label: "system" },
    ]);
    const chat = chatDouble();
    const { result } = renderHook(() => useSessions(cfg, true, false, chat, { filter: "client:main:" }));

    await waitFor(() => expect(result.current.sessions.map((s) => s.key)).toEqual(["client:main:a", "client:main:b"]));
    expect(recent).toHaveBeenCalledWith(cfg, 20);
  });

  it("refetches on window focus so a mid-restore partial list self-heals", async () => {
    // First fetch lands while the gateway is still restoring sessions in the
    // background (server_lifecycle.go) — only client:main is back yet.
    recent.mockResolvedValueOnce([{ key: "client:main", label: "업무" }]);
    const chat = chatDouble();
    const { result } = renderHook(() => useSessions(cfg, true, false, chat, { filter: "client:" }));
    await waitFor(() => expect(result.current.sessions.map((s) => s.key)).toEqual(["client:main"]));

    // Restore has since finished; a focus refresh must pick up the full list
    // rather than staying frozen on the mid-restore snapshot.
    recent.mockResolvedValue([
      { key: "client:main", label: "업무" },
      { key: "client:main:mrudefc16xpd", label: "근거 확인 및 판정 기록 완료" },
    ]);
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
    });
    const chat = chatDouble();
    const { result } = renderHook(() => useSessions(cfg, true, false, chat));

    await act(async () => result.current.selectSession("client:main:abc"));

    expect(result.current.sessionKey).toBe("client:main:abc");
    expect(chat.setTurns).toHaveBeenCalledWith([
      { id: "u1", role: "user", text: "질문", status: "done" },
      { id: "tr-client:main:abc-1", role: "assistant", text: "답변", status: "done" },
      { id: "sys", role: "assistant", text: "상태", status: "done" },
    ]);
    expect(result.current.sessionErr).toBe("");
  });

  it("when deleting the active session removes it and mints a fresh conversation", async () => {
    recent.mockResolvedValue([
      { key: "client:main", label: "업무" },
      { key: "client:main:old", label: "Old" },
    ]);
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

  it("clears stale session rows immediately on disconnect", async () => {
    recent.mockResolvedValue([{ key: "client:main", label: "업무" }]);
    const chat = chatDouble();
    const { result, rerender } = renderHook(({ connected }) => useSessions(cfg, connected, false, chat), {
      initialProps: { connected: true },
    });
    await waitFor(() => expect(result.current.sessions).toHaveLength(1));

    rerender({ connected: false });

    expect(result.current.sessions).toEqual([]);
  });
});
