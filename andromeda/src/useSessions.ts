import { useEffect, useRef, useState } from "react";

import {
  TRANSCRIPT_MAX,
  type GatewayConfig,
  type SessionRow,
  type TranscriptMsg,
  deleteSession,
  recentSessions,
  renameSession as renameSessionRpc,
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
  opts?: { mainKey?: string; filter?: string; channel?: string; newKey?: () => string },
) {
  // mainKey = the default session; newKey (if given) mints a *fresh* key per "새 대화"
  // so the 채팅 탭이 여러 client:main:* 대화를 가질 수 있다(work panel은 client:main 하나).
  // channel scopes the recent list SERVER-side (so `limit` applies to that channel,
  // not the newest-N across all channels); filter is the client-side namespace guard.
  const mainKey = opts?.mainKey ?? MAIN_SESSION;
  const filter = opts?.filter;
  const channel = opts?.channel;
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

  // 20 rows covers a working day; 최근 대화 더 보기 raises to the server cap so
  // older conversations stay reachable without an unbounded first fetch.
  const [sessionsLimit, setSessionsLimit] = useState(20);
  // A ref so the connect-time refetch backstops below always fetch at the
  // *current* limit (20, or 100 after "더 보기") without re-running their effect.
  const limitRef = useRef(sessionsLimit);
  useEffect(() => {
    limitRef.current = sessionsLimit;
  }, [sessionsLimit]);

  // Load recent sessions once connected — then a couple more times on a short
  // backoff, and again whenever the window regains focus.
  //
  // Why the extra refetches (not a plain one-shot): the gateway flips `ready`
  // and starts serving BEFORE its background session restore finishes
  // (server_lifecycle.go — restoreAndWakeSessions runs in a goroutine after
  // ready flips). Because it hot-swaps every few minutes, a fetch landing in
  // that restore window sees a partial list — often just client:main once the
  // client: filter runs — and, as a one-shot fetch, the drawer stayed frozen on
  // that mid-restore snapshot until the next send (the "네이티브엔 있는데
  // 안드로메다 채팅엔 안 보임" bug). The staggered refetches ride out the restore
  // window; the focus refresh self-heals any later staleness (incl. a session
  // started on the phone). Best-effort — an older/offline gateway just leaves
  // the list empty.
  useEffect(() => {
    if (!connected) return;
    let cancelled = false;
    const timers: ReturnType<typeof setTimeout>[] = [];
    const load = () => {
      void recentSessions(cfg, limitRef.current, channel)
        .then((s) => !cancelled && setSessions(keep(s)))
        .catch(() => {});
    };
    load();
    timers.push(setTimeout(load, 1500), setTimeout(load, 4000));
    const refresh = () => {
      if (cancelled || document.visibilityState === "hidden") return;
      load();
    };
    window.addEventListener("focus", refresh);
    document.addEventListener("visibilitychange", refresh);
    return () => {
      cancelled = true;
      timers.forEach(clearTimeout);
      window.removeEventListener("focus", refresh);
      document.removeEventListener("visibilitychange", refresh);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, cfg.url, cfg.token]);

  async function refreshSessions(limit = sessionsLimit) {
    try {
      setSessions(keep(await recentSessions(cfg, limit, channel)));
      setSessionErr("");
    } catch (e) {
      setSessionErr(errText(e));
    }
  }

  // The drawer's "이전 대화 더 보기" — one step to the gateway's cap (100).
  const canLoadMoreSessions = sessionsLimit < 100 && sessions.length >= sessionsLimit;
  async function loadMoreSessions() {
    setSessionsLimit(100);
    await refreshSessions(100);
  }

  async function renameSession(key: string, label: string) {
    const next = label.trim();
    if (!next) return;
    try {
      await renameSessionRpc(cfg, key, next);
      setSessions((prev) => prev.map((s) => (s.key === key ? { ...s, label: next } : s)));
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
      reasoning: m.reasoning,
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
    renameSession,
    canLoadMoreSessions,
    loadMoreSessions,
    newChat,
    loadOlderTurns,
  };
}
