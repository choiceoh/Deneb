import { useRef, useState } from "react";

import { type GatewayConfig } from "@/gateway";
import { useChat } from "@/hooks";
import { parseUiSubmission } from "@/markdown/denebUiParse";
import { useAttachPipeline, useComposerBehavior, useModels } from "@/useChatSurface";
import { useFileDrop } from "@/useFileDrop";
import { useSessions } from "@/useSessions";
import { useStickyScroll } from "@/useStickyScroll";
import { useWorkspace } from "@/workspaceContext";
import { AssistantBody, AssistantTurnActions } from "./AssistantBody";
import { ChatComposer, ScrollToBottomButton } from "./ChatComposer";
import { DenebStar } from "./DenebStar";
import { EditResendModal } from "./EditResendModal";
import { Icon } from "./Icon";
import { LiveDot } from "./LiveDot";
import { ModelPicker } from "./ModelPicker";
import { SessionDrawer } from "./SessionDrawer";
import { UiSubmissionBubble } from "./UiSubmission";

// 채팅 탭 — 풀사이즈 업무 대화 surface. 측면 데네브 패널(활성 pane 컨텍스트를
// 밀어넣음)과 달리 자체 useChat + client:main:* 세션을 가지며, pane 컨텍스트는 보내지
// 않는다 — 서버가 업무 프로파일(위키·회상·비서 페르소나)을 그대로 적용한다. 모바일
// 업무 워크스페이스와 같은 세션 공간을 공유한다. 레이아웃은 중앙 채팅 컬럼(가독성을
// 위해 메시지를 좁게 가운데 정렬) + 우측 세션 목록. 컴포저·첨부·모델 로딩 등 두 챗
// surface 공통 동작은 useChatSurface/ChatComposer/AssistantBody 공유 모듈에서 온다.
export function ChatView({ cfg, hidden = false }: { cfg: GatewayConfig; hidden?: boolean }) {
  const { connected } = useWorkspace();
  const {
    thinking,
    busy,
    stoppable,
    turns,
    send,
    captureBatch,
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
  const composeRef = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const { models, model, setModel } = useModels(cfg, connected);
  // 첨부 배치 진행 state — busy가 파일 읽기 틈에 잠깐 내려가는 동안에도 세션 전환/삭제/
  // 새 대화를 막는다 (useSessions 인자로 들어가야 해서 파이프라인 훅 밖에 산다).
  const [attaching, setAttaching] = useState(false);
  // 업무 네임스페이스(client:*)로 스코프 — 모바일 업무 드로어와 같은 세션 공간.
  const {
    sessions,
    sessionKey,
    sessionErr,
    hiddenHistory,
    selectSession,
    removeSession,
    renameSession,
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
      mainKey: "client:main",
      filter: "client:",
      // 새 대화 → 홈에서 분기한 고유 client:main:<id> 발급. Date.now/random은 앱 런타임이라 사용 가능.
      newKey: () => `client:main:${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`,
    },
  );
  const { ref: transcriptRef, onScroll, pin, atBottom, scrollToBottom } = useStickyScroll([turns, thinking]);

  useComposerBehavior(composeRef, { input, busy, hidden, focusOnReveal: true });

  const { attachNote, attachingRef, attachFiles, onPick, staged, removeStaged, sendStaged } = useAttachPipeline({
    connected,
    busy,
    input,
    setInput,
    setAttaching,
    pin,
    captureBatch: (files, caption) => captureBatch(files, { sessionKey, caption }),
    // 배치가 끝나면 세션 목록을 한 번 갱신 — 게이트웨이가 세션을 만들거나 라벨을 바꿨을 수 있다.
    onBatchDone: () => void refreshSessions(),
  });

  // No workspaceContext / activeResource push (that is the side panel's job) —
  // the gateway applies the full 업무 profile (wiki/recall/persona) on its own.
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
    pin();
    // refresh the history once the turn finishes — the gateway may have created or
    // relabelled this session.
    void send(msg, { model: model || undefined, sessionKey }).then(() => void refreshSessions());
  }

  // 채팅 컬럼 전체가 무표시 드롭존 — 파일 드래그가 위에 있을 때만 살짝 표시(.drop-over).
  const { over: dropOver, dropProps } = useFileDrop(!busy && connected, (files) => void attachFiles(files));

  const lastId = turns.at(-1)?.id;
  // Only the newest answer's cards may talk back to the agent (native parity);
  // older cards stay explorable but their callbacks/inputs lock.
  const lastAssistantId = [...turns].reverse().find((t) => t.role === "assistant")?.id;

  return (
    <section className="chat-view" style={{ display: hidden ? "none" : "flex" }}>
      <main className={"panel chat-main" + (dropOver ? " drop-over" : "")} {...dropProps}>
        <div className="ai-head">
          <span className="micro">Deneb · 채팅</span>
          <ModelPicker models={models} value={model} onChange={setModel} disabled={busy} />
          <button
            className="row-btn"
            onClick={newChat}
            disabled={busy || attaching}
            title="새 대화"
            aria-label="새 대화"
            style={{ padding: 4, display: "inline-flex" }}
          >
            <Icon name="plus" size={16} />
          </button>
          <LiveDot connected={connected} pulse />
        </div>

        <div
          className="ai-transcript chat-transcript"
          role="log"
          aria-live="polite"
          aria-label="Deneb 채팅"
          ref={transcriptRef}
          onScroll={onScroll}
        >
          {turns.length === 0 ? (
            <div className="chat-greeting">
              <DenebStar size={40} />
              <p>{connected ? timeOfDayGreeting() : "게이트웨이 연결 대기 중"}</p>
              {connected && <span className="chat-greeting-sub">무엇이든 편하게 물어보세요</span>}
            </div>
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
                    <div className="ai-turn-actions">
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
                      />
                    </div>
                  </div>
                );
              })}
            </>
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
          placeholder="질문을 입력하세요"
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
      </main>

      <aside className="panel chat-sessions">
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
      </aside>
    </section>
  );
}

// Personalized time-of-day greeting for the empty chat — mirrors the native
// client's 업무 EmptyState so the two surfaces read as one assistant.
function timeOfDayGreeting(): string {
  const h = new Date().getHours();
  if (h >= 5 && h <= 10) return "선택님, 좋은 아침이에요";
  if (h >= 11 && h <= 16) return "선택님, 좋은 오후예요";
  if (h >= 17 && h <= 21) return "선택님, 좋은 저녁이에요";
  return "선택님, 늦은 시간까지 고생 많으세요";
}
