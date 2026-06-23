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
) {
  const [sessions, setSessions] = useState<SessionRow[]>([]);
  const [sessionKey, setSessionKey] = useState(MAIN_SESSION);
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
      .then((s) => !cancelled && setSessions(s))
      .catch(() => {});
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, cfg.url, cfg.token]);

  async function refreshSessions() {
    try {
      setSessions(await recentSessions(cfg, 20));
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
    setSessionKey(MAIN_SESSION);
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

  return { sessions, sessionKey, sessionsOpen, sessionErr, toggleSessions, selectSession, removeSession, newChat };
}
