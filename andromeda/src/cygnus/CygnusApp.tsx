import { useEffect, useMemo, useRef, useState } from "react";

import { DataProviderScope } from "@/crud";
import { denebDataProvider } from "@/dataProvider";
import { useDesktopChrome } from "@/desktopChrome";
import { type GatewayConfig, loadConfig, saveConfig } from "@/gateway";
import { useEvents, useChat } from "@/hooks";
import { parseUiSubmission } from "@/markdown/denebUiParse";
import { getString, setString } from "@/storage";
import { readDesktopToken } from "@/tauri";
import { useAttachPipeline, useComposerBehavior, useModels, useSessionDraft } from "@/useChatSurface";
import { useFileDrop } from "@/useFileDrop";
import { useSessions } from "@/useSessions";
import { useStickyScroll } from "@/useStickyScroll";
import { AssistantBody, AssistantTurnActions } from "@/components/AssistantBody";
import { ChatComposer, ScrollToBottomButton } from "@/components/ChatComposer";
import { DenebStar } from "@/components/DenebStar";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { Icon } from "@/components/Icon";
import { LiveDot } from "@/components/LiveDot";
import { ModelPicker } from "@/components/ModelPicker";
import { SessionDrawer } from "@/components/SessionDrawer";
import { UiSubmissionBubble } from "@/components/UiSubmission";
import { WindowControls } from "@/components/WindowControls";
// The coding surface's mono voice — bundled so the real shell (webkit on
// Linux/macOS/Windows) doesn't fall back to a chunky system mono.
import "@fontsource/jetbrains-mono/400.css";
import "@fontsource/jetbrains-mono/500.css";
import "./cygnus.css";

// Cygnus — the agent workspace window. Andromeda stays the 업무비서 workstation;
// this surface is for DRIVING the agent: kicking off tasks, light coding runs,
// watching tool calls stream.
//
// Positioning (operator call, 2026-08-29): a WORKSPACE, not a summon-and-
// dismiss launcher. It was built on the Perplexity Portable Computer shell's
// summonable pattern (480px, tray + global shortcut), but that size is what
// forced the thread list into a scrim overlay — the surface is somewhere the
// operator stays, so the list docks and the window manages like any other
// (minimize/maximize/close). The tray and the global shortcut remain as
// conveniences for reaching it, not as its identity.
//
// Same gateway, same shared chat
// modules (useChat/AssistantBody/ChatComposer — the parity rule), its own
// session namespace (client:cygnus:*) and its own light-first token skin
// (cygnus.css re-values the workstation token names under .cygnus-root).
const CYGNUS_MAIN = "client:cygnus:main";
const THEME_KEY = "cygnus.theme";
const RAIL_KEY = "cygnus.rail";

// Width at which the thread rail DOCKS beside the conversation instead of
// covering it. Must match the @media breakpoint in cygnus.css — the CSS owns
// the layout, this constant only tells the behaviour half (auto-close) which
// mode is on screen. Below it the rail is an overlay with a scrim, so leaving
// it open after a pick would hide the thread the user just chose.
const CYGNUS_DOCK_PX = 560;

/** True while the window is wide enough for the docked rail. */
function useDockedRail(): boolean {
  const query = `(min-width: ${CYGNUS_DOCK_PX}px)`;
  const [docked, setDocked] = useState(
    () => typeof window.matchMedia === "function" && window.matchMedia(query).matches,
  );
  useEffect(() => {
    if (typeof window.matchMedia !== "function") return;
    const mq = window.matchMedia(query);
    const sync = () => setDocked(mq.matches);
    sync();
    mq.addEventListener("change", sync);
    return () => mq.removeEventListener("change", sync);
  }, [query]);
  return docked;
}

// Empty-state starters: what this surface is FOR, in the user's own words. They
// fill the composer (never send on their own) so the phrasing stays editable.
const CYGNUS_STARTERS = ["레포 상태 요약", "이 폴더 TODO 찾기", "스크립트 하나 짜줘"];

export function CygnusApp() {
  // Same config bootstrap as the workstation App: shared localStorage + desktop
  // keychain/token-file auto-fill, and the same DataProviderScope — useChat's
  // post-turn cache invalidation reaches through it, so the scope is
  // load-bearing here even though Cygnus renders no CRUD panes.
  const [cfg, setCfg] = useState<GatewayConfig>(loadConfig());
  const dataProvider = useMemo(() => denebDataProvider(cfg), [cfg]);
  const connected = Boolean(cfg.url && cfg.token);
  useDesktopChrome();
  useEffect(() => {
    if (cfg.token) return;
    let cancelled = false;
    void readDesktopToken().then((token) => {
      if (cancelled || !token) return;
      setCfg((c) => {
        if (c.token) return c;
        const next = { ...c, token };
        saveConfig(next);
        return next;
      });
    });
    return () => {
      cancelled = true;
    };
  }, [cfg.token]);
  useEffect(() => {
    document.title = "Cygnus";
    // The workstation stylesheet paints body with the warm --grad; on this
    // transparent frameless window that gradient would peek past .cygnus-root's
    // rounded corners. Marking body lets cygnus.css make it transparent.
    document.body.classList.add("cygnus-window");
    return () => document.body.classList.remove("cygnus-window");
  }, []);

  return (
    <ErrorBoundary>
      <DataProviderScope dataProvider={dataProvider}>
        <CygnusSurface cfg={cfg} connected={connected} />
      </DataProviderScope>
    </ErrorBoundary>
  );
}

// The chat surface proper — split from CygnusApp so every hook that needs the
// data-provider context (useChat → useInvalidate) mounts below the scope.
function CygnusSurface({ cfg, connected }: { cfg: GatewayConfig; connected: boolean }) {
  // Light-first (operator call, 2026-08-28) — dark stays one toggle away.
  // Dev-only `?theme=dark` lets the headless screenshot loop capture the other
  // skin (it cannot click the toggle). Stripped from production builds.
  const [theme, setTheme] = useState(() => {
    if (import.meta.env.DEV) {
      const forced = new URLSearchParams(window.location.search).get("theme");
      if (forced === "dark" || forced === "light") return forced;
    }
    return getString(THEME_KEY) === "dark" ? "dark" : "light";
  });
  function toggleTheme() {
    const next = theme === "dark" ? "light" : "dark";
    setTheme(next);
    setString(THEME_KEY, next);
  }

  const {
    thinking,
    busy,
    stoppable,
    turns,
    send,
    captureBatch,
    stop,
    regenerate,
    variants,
    selectVariant,
    clear,
    setTurns,
    patchTurns,
  } = useChat(cfg);

  // Hold the gateway events stream open in this window too. The workstation
  // gets it via ProactivePanel; without one here the spectate surface (live
  // chips for a turn started elsewhere) never receives its hello/agent frames.
  // The proactive list itself is unused — Cygnus stays a chat surface.
  useEvents(cfg, connected);
  const [input, setInput] = useState("");
  const [attaching, setAttaching] = useState(false);
  // The thread rail is a standing part of the surface, not a popup: docked, it
  // defaults open and remembers the user's choice across summons and restarts.
  // It only auto-opens when it can DOCK, though — in overlay mode an open rail
  // sits on a scrim over the conversation, so opening it unasked would mean
  // every summon greets the user with a covered thread. There the toggle is an
  // explicit, per-visit action. (Dev-only `?rail` still forces it open so the
  // headless screenshot loop can capture it — a one-shot screenshot can't click.)
  const docked = useDockedRail();
  const [railOpen, setRailOpen] = useState(() => {
    if (import.meta.env.DEV && new URLSearchParams(window.location.search).has("rail")) return true;
    const wideEnough =
      typeof window.matchMedia === "function" && window.matchMedia(`(min-width: ${CYGNUS_DOCK_PX}px)`).matches;
    return wideEnough && getString(RAIL_KEY) !== "closed";
  });
  function setRail(open: boolean) {
    setRailOpen(open);
    setString(RAIL_KEY, open ? "open" : "closed");
  }
  // Picking a thread closes the rail ONLY in overlay mode, where it sits on top
  // of the conversation it just opened. Docked, it stays — that is the point of
  // a permanent list.
  function closeRailIfOverlay() {
    if (!docked) setRail(false);
  }
  const composeRef = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  // Own namespace under the client channel so agent/coding threads don't get
  // labels mixed into the 업무 chat's client:main:* space (they still share the
  // gateway's client session plumbing — pin/rename/search all apply).
  const {
    sessions,
    sessionKey,
    sessionErr,
    hiddenHistory,
    selectSession,
    removeSession,
    renameSession,
    pinConversation,
    searchConversationHits,
    resetConversationModel,
    canLoadMoreSessions,
    loadMoreSessions,
    newChat,
    refreshSessions,
    loadOlderTurns,
  } = useSessions(
    cfg,
    connected,
    busy || attaching,
    { clear, setTurns, patchTurns },
    {
      mainKey: CYGNUS_MAIN,
      channel: "client",
      filter: "client:cygnus:",
      newKey: () => `client:cygnus:${crypto.randomUUID()}`,
      // Own restore slot — sharing the 채팅 탭's slot made either surface boot
      // into the other's conversation (and load its transcript).
      lastKeyStore: "cygnus.chat.lastSession",
      // And follow gateway focus only within our own namespace — otherwise the
      // conversation just touched on the phone/채팅 탭 hijacks the boot session.
      followPrefix: "client:cygnus:",
    },
  );
  const { clearDraft } = useSessionDraft(sessionKey, input, setInput);
  const sessionModel = sessions.find((s) => s.key === sessionKey)?.model ?? "";
  const { models, model, setModel } = useModels(cfg, connected, sessionKey, sessionModel);
  const { ref: transcriptRef, onScroll, pin, atBottom, scrollToBottom } = useStickyScroll([turns, thinking]);
  useComposerBehavior(composeRef, { input, busy, hidden: false, focusOnReveal: true });

  // Summon-to-type: the OS window regaining focus (tray click / global
  // shortcut) puts the caret straight into the composer — a summonable
  // surface must be typeable without a mouse click.
  useEffect(() => {
    const onWindowFocus = () => composeRef.current?.focus();
    window.addEventListener("focus", onWindowFocus);
    return () => window.removeEventListener("focus", onWindowFocus);
  }, []);

  // Esc stops the running turn — the status row advertises the key, so it has
  // to actually do it. Ignored while an IME composition is open (Esc cancels
  // the candidate window first) and when the turn is not abortable.
  useEffect(() => {
    if (!busy || !stoppable) return;
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== "Escape" || e.isComposing) return;
      e.preventDefault();
      stop();
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [busy, stoppable, stop]);

  const { attachNote, attachingRef, attachFiles, onPick, staged, removeStaged, sendStaged } = useAttachPipeline({
    connected,
    busy,
    input,
    setInput,
    setAttaching,
    pin,
    captureBatch: (files, caption) => captureBatch(files, { sessionKey, caption }),
    onBatchDone: () => void refreshSessions(),
  });

  function submit(message = input) {
    const msg = message.trim();
    if (busy || attachingRef.current || !connected) return;
    if (staged.length > 0) {
      void sendStaged(msg);
      return;
    }
    if (!msg) return;
    clearDraft();
    setInput("");
    pin();
    void send(msg, { model: model || undefined, sessionKey }).then(() => void refreshSessions());
  }

  const { over: dropOver, dropProps } = useFileDrop(!busy && connected, (files) => void attachFiles(files));

  const lastId = turns.at(-1)?.id;
  const lastAssistantId = [...turns].reverse().find((t) => t.role === "assistant")?.id;
  const sessionLabel = sessions.find((s) => s.key === sessionKey)?.label || "새 스레드";

  return (
    <div className="cygnus-root" data-theme={theme}>
      <header className="cy-titlebar" data-tauri-drag-region>
        <DenebStar size={16} />
        <span className="cy-title">Cygnus</span>
        <span className="cy-title-session">{sessionLabel}</span>
        <span className="cy-sp" data-tauri-drag-region />
        <button
          className="cy-tbtn"
          onClick={() => setRail(!railOpen)}
          aria-pressed={railOpen}
          title="스레드 목록"
          aria-label="스레드 목록"
        >
          <Icon name="history" size={14} />
        </button>
        <button
          className="cy-tbtn"
          onClick={newChat}
          disabled={busy || attaching}
          title="새 스레드"
          aria-label="새 스레드"
        >
          <Icon name="plus" size={14} />
        </button>
        <button className="cy-tbtn" onClick={toggleTheme} title="테마 전환" aria-label="테마 전환">
          {theme === "dark" ? "☾" : "☀"}
        </button>
        {/* Workspace window management, not just dismissal. Cygnus is
            frameless like the workstation, so without these there is no way to
            minimize or MAXIMIZE it at all — the window could only be summoned
            and hidden, which is what made it read as a launcher. Same shared
            cluster the workstation nav rail uses (close still hides to tray:
            the CloseRequested handler is app-wide). */}
        <WindowControls />
      </header>

      <div className="cy-body">
        {railOpen && (
          <>
            {!docked && <button className="cy-scrim" aria-label="스레드 목록 닫기" onClick={() => setRail(false)} />}
            <aside className="cy-rail">
              <SessionDrawer
                sessions={sessions}
                currentKey={sessionKey}
                busy={busy || attaching}
                error={sessionErr}
                onSelect={(key) => {
                  selectSession(key);
                  closeRailIfOverlay();
                }}
                onDelete={removeSession}
                onNew={() => {
                  newChat();
                  closeRailIfOverlay();
                }}
                onRename={(key, label) => void renameSession(key, label)}
                onPin={(key, pinned) => void pinConversation(key, pinned)}
                onResetModel={(key) => {
                  void resetConversationModel(key);
                  if (key === sessionKey) setModel("");
                }}
                onSearch={searchConversationHits}
                canLoadMore={canLoadMoreSessions}
                onLoadMore={() => void loadMoreSessions()}
              />
            </aside>
          </>
        )}

        <main className={"cy-main" + (dropOver ? " drop-over" : "")} {...dropProps}>
          <div
            className="cy-transcript"
            role="log"
            aria-live="polite"
            aria-label="Cygnus 에이전트 스레드"
            ref={transcriptRef}
            onScroll={onScroll}
          >
            {turns.length === 0 ? (
              <div className="cy-empty">
                <span className="cy-empty-mark" aria-hidden="true">
                  <DenebStar size={34} />
                </span>
                <h2>{connected ? "무엇을 시킬까요?" : "게이트웨이 연결 대기 중"}</h2>
                <span className="cy-empty-sub">
                  {connected ? "에이전트 실행과 가벼운 코딩" : "Andromeda 본창에서 연결하면 이 창도 따라옵니다"}
                </span>
                <span className="cy-empty-hint">Ctrl+Shift+Space 어디서든 소환</span>
              </div>
            ) : (
              <>
                {hiddenHistory && (
                  <button className="btn history-more" onClick={() => void loadOlderTurns()}>
                    이전 대화 {hiddenHistory.count}개 더 불러오기
                  </button>
                )}
                {turns.map((turn) => {
                  const sub = turn.role === "user" ? parseUiSubmission(turn.text) : null;
                  return (
                    <div key={turn.id} className={`ai-turn ${turn.role} ${turn.status}`}>
                      {/* Quiet identity: the answer is marked by the star, the
                          request by its right-aligned bubble — no wordmark
                          repeated on every turn. */}
                      {turn.role === "assistant" && (
                        <div className="ai-turn-label" aria-label="Cygnus 응답">
                          <DenebStar size={12} />
                        </div>
                      )}
                      {turn.role === "user" ? (
                        <div className="ai-turn-body">
                          {turn.imageUrl && <img className="ai-turn-image" src={turn.imageUrl} alt="첨부 이미지" />}
                          {sub ? <UiSubmissionBubble sub={sub} /> : turn.text}
                        </div>
                      ) : (
                        <AssistantBody
                          turn={turn}
                          thinking={thinking}
                          onUiSubmit={submit}
                          busy={busy}
                          interactive={turn.id === lastAssistantId}
                        />
                      )}
                      <div className="ai-turn-actions">
                        <AssistantTurnActions
                          turn={turn}
                          lastId={lastId}
                          busy={busy}
                          onRegenerate={regenerate}
                          variants={variants}
                          onVariant={selectVariant}
                        />
                      </div>
                    </div>
                  );
                })}
              </>
            )}
          </div>

          <ScrollToBottomButton visible={!atBottom && turns.length > 0} onClick={scrollToBottom} />
          {/* Suggestions belong to the input, not to the void: they sit right
              above the composer and align with its card. */}
          {connected && turns.length === 0 && (
            <div className="cy-starters" role="group" aria-label="시작 제안">
              {CYGNUS_STARTERS.map((s) => (
                <button
                  key={s}
                  className="cy-starter"
                  onClick={() => {
                    setInput(s);
                    composeRef.current?.focus();
                  }}
                >
                  {s}
                </button>
              ))}
            </div>
          )}
          <div className="cy-foot">
            <ChatComposer
              composeRef={composeRef}
              fileRef={fileRef}
              busy={busy}
              stoppable={stoppable}
              connected={connected}
              input={input}
              placeholder="에이전트에게 시킬 일…"
              note={attachNote}
              onInput={setInput}
              onSubmit={submit}
              onStop={stop}
              onPick={onPick}
              onAttachFiles={attachFiles}
              staged={staged}
              onRemoveStaged={removeStaged}
            />
            <div className="cy-status">
              <LiveDot connected={connected} pulse />
              <span>{connected ? "연결됨" : "미연결"}</span>
              <ModelPicker models={models} value={model} onChange={setModel} disabled={busy} />
              <span className="cy-sp" />
              {/* A keyboard-driven surface states its keys where they apply —
                  send/newline while composing, stop while a turn runs. */}
              <span className="cy-keys" aria-hidden="true">
                {busy && stoppable ? (
                  <>
                    <kbd>Esc</kbd> 중단
                  </>
                ) : (
                  <>
                    <kbd>⏎</kbd> 전송 <kbd>⇧⏎</kbd> 줄바꿈
                  </>
                )}
              </span>
            </div>
          </div>
        </main>
      </div>
    </div>
  );
}
