import { useEffect, useRef, useState } from "react";

import {
  TRANSCRIPT_MAX,
  type GatewayConfig,
  type SessionRow,
  type TranscriptMsg,
  deleteSession,
  focusSession,
  pinSession,
  callRpc,
  recentSessions,
  recoverTurnAnswer,
  renameSession as renameSessionRpc,
  searchSessions,
  sessionTranscript,
  setModel as persistModel,
} from "@/gateway";
import { type AssistantPart, type ChatTurn } from "@/hooks";
import { onEventsHello, onGatewayEvent } from "@/events";
import { upsertToolPart } from "./chatParts";
import { errText } from "@/format";
import { getString, setString } from "@/storage";

const MAIN_SESSION = "client:main";
const LAST_SESSION_KEY = "andromeda.chat.lastSession";
// One drawer page. SESSION_MAX_PAGE mirrors the gateway's per-response cap
// (maxSessionsLimit in handlerminiapp/sessions), which is why a window wider
// than one page is assembled from several offset fetches.
const SESSION_PAGE = 20;
const SESSION_MAX_PAGE = 100;

// The AI panel's conversation-history state: the recent-sessions list, the active
// session key, the drawer-open flag, and switching/deleting/new-chat. Pulled out of
// AIPanel so the component is layout + compose. Takes useChat's clear/setTurns/busy
// because switching a session loads its transcript into the live chat.
export function useSessions(
  cfg: GatewayConfig,
  connected: boolean,
  busy: boolean,
  chat: {
    clear: () => void;
    setTurns: (turns: ChatTurn[]) => void;
    patchTurns?: (fn: (turns: ChatTurn[]) => ChatTurn[]) => void;
  },
  opts?: {
    mainKey?: string;
    filter?: string;
    channel?: string;
    newKey?: () => string;
    lastKeyStore?: string;
    followPrefix?: string;
  },
) {
  // mainKey = the default session; newKey (if given) mints a *fresh* key per "새 대화"
  // so the 채팅 탭이 여러 client:main:* 대화를 가질 수 있다(work panel은 client:main 하나).
  // channel scopes the recent list SERVER-side (so `limit` applies to that channel,
  // not the newest-N across all channels); filter is the client-side namespace guard.
  const mainKey = opts?.mainKey ?? MAIN_SESSION;
  const filter = opts?.filter;
  const channel = opts?.channel;
  // lastKeyStore = the localStorage slot the restored "last conversation" lives
  // in. Each surface that mints its own keys MUST use its own slot — Cygnus and
  // the 채팅 탭 sharing one slot made either surface boot into (and load the
  // transcript of) the other's conversation.
  const lastKeyStore = opts?.lastKeyStore ?? LAST_SESSION_KEY;
  // followPrefix scopes gateway focus-follow (adopting the conversation the
  // user just touched on another device) to THIS surface's minted-key
  // namespace. The old hardcoded "client:main:" made Cygnus adopt the 채팅
  // 탭/모바일's freshest conversation at boot.
  const followPrefix = opts?.followPrefix ?? "client:main:";
  const keep = (s: SessionRow[]) => (filter ? s.filter((r) => r.key.startsWith(filter)) : s);
  const [sessions, setSessions] = useState<SessionRow[]>([]);
  const [sessionKey, setSessionKey] = useState(() => {
    if (!opts?.newKey) return mainKey;
    const stored = getString(lastKeyStore).trim();
    // Namespace guard: a restored key from outside this surface's namespace
    // (stale slot contents, another surface's write) must not hijack the boot
    // session — fall back to this surface's own main.
    if (stored && filter && !stored.startsWith(filter)) return mainKey;
    return stored || mainKey;
  });
  const persistKey = (key: string) => {
    setSessionKey(key);
    if (opts?.newKey) setString(lastKeyStore, key);
    void focusSession(cfg, key).catch(() => {});
  };
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

  // 20 rows covers a working day; 이전 대화 더 보기 pulls the next page. Paging
  // (not a bigger single fetch) is what keeps old conversations reachable: the
  // gateway caps one response at 100 and conversations are no longer GC'd, so a
  // limit-only drawer would silently stop at that cap as history accumulates.
  const [loadedCount, setLoadedCount] = useState(SESSION_PAGE);
  const [fetchedCount, setFetchedCount] = useState(0);
  const [sessionTotal, setSessionTotal] = useState(0);
  // A ref so the connect-time refetch backstops below always refresh the window
  // the user has actually pulled in, without re-running their effect.
  const loadedRef = useRef(loadedCount);
  useEffect(() => {
    loadedRef.current = loadedCount;
  }, [loadedCount]);

  // Fetch the first `count` rows as server-capped pages, so a window wider than
  // one page still comes back whole. Returns raw rows (the namespace filter is
  // applied by the caller) plus the size of the whole scoped set.
  async function fetchWindow(count: number): Promise<{ rows: SessionRow[]; total: number; focus?: string }> {
    const rows: SessionRow[] = [];
    let total = 0;
    let focus: string | undefined;
    for (let offset = 0; offset < count; offset += SESSION_MAX_PAGE) {
      const page = await recentSessions(cfg, Math.min(SESSION_MAX_PAGE, count - offset), channel, offset);
      if (offset === 0) focus = page.focus;
      total = page.total;
      rows.push(...page.sessions);
      if (page.sessions.length === 0 || rows.length >= total) break;
    }
    return { rows, total, focus };
  }

  function applyPage(page: { rows: SessionRow[]; total: number; focus?: string }) {
    const rows = [...keep(page.rows)].sort((a, b) => Number(Boolean(b.pinned)) - Number(Boolean(a.pinned)));
    setSessions(rows);
    setFetchedCount(page.rows.length);
    setSessionTotal(page.total);
  }

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
    let landed = false; // a fetch has succeeded at least once
    let loadFailed = false; // this loader owns the visible error
    let retryMs = 8000;
    let retry: ReturnType<typeof setTimeout> | undefined;
    const timers: ReturnType<typeof setTimeout>[] = [];
    // Failures used to be swallowed outright: if the gateway was down or
    // mid-hot-swap for the whole 0/1.5/4s window (app launched during an
    // auto-deploy), the list stayed empty and silent forever — the 채팅 탭 has no
    // drawer-open refresh to retry from, so only a window focus healed it. Keep
    // retrying on a widening backoff until the first fetch lands, and say so
    // meanwhile: "불러오지 못함" must not look like "대화가 없음".
    function armRetry() {
      if (cancelled || landed || retry) return;
      retry = setTimeout(() => {
        retry = undefined;
        load();
      }, retryMs);
      retryMs = Math.min(retryMs * 2, 60000);
    }
    function load() {
      void fetchWindow(loadedRef.current)
        .then((page) => {
          if (cancelled) return;
          landed = true;
          applyPage(page);
          if (opts?.newKey && page.focus && sessionKey === mainKey && page.focus.startsWith(followPrefix)) {
            persistKey(page.focus);
            void loadTranscript(page.focus).catch(() => {});
          }
          // Only clear what this loader put up — a transcript/delete error the
          // user has not seen yet must not be wiped by a background refresh.
          if (loadFailed) {
            loadFailed = false;
            setSessionErr("");
          }
        })
        .catch((e) => {
          if (cancelled || landed) return;
          loadFailed = true;
          setSessionErr(errText(e));
          armRetry();
        });
    }
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
      if (retry) clearTimeout(retry);
      window.removeEventListener("focus", refresh);
      document.removeEventListener("visibilitychange", refresh);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, cfg.url, cfg.token]);

  const restoredRef = useRef(false);
  useEffect(() => {
    if (!connected || restoredRef.current || !opts?.newKey || sessionKey === mainKey) return;
    restoredRef.current = true;
    void loadTranscript(sessionKey).catch((e) => setSessionErr(errText(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected]);

  async function refreshSessions(count = loadedCount) {
    try {
      applyPage(await fetchWindow(count));
      setSessionErr("");
    } catch (e) {
      setSessionErr(errText(e));
    }
  }

  // The drawer's "이전 대화 더 보기" — one more page. Compare against the RAW
  // fetched count, not the filtered rows: a namespace filter that drops rows
  // would otherwise keep offering a next page forever.
  const canLoadMoreSessions = fetchedCount < sessionTotal;
  async function loadMoreSessions() {
    const next = loadedCount + SESSION_PAGE;
    setLoadedCount(next);
    await refreshSessions(next);
  }

  async function pinConversation(key: string, pinned: boolean) {
    try {
      await pinSession(cfg, key, pinned);
      setSessions((prev) =>
        [...prev.map((s) => (s.key === key ? { ...s, pinned } : s))].sort(
          (a, b) => Number(Boolean(b.pinned)) - Number(Boolean(a.pinned)),
        ),
      );
      setSessionErr("");
    } catch (e) {
      setSessionErr(errText(e));
    }
  }

  async function searchConversationHits(query: string) {
    const q = query.trim();
    if (!q) return [];
    try {
      return await searchSessions(cfg, q);
    } catch (e) {
      setSessionErr(errText(e));
      return [];
    }
  }

  async function resetConversationModel(key: string) {
    try {
      await persistModel(cfg, "", "main", key);
      setSessions((prev) => prev.map((s) => (s.key === key ? { ...s, model: "" } : s)));
      setSessionErr("");
    } catch (e) {
      setSessionErr(errText(e));
    }
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
    stopRunningWatch();
    if (busy) return;
    setSessionsOpen(false);
    // mint a fresh key when the caller provides one (채팅 탭 → 새 대화마다 새
    // client:main:<id>), else reuse the single main session (work panel → client:main).
    persistKey(opts?.newKey ? opts.newKey() : mainKey);
    setHiddenHistory(null);
    chat.clear();
  }

  const toTurns = (msgs: TranscriptMsg[], key: string): ChatTurn[] =>
    msgs.map((m, i) => {
      // Rebuild tool chips from the server's toolTrace so a restored
      // conversation shows the same tool activity the live stream did. The
      // calls ran before the message's text was written, so chips precede it.
      const parts: AssistantPart[] = (m.toolTrace ?? []).map((t, j) => ({
        kind: "tool" as const,
        id: `tr-${key}-${i}-t${j}`,
        tool: t.tool,
        state: "completed" as const,
        detail: t.detail || undefined,
        isError: t.isError || undefined,
        resultSummary: t.summary || undefined,
        resultPreview: t.preview || undefined,
      }));
      if (parts.length > 0 && m.content) parts.push({ kind: "text", text: m.content });
      return {
        id: m.id || `tr-${key}-${i}`,
        role: m.role === "user" ? ("user" as const) : ("assistant" as const),
        text: m.content,
        reasoning: m.reasoning,
        status: "done" as const,
        // Textless turns keep parts undefined so the plain-body path renders.
        parts: parts.length > 0 ? parts : undefined,
        // Regenerate replays lastSendRef, which only a live send fills — on a
        // restored turn the button would render but do nothing (dead control).
        canRegenerate: false,
      };
    });

  // A turn started elsewhere (the phone, a closed window) can still be running
  // when this surface opens the session — the transcript alone would show a
  // stale conversation and the answer would never arrive. Watch it: show the
  // working sparkle, poll through the shared recovery helper, and reload the
  // transcript (answer + tool chips + footprint in one shot) when it lands.
  const runningWatch = useRef<AbortController | null>(null);
  const spectateStop = useRef<(() => void) | null>(null);
  function stopRunningWatch() {
    runningWatch.current?.abort();
    runningWatch.current = null;
    spectateStop.current?.();
    spectateStop.current = null;
  }

  // Live spectate: while the foreign turn runs, subscribe this events-stream
  // connection to the session's agent events (tool.start/tool.end/run.end) and
  // render them as chips on the placeholder — the same activity the owning
  // stream shows, arriving live instead of after the fact. The transcript
  // reload on run.end (or the poll fallback) still delivers the answer text.
  function startSpectate(key: string, placeholderId: string): () => void {
    let stopped = false;
    let subscribedConn: string | null = null;
    const subscribe = (connId: string) => {
      subscribedConn = connId;
      void callRpc(cfg, "miniapp.sessions.events.subscribe", { connId, sessionKey: key }).catch(() => {
        subscribedConn = null; // legacy gateway without the event plane — poll fallback covers us
      });
    };
    // Fires immediately when the stream is already up, and again on reconnect
    // (a fresh connId voids server-side subscriptions — resubscribe).
    const offHello = onEventsHello((connId) => {
      if (!stopped) subscribe(connId);
    });
    const offGateway = onGatewayEvent((frame) => {
      if (stopped || frame.event !== "agent.event") return;
      const p = frame.payload;
      if (p.sessionKey !== key) return;
      const kind = typeof p.kind === "string" ? p.kind : "";
      const inner = (p.payload && typeof p.payload === "object" ? p.payload : {}) as Record<string, unknown>;
      if (kind === "tool.start" || kind === "tool.end") {
        const ev = {
          state: kind === "tool.start" ? "started" : "completed",
          tool: typeof inner.tool === "string" ? inner.tool : "",
          toolUseId: typeof inner.toolUseId === "string" ? inner.toolUseId : "",
          detail: typeof inner.detail === "string" ? inner.detail : undefined,
          resultSummary: typeof inner.summary === "string" ? inner.summary : undefined,
          isError: inner.isError === true || undefined,
        };
        if (!ev.tool) return;
        chat.patchTurns?.((turns) => turns.map((t) => (t.id === placeholderId ? upsertToolPart(t, ev) : t)));
        return;
      }
      if (kind === "run.end") {
        // Faster than the next 3s poll — and loadTranscript stops this spectate.
        void loadTranscript(key).catch(() => {});
      }
    });
    return () => {
      stopped = true;
      offHello();
      offGateway();
      if (subscribedConn) {
        void callRpc(cfg, "miniapp.sessions.events.unsubscribe", { connId: subscribedConn, sessionKey: key }).catch(
          () => {},
        );
      }
    };
  }
  // A live send in this surface takes over the stream — the watcher's reload
  // would clobber the streaming turn. Unmount must not leak the poll either.
  useEffect(() => {
    if (busy) stopRunningWatch();
  }, [busy]);
  useEffect(() => stopRunningWatch, []);

  function watchRunningTurn(key: string, sentText: string) {
    const ctrl = new AbortController();
    runningWatch.current = ctrl;
    void (async () => {
      // Null = lost/anchor-miss; the reload below re-checks turnRunning and
      // re-arms if the turn is genuinely still going, so a miss degrades to a
      // slow transcript refresh instead of a dropped answer.
      await recoverTurnAnswer(cfg, key, sentText, {
        signal: ctrl.signal,
        // Injected (not the helper's internal default) so the poll goes
        // through THIS module's sessionTranscript import — one seam for
        // tests and for any future per-surface fetch shaping.
        fetchSnapshot: (c, s) =>
          sessionTranscript(c, s).then((r) => ({ messages: r.messages, turnRunning: r.turnRunning })),
      }).catch(() => null);
      if (ctrl.signal.aborted) return;
      runningWatch.current = null;
      await loadTranscript(key).catch(() => {});
    })();
  }

  async function loadTranscript(key: string, limit?: number) {
    stopRunningWatch();
    const { messages, total, turnRunning } = await sessionTranscript(cfg, key, limit);
    const turns = toTurns(messages, key);
    if (turnRunning) {
      const lastUser = [...messages].reverse().find((m) => m.role === "user");
      turns.push({
        id: `running-${key}`,
        role: "assistant",
        text: "",
        status: "streaming",
        // Real start when the transcript carries it — the elapsed timer then
        // reads "this turn has been going N seconds", not "since I looked".
        startedAt: lastUser?.timestampMs || Date.now(),
        canRegenerate: false,
      });
      watchRunningTurn(key, lastUser?.content ?? "");
      spectateStop.current = startSpectate(key, `running-${key}`);
    }
    chat.setTurns(turns);
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
    persistKey(key);
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
    pinConversation,
    searchConversationHits,
    resetConversationModel,
    canLoadMoreSessions,
    loadMoreSessions,
    newChat,
    loadOlderTurns,
  };
}
