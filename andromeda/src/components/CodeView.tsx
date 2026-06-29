import { useEffect, useRef, useState } from "react";

import { type GatewayConfig, type ModelsList, listModels, sessionTranscript } from "@/gateway";
import { useChat } from "@/hooks";
import { errText } from "@/format";
import { useStickyScroll } from "@/useStickyScroll";
import { useWorkspace } from "@/workspaceContext";
import { AssistantBody } from "./AIPanel";
import { CodePane } from "./panes/CodePane";
import { DenebStar } from "./DenebStar";
import { Icon } from "./Icon";
import { LiveDot } from "./LiveDot";
import { ModelPicker } from "./ModelPicker";

// CodeView — 코드 모드의 작업면. 일반 코딩 AI처럼 *중앙이 채팅(주작업)*, 우측이 보조(작업 관리).
// 중앙은 선택된 코딩 세션(activeCodeKey="code:<id>")과의 대화 — 여기서 코드를 시키면 게이트웨이가
// 그 워크트리를 편집하고 턴마다 검증·체크포인트를 남긴다. 우측 aside는 CodePane(새 작업·세션
// 목록·검증/올리기/되돌리기). ChatView와 같은 중앙-채팅 레이아웃(chat-view)을 재사용한다.
export function CodeView({ cfg, hidden = false }: { cfg: GatewayConfig; hidden?: boolean }) {
  const { connected, activeCodeKey } = useWorkspace();
  const { thinking, busy, turns, send, stop, regenerate, clear, setTurns } = useChat(cfg);
  const [input, setInput] = useState("");
  const composeRef = useRef<HTMLTextAreaElement>(null);
  const [models, setModels] = useState<ModelsList | null>(null);
  const [model, setModel] = useState("");
  const [loadErr, setLoadErr] = useState("");
  const { ref: transcriptRef, onScroll, pin } = useStickyScroll([turns, thinking]);

  // Load the selected coding session's transcript into the chat when it changes; clear
  // when nothing is selected. Switching tasks continues that worktree's turns.
  useEffect(() => {
    if (!activeCodeKey || !connected) {
      clear();
      setLoadErr("");
      return;
    }
    let cancelled = false;
    sessionTranscript(cfg, activeCodeKey)
      .then((msgs) => {
        if (cancelled) return;
        setTurns(
          msgs.map((m, i) => ({
            id: m.id || `tr-${activeCodeKey}-${i}`,
            role: m.role === "user" ? ("user" as const) : ("assistant" as const),
            text: m.content,
            status: "done" as const,
          })),
        );
        setLoadErr("");
      })
      .catch((e) => !cancelled && setLoadErr(errText(e)));
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeCodeKey, connected, cfg.url, cfg.token]);

  // Model registry — best-effort, same as ChatView/AIPanel.
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

  // Re-measure on reveal (stays mounted while hidden → textarea measures 0).
  useEffect(() => {
    const el = composeRef.current;
    if (!el || hidden) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, [input, hidden]);

  // Focus the composer when revealed with a task selected, so you can type right away.
  useEffect(() => {
    if (!hidden && activeCodeKey) composeRef.current?.focus();
  }, [hidden, activeCodeKey]);

  function submit(message = input) {
    const msg = message.trim();
    if (!msg || busy || !connected || !activeCodeKey) return;
    setInput("");
    pin();
    void send(msg, { model: model || undefined, sessionKey: activeCodeKey });
  }

  const last = turns.at(-1);
  const lastId = last?.id;
  const taskId = activeCodeKey?.startsWith("code:") ? activeCodeKey.slice(5) : null;

  return (
    <section className="chat-view code-view" style={{ display: hidden ? "none" : "flex" }}>
      <main className="panel chat-main">
        <div className="ai-head">
          <span className="micro">{taskId ? `Deneb · 코딩 · ${taskId}` : "Deneb · 코딩"}</span>
          <ModelPicker models={models} value={model} onChange={setModel} disabled={busy} />
          <LiveDot connected={connected} pulse />
        </div>

        <div
          className="ai-transcript chat-transcript"
          role="log"
          aria-live="polite"
          aria-label="코딩 대화"
          ref={transcriptRef}
          onScroll={onScroll}
        >
          {!activeCodeKey ? (
            <div className="chat-greeting">
              <DenebStar size={40} />
              <p>{connected ? "작업을 선택하거나 새로 만드세요" : "게이트웨이 연결 대기 중"}</p>
              {connected && (
                <span className="chat-greeting-sub">우측에서 새 작업을 만들면 여기서 코드를 시킬 수 있습니다</span>
              )}
            </div>
          ) : turns.length === 0 ? (
            <div className="chat-greeting">
              <DenebStar size={40} />
              <p>{loadErr || "무엇을 만들까요?"}</p>
              {!loadErr && <span className="chat-greeting-sub">예: "로그인 폼에 비밀번호 표시 토글 추가해줘"</span>}
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

        <form
          className="ai-composer"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <textarea
            ref={composeRef}
            className="ai-compose"
            aria-label="Deneb에게 코딩 작업 지시"
            placeholder={
              !activeCodeKey
                ? "먼저 왼쪽에서 작업을 선택하세요"
                : busy
                  ? "응답 중…"
                  : "무엇을 만들까요? (예: 로그인 폼 추가)"
            }
            rows={1}
            value={input}
            disabled={busy || !activeCodeKey}
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
              disabled={!connected || !activeCodeKey || input.trim().length === 0}
              aria-label="전송"
            >
              <Icon name="send" size={16} />
            </button>
          )}
        </form>
      </main>

      <aside className="panel chat-sessions code-aside" style={{ width: "var(--ai-w)" }}>
        <CodePane />
      </aside>
    </section>
  );
}
