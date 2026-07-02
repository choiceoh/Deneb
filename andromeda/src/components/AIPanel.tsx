import { type ChangeEvent, useEffect, useRef, useState } from "react";
import { inferAttachmentMimeType } from "@/attachmentMime";
import { type GatewayConfig, type ModelsList, listModels } from "@/gateway";
import { type AttachmentPart, type ChatTurn, useChat } from "@/hooks";
import { useFileDrop } from "@/useFileDrop";
import { useSessions } from "@/useSessions";
import { useStickyScroll } from "@/useStickyScroll";
import { useWorkspace } from "@/workspaceContext";
import { DenebStatus } from "./DenebStatus";
import { AssistantText } from "./DenebUi";
import { Icon } from "./Icon";
import { LiveDot } from "./LiveDot";
import { ModelPicker } from "./ModelPicker";
import { ProactivePanel } from "./ProactivePanel";
import { SessionDrawer } from "./SessionDrawer";
import { ToolChip } from "./ToolChip";

function attachmentKindLabel(kind: AttachmentPart["captureKind"]) {
  if (kind === "image") return "이미지 분석";
  if (kind === "audio") return "녹음 전사";
  return "문서 추출";
}

function AttachmentResult({
  part,
  onUiSubmit,
  busy,
}: {
  part: AttachmentPart;
  onUiSubmit: (msg: string) => void;
  busy: boolean;
}) {
  const stateText = part.isError ? "실패" : "완료";
  return (
    <section className={"attachment-result" + (part.isError ? " error" : "")} role="group" aria-label="첨부 분석 결과">
      <div className="attachment-result-head">
        <span className="attachment-result-icon" aria-hidden="true">
          <Icon name="attach" size={15} />
        </span>
        <div className="attachment-result-title">
          <span>{attachmentKindLabel(part.captureKind)}</span>
          <strong>{part.filename}</strong>
        </div>
        <span className="attachment-result-state">{stateText}</span>
      </div>
      <div className="attachment-result-meta">
        <span>형식</span>
        <b>{part.mimeType}</b>
        {part.caption ? (
          <>
            <span>설명</span>
            <b>{part.caption}</b>
          </>
        ) : null}
      </div>
      <div className="attachment-result-content">
        <AssistantText text={part.text} onUiSubmit={onUiSubmit} busy={busy} />
      </div>
    </section>
  );
}

// One assistant reply: ordered text and tool chips. Each text span renders as
// Markdown, with any ```deneb-ui block drawn as interactive UI (AssistantText);
// transcript-loaded / pre-stream turns with no parts use the plain body.
export function AssistantBody({
  turn,
  thinking,
  onUiSubmit,
  busy,
}: {
  turn: ChatTurn;
  thinking?: string;
  onUiSubmit: (msg: string) => void;
  busy: boolean;
}) {
  const parts = turn.parts;
  if (!parts || parts.length === 0) {
    // Pre-content stream → Deneb's "응답 중" sparkle, with the gateway's thinking
    // preview as its inline summary (mirrors the native PulsingStatusIndicator).
    if (turn.status === "streaming") return <DenebStatus summary={thinking?.trim() ? thinking : undefined} />;
    return (
      <div className="ai-turn-body">
        <AssistantText text={turn.text || ""} onUiSubmit={onUiSubmit} busy={busy} />
      </div>
    );
  }
  return (
    <div className="ai-turn-body">
      {parts.map((p, i) =>
        p.kind === "text" ? (
          <AssistantText key={i} text={p.text} onUiSubmit={onUiSubmit} busy={busy} />
        ) : p.kind === "attachment" ? (
          <AttachmentResult key={p.id || i} part={p} onUiSubmit={onUiSubmit} busy={busy} />
        ) : (
          <ToolChip key={p.id || i} part={p} />
        ),
      )}
    </div>
  );
}

// Right floating panel: Deneb AI collaboration. Reads the active pane's pushed
// text from the workspace context and streams a reply with Markdown + tool
// chips; a model picker drives the per-turn model and a history drawer switches
// conversations. Tool calls that mutate data refresh the active grid (useChat).
// Files attach via the same capture path as the chat tab (image OCR · audio
// transcription · document extraction), landing in this panel's session.
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
  const { aiText, activeResource, connected, noteSink } = useWorkspace();
  const { thinking, busy, turns, send, capture, stop, regenerate, clear, setTurns } = useChat(cfg);
  const [input, setInput] = useState("");
  // Answers already saved into the open notebook this session (turn ids) — flips
  // the per-answer 노트에 저장 button to a done state so a double-click can't pin
  // the same answer twice.
  const [savedNoteIds, setSavedNoteIds] = useState<ReadonlySet<string>>(new Set());
  const composeRef = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const [models, setModels] = useState<ModelsList | null>(null);
  const [model, setModel] = useState(""); // selected override id ("" → gateway main)
  const { sessions, sessionKey, sessionsOpen, sessionErr, toggleSessions, selectSession, removeSession, newChat } =
    useSessions(cfg, connected, busy, { clear, setTurns });
  // Follow the newest message while it streams, unless the user scrolled up to read.
  const { ref: transcriptRef, onScroll, pin } = useStickyScroll([turns, thinking]);

  // Load the model registry once connected; best-effort (older gateway / the offline
  // test path just leaves it empty).
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

  // Grow the composer from one line up to its CSS max-height, then it scrolls.
  useEffect(() => {
    const el = composeRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, [input]);

  function submit(message = input) {
    const msg = message.trim();
    if (!msg || busy || !connected) return;
    setInput("");
    pin(); // a fresh send always rides down to the latest
    void send(msg, { workspaceContext: aiText, activeResource, model: model || undefined, sessionKey });
  }

  // 첨부: 파일을 base64로 읽어 capture(이미지 OCR·음성 전사·문서 추출)로 보낸다 — 채팅 탭과
  // 같은 경로, 이 패널의 세션(client:main)에 한 턴으로 남는다. 클립 버튼과 드롭 공용.
  function attachFile(file: File) {
    if (busy || !connected) return;
    const mimeType = inferAttachmentMimeType(file.name, file.type);
    const caption = mimeType.startsWith("audio/") ? "" : input.trim();
    if (caption) setInput("");
    const reader = new FileReader();
    reader.onload = () => {
      const base64 = String(reader.result).split(",")[1] ?? "";
      pin();
      void capture({ name: file.name, mimeType, base64 }, { sessionKey, caption });
    };
    reader.readAsDataURL(file);
  }

  function onPick(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    e.target.value = ""; // let the same file be picked again later
    if (file) attachFile(file);
  }

  // 패널 전체가 무표시 드롭존 — 파일 드래그가 위에 있을 때만 살짝 표시(.drop-over).
  const { over: dropOver, dropProps } = useFileDrop(!busy && connected, attachFile);

  const last = turns.at(-1);
  const lastId = last?.id;

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
              minWidth: 0,
              minHeight: 0,
              display: hidden ? "none" : "flex",
              flexDirection: "column",
              padding: "12px 16px",
            }
          : {
              // 확대 시 사이드바를 제외한 전 폭(flex:1), 평시엔 고정 폭(--ai-w).
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
          busy={busy}
          error={sessionErr}
          onSelect={selectSession}
          onDelete={removeSession}
          onNew={newChat}
        />
      )}

      <ProactivePanel cfg={cfg} />

      <div
        className="ai-transcript"
        role="log"
        aria-live="polite"
        aria-label="Deneb 대화"
        ref={transcriptRef}
        onScroll={onScroll}
      >
        {turns.length === 0 ? (
          connected ? null : (
            <div className="ai-empty">게이트웨이 연결 대기 중</div>
          )
        ) : (
          turns.map((turn) => (
            <div key={turn.id} className={`ai-turn ${turn.role} ${turn.status}`}>
              <div className="ai-turn-label">{turn.role === "user" ? "나" : "Deneb"}</div>
              {turn.role === "user" ? (
                <div className="ai-turn-body">{turn.text}</div>
              ) : (
                <AssistantBody turn={turn} thinking={thinking} onUiSubmit={submit} busy={busy} />
              )}
              {/* Regenerate only the last streamed reply (transcript-loaded turns have no parts). */}
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
              {/* Save this answer into the open notebook as a cited note — shown only
                  while a notebook pane has registered a sink (the notebook's output
                  loop: material made with the AI stays with the deal). */}
              {noteSink && turn.role === "assistant" && turn.status === "done" && turn.text.trim() && (
                <button
                  className="row-btn ai-save-note"
                  disabled={savedNoteIds.has(turn.id)}
                  onClick={() => {
                    noteSink(turn.text);
                    setSavedNoteIds((prev) => new Set(prev).add(turn.id));
                  }}
                  title="이 답변을 노트북에 인용자료(노트)로 저장"
                >
                  <Icon name="plus" size={12} /> {savedNoteIds.has(turn.id) ? "노트로 저장됨" : "노트에 저장"}
                </button>
              )}
            </div>
          ))
        )}
        {/* Once content has started streaming, a mid-turn thinking burst (between
            tools) shows here; before the first token it rides in the sparkle above. */}
        {thinking && last?.role === "assistant" && last.status === "streaming" && (last.parts?.length ?? 0) > 0 && (
          <div className="ai-thinking">{thinking}…</div>
        )}
      </div>

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
          accept="image/*,audio/*,.png,.jpg,.jpeg,.webp,.gif,.mp3,.m4a,.wav,.ogg,.webm,.pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.csv,.txt"
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
          placeholder={busy ? "응답 중…" : "메시지…"}
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
    </aside>
  );
}
