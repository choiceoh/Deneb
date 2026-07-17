import { useEffect, useState } from "react";

import {
  TRANSCRIPT_MAX,
  type GatewayConfig,
  type SessionRow,
  type TranscriptMsg,
  deleteSession,
  recentSessions,
  sessionTranscript,
} from "@/gateway";
import { type ChatTurn } from "@/hooks";
import { errText } from "@/format";

const MAIN_SESSION = "client:main";

// The AI panel's conversation-history state: the recent-sessions list, the active
// session key, the drawer-open flag, and switching/deleting/new-chat. Pulled out of
// AIPanel so the component is layout + compose. Takes useChat's clear/setTurns/busy
// because switching a session loads its transcript into the live chat.
export function useSessions(
  cfg: GatewayConfig,
  connected: boolean,
  busy: boolean,
  chat: { clear: () => void; setTurns: (turns: ChatTurn[]) => void },
  opts?: { mainKey?: string; filter?: string; newKey?: () => string },
) {
  // mainKey = the default session; newKey (if given) mints a *fresh* key per "새 대화"
  // so the 채팅 탭이 여러 client:main:* 대화를 가질 수 있다(work panel은 client:main 하나).
  // filter scopes the recent list to a namespace.
  const mainKey = opts?.mainKey ?? MAIN_SESSION;
  const filter = opts?.filter;
  const keep = (s: SessionRow[]) => (filter ? s.filter((r) => r.key.startsWith(filter)) : s);
  const [sessions, setSessions] = useState<SessionRow[]>([]);
  const [sessionKey, setSessionKey] = useState(mainKey);
  const [sessionsOpen, setSessionsOpen] = useState(false);
  const [sessionErr, setSessionErr] = useState("");
  // Older messages the initial transcript load left behind (server returns the
  // most recent N of `total`). null = nothing hidden / nothing loaded yet.
  const [hiddenHistory, setHiddenHistory] = useState<{ key: string; count: number } | null>(null);

  // Clear the list the moment the connection drops — adjusted during render
  // (https://react.dev/learn/you-might-not-need-an-effect) instead of inside the
  // effect so the reset doesn't trigger a second render pass after commit.
  const [prevConnected, setPrevConnected] = useState(connected);
  if (prevConnected !== connected) {
    setPrevConnected(connected);
    if (!connected) setSessions([]);
  }

  // Load recent sessions once connected (best-effort — older gateway / offline test
  // just leaves the list empty).
  useEffect(() => {
    if (!connected) return;
    let cancelled = false;
    void recentSessions(cfg, 20)
      .then((s) => !cancelled && setSessions(keep(s)))
      .catch(() => {});
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, cfg.url, cfg.token]);

  async function refreshSessions() {
    try {
      setSessions(keep(await recentSessions(cfg, 20)));
      setSessionErr("");
    } catch (e) {
      setSessionErr(errText(e));
    }
  }

  function toggleSessions() {
    const next = !sessionsOpen;
    setSessionsOpen(next);
    if (next) void refreshSessions();
  }

  function newChat() {
    if (busy) return;
    setSessionsOpen(false);
    // mint a fresh key when the caller provides one (채팅 탭 → 새 대화마다 새
    // client:main:<id>), else reuse the single main session (work panel → client:main).
    setSessionKey(opts?.newKey ? opts.newKey() : mainKey);
    setHiddenHistory(null);
    chat.clear();
  }

  const toTurns = (msgs: TranscriptMsg[], key: string): ChatTurn[] =>
    msgs.map((m, i) => ({
      id: m.id || `tr-${key}-${i}`,
      role: m.role === "user" ? "user" : "assistant",
      text: m.content,
      status: "done" as const,
    }));

  async function loadTranscript(key: string, limit?: number) {
    const { messages, total } = await sessionTranscript(cfg, key, limit);
    chat.setTurns(toTurns(messages, key));
    const hidden = total - messages.length;
    // The server clamps at TRANSCRIPT_MAX, so once we've loaded that much there
    // is no way to page further — drop the affordance instead of lying.
    setHiddenHistory(hidden > 0 && messages.length < TRANSCRIPT_MAX ? { key, count: hidden } : null);
    setSessionErr("");
  }

  // Switch conversations: load the picked session's transcript and continue it.
  async function selectSession(key: string) {
    if (busy) return;
    setSessionsOpen(false);
    setSessionKey(key);
    try {
      await loadTranscript(key);
    } catch (e) {
      setSessionErr(errText(e));
    }
  }

  // "이전 대화 더 불러오기" — refetch the same session at the server cap. The
  // transcript is the source of truth, so wholesale replace is safe (gated on
  // !busy: no in-flight turn to clobber).
  async function loadOlderTurns() {
    if (busy || !hiddenHistory) return;
    try {
      await loadTranscript(hiddenHistory.key, TRANSCRIPT_MAX);
    } catch (e) {
      setSessionErr(errText(e));
    }
  }

  async function removeSession(key: string) {
    // busy에는 첨부 배치(attaching)도 포함된다 — 진행 중 세션이 삭제되면 남은 턴이
    // 되살아난 유령 세션에 쌓인다. selectSession/newChat과 같은 게이트.
    if (busy) return;
    try {
      await deleteSession(cfg, key);
      setSessions((prev) => prev.filter((s) => s.key !== key));
      if (key === sessionKey) newChat();
    } catch (e) {
      setSessionErr(errText(e));
    }
  }

  return {
    sessions,
    sessionKey,
    sessionsOpen,
    sessionErr,
    hiddenHistory,
    toggleSessions,
    refreshSessions,
    selectSession,
    removeSession,
    newChat,
    loadOlderTurns,
  };
}
