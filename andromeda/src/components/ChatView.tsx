import { type ChangeEvent, useEffect, useRef, useState } from "react";

import { type GatewayConfig, type ModelsList, listModels } from "@/gateway";
import { useChat } from "@/hooks";
import { useSessions } from "@/useSessions";
import { useStickyScroll } from "@/useStickyScroll";
import { useWorkspace } from "@/workspaceContext";
import { AssistantBody } from "./AIPanel";
import { DenebStar } from "./DenebStar";
import { Icon } from "./Icon";
import { LiveDot } from "./LiveDot";
import { ModelPicker } from "./ModelPicker";
import { SessionDrawer } from "./SessionDrawer";

// 채팅 탭 — 비업무용(non-work) 전용 대화 surface (네이티브 챗봇 모드 대응). 측면 데네브
// 패널(업무 · client:main, 활성 pane 컨텍스트를 밀어넣음)과 달리 자체 useChat + chat:*
// 세션을 가지며, 워크스페이스 컨텍스트를 보내지 않는 순수 대화다. 레이아웃은 중앙 채팅
// 컬럼(가독성을 위해 메시지를 좁게 가운데 정렬) + 우측 세션 목록.
export function ChatView({ cfg, hidden = false }: { cfg: GatewayConfig; hidden?: boolean }) {
  const { connected } = useWorkspace();
  const { thinking, busy, turns, send, capture, stop, regenerate, clear, setTurns } = useChat(cfg);
  const [input, setInput] = useState("");
  const composeRef = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const [models, setModels] = useState<ModelsList | null>(null);
  const [model, setModel] = useState("");
  // chat:* 네임스페이스로 스코프 — 업무 패널의 client:main 세션과 섞이지 않는다.
  const { sessions, sessionKey, sessionErr, selectSession, removeSession, newChat, refreshSessions } = useSessions(
    cfg,
    connected,
    busy,
    { clear, setTurns },
    {
      mainKey: "chat:main",
      filter: "chat:",
      // 새 대화 → 고유 chat:<id> 발급(비업무 대화를 여러 개 유지). Date.now/random은 앱 런타임이라 사용 가능.
      newKey: () => `chat:${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`,
    },
  );
  const { ref: transcriptRef, onScroll, pin, atBottom, scrollToBottom } = useStickyScroll([turns, thinking]);

  useEffect(() => {
    if (!connected) {
      setModels(null);
      return;
    }
    let cancelled = false;
    void listModels(cfg)
      .then((m) => {
        if (cancelled) return;
        setModels(m);
        setModel((prev) => prev || m.current || "");
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, cfg.url, cfg.token]);

  // Re-measure on reveal too: the tab stays mounted while hidden (display:none → the
  // textarea measures 0 height), so without `hidden` here it would open collapsed.
  useEffect(() => {
    const el = composeRef.current;
    if (!el || hidden) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, [input, hidden]);

  // Focus the composer when the tab is revealed, so you can type right away.
  useEffect(() => {
    if (!hidden) composeRef.current?.focus();
  }, [hidden]);

  // Non-work: no workspaceContext / activeResource — a pure conversation, scoped to
  // its own chat:* session.
  function submit(message = input) {
    const msg = message.trim();
    if (!msg || busy || !connected) return;
    setInput("");
    pin();
    // refresh the history once the turn finishes — the gateway may have created or
    // relabelled this chat:* session.
    void send(msg, { model: model || undefined, sessionKey }).then(() => void refreshSessions());
  }

  // 첨부: 파일을 base64로 읽어 capture(이미지 OCR·음성 전사·문서 추출)로 보낸다.
  function onPick(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    e.target.value = ""; // let the same file be picked again later
    if (!file || busy || !connected) return;
    const caption = file.type.startsWith("audio/") ? "" : input.trim();
    if (caption) setInput("");
    const reader = new FileReader();
    reader.onload = () => {
      const base64 = String(reader.result).split(",")[1] ?? "";
      pin();
      void capture({ name: file.name, mimeType: file.type, base64 }, { sessionKey, caption }).then(
        () => void refreshSessions(),
      );
    };
    reader.readAsDataURL(file);
  }

  const last = turns.at(-1);
  const lastId = last?.id;

  return (
    <section className="chat-view" style={{ display: hidden ? "none" : "flex" }}>
      <main className="panel chat-main">
        <div className="ai-head">
          <span className="micro">Deneb · 채팅</span>
          <ModelPicker models={models} value={model} onChange={setModel} disabled={busy} />
          <button
            className="row-btn"
            onClick={newChat}
            disabled={busy}
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
              <p>{connected ? "안녕하세요? 무슨 대화를 할까요?" : "게이트웨이 연결 대기 중"}</p>
              {connected && <span className="chat-greeting-sub">무엇이든 편하게 물어보세요</span>}
            </div>
          ) : (
            turns.map((turn) => (
              <div key={turn.id} className={`ai-turn ${turn.role} ${turn.status}`}>
                <div className="ai-turn-label">{turn.role === "user" ? "나" : "Deneb"}</div>
                {turn.role === "user" ? (
                  <div className="ai-turn-body">{turn.text}</div>
                ) : (
                  <AssistantBody turn={turn} thinking={thinking} onUiSubmit={submit} busy={busy} />
                )}
                {turn.role === "assistant" &&
                  turn.id === lastId &&
                  turn.parts &&
                  turn.canRegenerate !== false &&
                  !busy &&
                  turn.status !== "streaming" && (
                    <button className="row-btn ai-regen" onClick={regenerate} title="다시 생성">
                      <Icon name="refresh" size={12} /> 다시 생성
                    </button>
                  )}
              </div>
            ))
          )}
        </div>

        {!atBottom && turns.length > 0 && (
          <button
            type="button"
            className="chat-scroll-bottom"
            onClick={scrollToBottom}
            aria-label="맨 아래로"
            title="맨 아래로"
          >
            <Icon name="chevron-down" size={18} />
          </button>
        )}
        <form
          className="ai-composer"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <input
            ref={fileRef}
            type="file"
            accept="image/*,audio/*,.pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.csv,.txt"
            hidden
            onChange={onPick}
          />
          <button
            type="button"
            className="row-btn"
            onClick={() => fileRef.current?.click()}
            disabled={busy || !connected}
            title="파일 첨부 (이미지·문서·녹음)"
            aria-label="파일 첨부"
            style={{ padding: 5, alignSelf: "flex-end" }}
          >
            <Icon name="attach" size={18} />
          </button>
          <textarea
            ref={composeRef}
            className="ai-compose"
            aria-label="Deneb에게 메시지"
            placeholder={busy ? "응답 중…" : "질문을 입력하세요"}
            rows={1}
            value={input}
            disabled={busy}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key !== "Enter" || e.shiftKey || e.nativeEvent.isComposing) return;
              e.preventDefault();
              submit();
            }}
          />
          {busy ? (
            <button type="button" className="ai-send ai-send-stop" onClick={stop} aria-label="중단" title="응답 중단">
              <Icon name="stop" size={15} />
            </button>
          ) : (
            <button
              type="submit"
              className="ai-send"
              disabled={!connected || input.trim().length === 0}
              aria-label="전송"
            >
              <Icon name="send" size={16} />
            </button>
          )}
        </form>
      </main>

      <aside className="panel chat-sessions">
        <SessionDrawer
          sessions={sessions}
          currentKey={sessionKey}
          busy={busy}
          error={sessionErr}
          onSelect={selectSession}
          onDelete={removeSession}
          onNew={newChat}
        />
      </aside>
    </section>
  );
}
