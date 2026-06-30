import { useEffect, useState } from "react";

import { type GatewayConfig, type SessionRow, deleteSession, recentSessions, sessionTranscript } from "@/gateway";
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
  opts?: { mainKey?: string; filter?: string; newKey?: () => string; boundKey?: string },
) {
  // mainKey = the default session; newKey (if given) mints a *fresh* key per "새 대화".
  // boundKey (if given) ties the active conversation to an external selector — the work
  // panel passes the per-pane key so each work area (메일·일정·…) keeps its own thread;
  // when it changes, the bound session's transcript is loaded automatically below.
  // filter scopes the recent list to a namespace so namespaces don't mix.
  const mainKey = opts?.mainKey ?? MAIN_SESSION;
  const boundKey = opts?.boundKey;
  const filter = opts?.filter;
  const keep = (s: SessionRow[]) => (filter ? s.filter((r) => r.key.startsWith(filter)) : s);
  const [sessions, setSessions] = useState<SessionRow[]>([]);
  const [sessionKey, setSessionKey] = useState(boundKey ?? mainKey);
  const [sessionsOpen, setSessionsOpen] = useState(false);
  const [sessionErr, setSessionErr] = useState("");

  // Load recent sessions once connected (best-effort — older gateway / offline test
  // just leaves the list empty).
  useEffect(() => {
    if (!connected) {
      setSessions([]);
      return;
    }
    let cancelled = false;
    void recentSessions(cfg, 20)
      .then((s) => !cancelled && setSessions(keep(s)))
      .catch(() => {});
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, cfg.url, cfg.token]);

  // Pane binding: when the work panel's active pane changes, switch to that pane's
  // session and load its transcript so the conversation resumes where it left off.
  // The composer is disabled while busy, so this never races a live send; switching
  // panes mid-stream simply resumes the new pane (the in-flight reply still completes
  // server-side and reappears when that pane is reopened). No-op when unbound (채팅 탭).
  useEffect(() => {
    if (!boundKey) return;
    setSessionsOpen(false);
    setSessionKey(boundKey);
    if (!connected) {
      chat.setTurns([]);
      return;
    }
    let cancelled = false;
    void sessionTranscript(cfg, boundKey)
      .then((msgs) => {
        if (cancelled) return;
        chat.setTurns(
          msgs.map((m, i) => ({
            id: m.id || `tr-${boundKey}-${i}`,
            role: m.role === "user" ? "user" : "assistant",
            text: m.content,
            status: "done" as const,
          })),
        );
      })
      // A pane with no prior conversation has no transcript yet — start clean, no error.
      .catch(() => !cancelled && chat.setTurns([]));
    return () => {
      cancelled = true;
    };
    // chat.setTurns is the stable useState setter; re-run only when the bound pane,
    // connection, or gateway target changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [boundKey, connected, cfg.url, cfg.token]);

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
    // mint a fresh key when the caller provides one (채팅 탭 → 새 대화마다 새 chat:<id>),
    // else reuse the single main session (work panel → client:main).
    setSessionKey(opts?.newKey ? opts.newKey() : mainKey);
    chat.clear();
  }

  // Switch conversations: load the picked session's transcript and continue it.
  async function selectSession(key: string) {
    if (busy) return;
    setSessionsOpen(false);
    setSessionKey(key);
    try {
      const msgs = await sessionTranscript(cfg, key);
      chat.setTurns(
        msgs.map((m, i) => ({
          id: m.id || `tr-${key}-${i}`,
          role: m.role === "user" ? "user" : "assistant",
          text: m.content,
          status: "done" as const,
        })),
      );
      setSessionErr("");
    } catch (e) {
      setSessionErr(errText(e));
    }
  }

  async function removeSession(key: string) {
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
    toggleSessions,
    refreshSessions,
    selectSession,
    removeSession,
    newChat,
  };
}
