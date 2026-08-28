import { useEffect, useMemo, useRef, useState } from "react";

import { DataProviderScope } from "@/crud";
import { denebDataProvider } from "@/dataProvider";
import { type GatewayConfig, loadConfig, saveConfig } from "@/gateway";
import { useChat } from "@/hooks";
import { parseUiSubmission } from "@/markdown/denebUiParse";
import { getString, setString } from "@/storage";
import { isTauri, readDesktopToken } from "@/tauri";
import { useAttachPipeline, useComposerBehavior, useModels, useSessionDraft } from "@/useChatSurface";
import { useFileDrop } from "@/useFileDrop";
import { useSessions } from "@/useSessions";
import { useStickyScroll } from "@/useStickyScroll";
import { AssistantBody, AssistantTurnActions } from "@/components/AssistantBody";
import { ChatComposer, ScrollToBottomButton } from "@/components/ChatComposer";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { Icon } from "@/components/Icon";
import { LiveDot } from "@/components/LiveDot";
import { ModelPicker } from "@/components/ModelPicker";
import { SessionDrawer } from "@/components/SessionDrawer";
import { UiSubmissionBubble } from "@/components/UiSubmission";
import "./cygnus.css";

// Cygnus — the agent companion window. Andromeda stays the 업무비서 workstation;
// this summonable surface is for DRIVING the agent: kicking off tasks, light
// coding runs, watching tool calls stream. Same gateway, same shared chat
// modules (useChat/AssistantBody/ChatComposer — the parity rule), its own
// session namespace (client:cygnus:*) and its own dark-first token skin
// (cygnus.css re-values the workstation token names under .cygnus-root).
const CYGNUS_MAIN = "client:cygnus:main";
const THEME_KEY = "cygnus.theme";

// Hide (not close) the companion window — the tray/global shortcut re-summons.
async function hideSelf(): Promise<void> {
  if (!isTauri()) return;
  const { getCurrentWindow } = await import("@tauri-apps/api/window");
  await getCurrentWindow().hide();
}

export function CygnusApp() {
  // Same config bootstrap as the workstation App: shared localStorage + desktop
  // keychain/token-file auto-fill, and the same DataProviderScope — useChat's
  // post-turn cache invalidation reaches through it, so the scope is
  // load-bearing here even though Cygnus renders no CRUD panes.
  const [cfg, setCfg] = useState<GatewayConfig>(loadConfig());
  const dataProvider = useMemo(() => denebDataProvider(cfg), [cfg]);
  const connected = Boolean(cfg.url && cfg.token);
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
  const [theme, setTheme] = useState(() => (getString(THEME_KEY) === "light" ? "light" : "dark"));
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
  } = useChat(cfg);
  const [input, setInput] = useState("");
  const [attaching, setAttaching] = useState(false);
  const [railOpen, setRailOpen] = useState(false);
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
    { clear, setTurns },
    {
      mainKey: CYGNUS_MAIN,
      channel: "client",
      filter: "client:cygnus:",
      newKey: () => `client:cygnus:${crypto.randomUUID()}`,
      // Own restore slot — sharing the 채팅 탭's slot made either surface boot
      // into the other's conversation (and load its transcript).
      lastKeyStore: "cygnus.chat.lastSession",
    },
  );
  const { clearDraft } = useSessionDraft(sessionKey, input, setInput);
  const sessionModel = sessions.find((s) => s.key === sessionKey)?.model ?? "";
  const { models, model, setModel } = useModels(cfg, connected, sessionKey, sessionModel);
  const { ref: transcriptRef, onScroll, pin, atBottom, scrollToBottom } = useStickyScroll([turns, thinking]);
  useComposerBehavior(composeRef, { input, busy, hidden: false, focusOnReveal: true });

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
        <span className={"cy-orb" + (busy ? " busy" : "")} aria-hidden="true" />
        <span className="cy-title">Cygnus</span>
        <span className="cy-title-session">{sessionLabel}</span>
        <span className="cy-sp" data-tauri-drag-region />
        <button
          className="cy-tbtn"
          onClick={() => setRailOpen((v) => !v)}
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
        {isTauri() && (
          <button className="cy-tbtn" onClick={() => void hideSelf()} title="트레이로 접기" aria-label="트레이로 접기">
            ⌄
          </button>
        )}
      </header>

      <div className="cy-body">
        {railOpen && (
          <>
            <button className="cy-scrim" aria-label="스레드 목록 닫기" onClick={() => setRailOpen(false)} />
            <aside className="cy-rail">
              <SessionDrawer
                sessions={sessions}
                currentKey={sessionKey}
                busy={busy || attaching}
                error={sessionErr}
                onSelect={(key) => {
                  selectSession(key);
                  setRailOpen(false);
                }}
                onDelete={removeSession}
                onNew={() => {
                  newChat();
                  setRailOpen(false);
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
                <span className="cy-orb cy-orb-hero" aria-hidden="true" />
                <p>{connected ? "무엇을 시킬까요?" : "게이트웨이 연결 대기 중"}</p>
                <span className="cy-empty-sub">
                  {connected
                    ? "에이전트 실행과 가벼운 코딩을 여기서 — 파일을 끌어놓거나 명령을 적으세요"
                    : "Andromeda 본창에서 게이트웨이를 연결하면 이 창도 함께 연결됩니다"}
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
                      <div className="ai-turn-label">{turn.role === "user" ? "나" : "Cygnus"}</div>
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
              <span>
                스레드 <b>{sessions.length}</b>
              </span>
            </div>
          </div>
        </main>
      </div>
    </div>
  );
}
