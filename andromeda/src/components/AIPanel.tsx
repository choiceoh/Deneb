import { useEffect, useRef, useState } from "react";

import { type GatewayConfig } from "@/gateway";
import { useChat } from "@/hooks";
import { parseUiSubmission } from "@/markdown/denebUiParse";
import { useAttachPipeline, useComposerBehavior, useModels } from "@/useChatSurface";
import { useFileDrop } from "@/useFileDrop";
import { useSessions } from "@/useSessions";
import { useStickyScroll } from "@/useStickyScroll";
import { useAiFeed, useWorkspace } from "@/workspaceContext";
import { AssistantBody, AssistantTurnActions } from "./AssistantBody";
import { ChatComposer, ScrollToBottomButton } from "./ChatComposer";
import { EditResendModal } from "./EditResendModal";
import { Icon } from "./Icon";
import { LiveDot } from "./LiveDot";
import { ModelPicker } from "./ModelPicker";
import { ContextFollow } from "./ContextFollow";
import { ProactivePanel } from "./ProactivePanel";
import { SessionDrawer } from "./SessionDrawer";
import { UiSubmissionBubble } from "./UiSubmission";

// Right floating panel: Deneb AI collaboration. Reads the active pane's pushed
// text from the workspace context and streams a reply with Markdown + tool
// chips; a model picker drives the per-turn model and a history drawer switches
// conversations. Tool calls that mutate data refresh the active grid (useChat).
// Files attach via the same capture path as the chat tab (image OCR · audio
// transcription · document extraction), landing in this panel's session.
// 컴포저·첨부·모델 로딩 등 두 챗 surface 공통 동작은 useChatSurface/ChatComposer/
// AssistantBody 공유 모듈에서 온다.
export function AIPanel({
  cfg,
  hidden = false,
  expanded = false,
  onToggleExpand,
  onCollapse,
  placement = "side",
}: {
  cfg: GatewayConfig;
  hidden?: boolean;
  // 중앙 작업 영역까지 패널을 넓힌 상태인지(Workstation이 소유). true면 작업 pane이 숨겨지고
  // 이 패널이 사이드바를 제외한 전 폭을 차지한다.
  expanded?: boolean;
  onToggleExpand?: () => void;
  // 패널 접기(완전히 숨김) — 위키처럼 본문을 넓게 보고 싶을 때. Workstation이 우측 가장자리에
  // 다시-열기 탭을 그린다. 노트북 하단 모드에선 넘기지 않는다.
  onCollapse?: () => void;
  // "side"(기본, 우측 고정폭) | "bottom"(노트북 등에서 하단 도킹). bottom일 땐 크기를 그리드
  // 셀이 정하므로 width/flex를 지정하지 않고, 넓어진 만큼 대화 폭을 가독성 있게 가운데 정렬한다.
  placement?: "side" | "bottom";
}) {
  const { connected, noteSink, setAskSink, followMode } = useWorkspace();
  const { aiText, activeResource } = useAiFeed();
  const {
    thinking,
    busy,
    stoppable,
    turns,
    send,
    capture,
    stop,
    regenerate,
    editResend,
    variants,
    selectVariant,
    clear,
    setTurns,
  } = useChat(cfg);
  // 마지막 사용자 메시지만 편집-재전송 가능 (마지막 교환 대체 시맨틱).
  const [editingMsg, setEditingMsg] = useState<string | null>(null);
  const lastUserId = [...turns].reverse().find((t) => t.role === "user")?.id;
  const [input, setInput] = useState("");
  // Per-answer save-to-notebook progress (turn id → state). "saved" flips the
  // button to a done state so a double-click can't pin the same answer twice;
  // "error" keeps it clickable — the RPC failed, nothing was pinned, retry works.
  const [noteSaves, setNoteSaves] = useState<ReadonlyMap<string, "saving" | "saved" | "error">>(new Map());
  // A different sink = a different target notebook (or none) — the 저장됨 marks
  // belong to the previous target, so saving the same answer to a NEW notebook
  // must start fresh. Adjusted during render (react.dev adjust-state pattern).
  // noteSink is a function, so it must be wrapped — a bare useState(noteSink) /
  // setState(noteSink) would invoke it as initializer/updater.
  const [prevNoteSink, setPrevNoteSink] = useState(() => noteSink);
  if (prevNoteSink !== noteSink) {
    setPrevNoteSink(() => noteSink);
    setNoteSaves(new Map());
  }
  const composeRef = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const { models, model, setModel } = useModels(cfg, connected);
  // 첨부 배치 진행 state — busy가 파일 읽기 틈에 잠깐 내려가는 동안에도 세션 전환/삭제/
  // 새 대화를 막는다 (useSessions 인자로 들어가야 해서 파이프라인 훅 밖에 산다).
  const [attaching, setAttaching] = useState(false);
  const {
    sessions,
    sessionKey,
    sessionsOpen,
    sessionErr,
    hiddenHistory,
    toggleSessions,
    selectSession,
    removeSession,
    renameSession,
    canLoadMoreSessions,
    loadMoreSessions,
    newChat,
    loadOlderTurns,
  } = useSessions(cfg, connected, busy || attaching, { clear, setTurns });
  // Follow the newest message while it streams, unless the user scrolled up to read.
  const { ref: transcriptRef, onScroll, pin, atBottom, scrollToBottom } = useStickyScroll([turns, thinking]);

  // 사이드 패널은 focusOnReveal 없음 — 드러날 때 작업 영역의 포커스를 뺏으면 안 된다.
  useComposerBehavior(composeRef, { input, busy, hidden });

  const { attachNote, attachingRef, attachFiles, onPick, staged, removeStaged, sendStaged } = useAttachPipeline({
    connected,
    busy,
    input,
    setInput,
    setAttaching,
    pin,
    capture: (file, caption, previewUrl) => capture(file, { sessionKey, caption, previewUrl }),
  });

  function submit(message = input) {
    const msg = message.trim();
    if (busy || attachingRef.current || !connected) return;
    if (staged.length > 0) {
      // 스테이징된 파일이 있으면 이 전송이 배치를 나른다 — 텍스트는 첫 파일 캡션
      // (오디오만이면 텍스트는 남는다; 캡션 소비는 sendStaged가 소유).
      void sendStaged(msg);
      return;
    }
    if (!msg) return;
    setInput("");
    pin(); // a fresh send always rides down to the latest
    void send(msg, { workspaceContext: aiText, activeResource, model: model || undefined, sessionKey });
  }

  // 팔레트의 "데네브에게 묻기"가 이 패널로 프롬프트를 쏠 수 있게 submit을 등록한다.
  // 최신 클로저는 ref로 따라가고(커밋 후 갱신 — 렌더 중 ref 대입 금지 규칙), 등록
  // 자체는 stable 래퍼 1회. 스트리밍 중(busy)이면 submit 가드가 그대로 무시한다.
  const submitRef = useRef(submit);
  useEffect(() => {
    submitRef.current = submit;
  });
  useEffect(() => {
    setAskSink((text) => submitRef.current(text));
    return () => setAskSink(null);
  }, [setAskSink]);

  // 노트에 저장: 성공해야만 저장됨으로 표시한다 — sink(RPC)가 실패하면 실패 상태로
  // 남겨 같은 버튼으로 재시도할 수 있다 (과거엔 발사 후 무조건 저장됨으로 위장했다).
  async function saveNote(turnId: string, text: string) {
    if (!noteSink) return;
    setNoteSaves((prev) => new Map(prev).set(turnId, "saving"));
    let ok = false;
    try {
      ok = await noteSink(text);
    } catch {
      ok = false;
    }
    setNoteSaves((prev) => new Map(prev).set(turnId, ok ? "saved" : "error"));
  }

  // 패널 전체가 무표시 드롭존 — 파일 드래그가 위에 있을 때만 살짝 표시(.drop-over).
  const { over: dropOver, dropProps } = useFileDrop(!busy && connected, (files) => void attachFiles(files));

  const last = turns.at(-1);

  // 컨텍스트 팔로우 모드 ("말하면 화면이 따라온다") — 실행부는 토글이 켜졌을
  // 때만 마운트되는 ContextFollow가 소유한다 (엔티티 fetch도 그때만).
  const followTurn = last?.role === "assistant" && last.status === "done" ? last : null;
  const lastId = last?.id;
  // Only the newest answer's cards may talk back to the agent (native parity);
  // older cards stay explorable but their callbacks/inputs lock.
  const lastAssistantId = [...turns].reverse().find((t) => t.role === "assistant")?.id;

  const bottom = placement === "bottom";
  return (
    <aside
      className={
        "panel" + (expanded ? " ai-expanded" : "") + (bottom ? " ai-bottom" : "") + (dropOver ? " drop-over" : "")
      }
      {...dropProps}
      style={
        bottom
          ? // 하단 도킹: 크기는 그리드 셀이 결정. width/flex 미지정, 높이 넘침은 내부 transcript가 스크롤.
            {
              position: "relative", // 맨 아래로 버튼(.chat-scroll-bottom)의 앵커
              minWidth: 0,
              minHeight: 0,
              display: hidden ? "none" : "flex",
              flexDirection: "column",
              padding: "12px 16px",
            }
          : {
              // 확대 시 사이드바를 제외한 전 폭(flex:1), 평시엔 고정 폭(--ai-w).
              position: "relative", // 맨 아래로 버튼(.chat-scroll-bottom)의 앵커
              width: expanded ? "auto" : "var(--ai-w)",
              flex: expanded ? "1 1 auto" : "0 0 auto",
              minWidth: 0,
              display: hidden ? "none" : "flex",
              flexDirection: "column",
              padding: "16px 16px",
            }
      }
    >
      <div className="ai-head">
        <span className="micro">Deneb AI</span>
        <ModelPicker models={models} value={model} onChange={setModel} disabled={busy} />
        {onToggleExpand && (
          <button
            className={"row-btn" + (expanded ? " active" : "")}
            onClick={onToggleExpand}
            title={expanded ? "패널 좁히기" : "패널 넓히기"}
            aria-label={expanded ? "패널 좁히기" : "패널 넓히기"}
            aria-pressed={expanded}
            style={{ padding: 4, display: "inline-flex" }}
          >
            <Icon name={expanded ? "collapse-panel" : "expand-panel"} size={15} />
          </button>
        )}
        {onCollapse && !expanded && (
          <button
            className="row-btn"
            onClick={onCollapse}
            title="패널 접기"
            aria-label="Deneb 패널 접기"
            style={{ padding: 4, display: "inline-flex" }}
          >
            <Icon name="collapse-panel" size={15} />
          </button>
        )}
        <button
          className={"row-btn" + (sessionsOpen ? " active" : "")}
          onClick={toggleSessions}
          title="대화 기록"
          aria-label="대화 기록"
          style={{ padding: 4, display: "inline-flex" }}
        >
          <Icon name="history" size={15} />
        </button>
        <LiveDot connected={connected} pulse />
      </div>

      {sessionsOpen && (
        <SessionDrawer
          sessions={sessions}
          currentKey={sessionKey}
          busy={busy || attaching}
          error={sessionErr}
          onSelect={selectSession}
          onDelete={removeSession}
          onNew={newChat}
          onRename={(key, label) => void renameSession(key, label)}
          canLoadMore={canLoadMoreSessions}
          onLoadMore={() => void loadMoreSessions()}
        />
      )}

      <ProactivePanel cfg={cfg} />
      {followMode && connected && <ContextFollow turn={followTurn} />}

      <div
        className="ai-transcript"
        role="log"
        aria-live="polite"
        aria-label="Deneb 대화"
        ref={transcriptRef}
        onScroll={onScroll}
      >
        {turns.length === 0 ? (
          connected ? (
            // Designed empty: the side panel reads the focused pane's projection,
            // so pitch that instead of a blank column. Chips send immediately —
            // mouse-first affordance (the user doesn't use shortcuts).
            <div className="ai-empty-suggest">
              <p>지금 보고 있는 화면을 함께 봅니다</p>
              {["이 화면 요약해줘", "여기서 눈에 띄는 점은?", "오늘 뭐부터 하면 좋을까?"].map((s) => (
                <button key={s} className="btn suggest-chip" onClick={() => submit(s)} disabled={busy}>
                  {s}
                </button>
              ))}
            </div>
          ) : (
            <div className="ai-empty">게이트웨이 연결 대기 중</div>
          )
        ) : (
          <>
            {hiddenHistory && (
              <button className="btn history-more" onClick={() => void loadOlderTurns()}>
                이전 대화 {hiddenHistory.count}개 더 불러오기
              </button>
            )}
            {turns.map((turn) => {
              // A card answer round-trips as machine text — humanize it.
              const sub = turn.role === "user" ? parseUiSubmission(turn.text) : null;
              return (
                <div key={turn.id} className={`ai-turn ${turn.role} ${turn.status}`}>
                  <div className="ai-turn-label">{turn.role === "user" ? "나" : "Deneb"}</div>
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
                  {turn.role === "user" && turn.id === lastUserId && !busy && !sub && (
                    <button
                      className="row-btn ai-edit no-print"
                      onClick={() => setEditingMsg(turn.text)}
                      title="이 메시지를 수정해 다시 보내기"
                    >
                      <Icon name="pencil" size={12} /> 수정
                    </button>
                  )}
                  <AssistantTurnActions
                    turn={turn}
                    lastId={lastId}
                    busy={busy}
                    onRegenerate={regenerate}
                    variants={variants}
                    onVariant={selectVariant}
                  >
                    {/* Save this answer into the open notebook as a cited note — shown only
                    while a notebook pane has registered a sink (the notebook's output
                    loop: material made with the AI stays with the deal). 저장됨 only
                    after the sink confirms; a failed pin stays clickable for retry. */}
                    {noteSink && turn.status === "done" && turn.text.trim() && (
                      <button
                        className="row-btn ai-save-note no-print"
                        disabled={noteSaves.get(turn.id) === "saving" || noteSaves.get(turn.id) === "saved"}
                        onClick={() => void saveNote(turn.id, turn.text)}
                        title="이 답변을 노트북에 인용자료(노트)로 저장"
                      >
                        <Icon name="plus" size={12} />{" "}
                        {noteSaves.get(turn.id) === "saved"
                          ? "노트로 저장됨"
                          : noteSaves.get(turn.id) === "saving"
                            ? "저장 중…"
                            : noteSaves.get(turn.id) === "error"
                              ? "저장 실패 — 다시 시도"
                              : "노트에 저장"}
                      </button>
                    )}
                  </AssistantTurnActions>
                </div>
              );
            })}
          </>
        )}
        {/* Once content has started streaming, a mid-turn thinking burst (between
            tools) shows here; before the first token it rides in the sparkle above. */}
        {thinking && last?.role === "assistant" && last.status === "streaming" && (last.parts?.length ?? 0) > 0 && (
          <div className="ai-thinking">{thinking}…</div>
        )}
      </div>

      <ScrollToBottomButton visible={!atBottom && turns.length > 0} onClick={scrollToBottom} />
      <ChatComposer
        composeRef={composeRef}
        fileRef={fileRef}
        busy={busy}
        stoppable={stoppable}
        connected={connected}
        input={input}
        placeholder="메시지…"
        note={attachNote}
        onInput={setInput}
        onSubmit={submit}
        onStop={stop}
        onPick={onPick}
        onAttachFiles={attachFiles}
        staged={staged}
        onRemoveStaged={removeStaged}
      />
      {editingMsg !== null && (
        <EditResendModal initial={editingMsg} onClose={() => setEditingMsg(null)} onResend={editResend} />
      )}
    </aside>
  );
}
