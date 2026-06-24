import { useEffect, useRef, useState } from "react";
import { type GatewayConfig, type ModelsList, listModels } from "@/gateway";
import { type ChatTurn, useChat } from "@/hooks";
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
export function AIPanel({ cfg, hidden = false }: { cfg: GatewayConfig; hidden?: boolean }) {
  const { aiText, activeResource, connected } = useWorkspace();
  const { thinking, busy, turns, send, stop, regenerate, clear, setTurns } = useChat(cfg);
  const [input, setInput] = useState("");
  const composeRef = useRef<HTMLTextAreaElement>(null);
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

  const last = turns.at(-1);
  const lastId = last?.id;

  return (
    <aside
      className="panel"
      style={{
        width: "var(--ai-w)",
        flex: "0 0 auto",
        display: hidden ? "none" : "flex",
        flexDirection: "column",
        padding: "16px 16px",
      }}
    >
      <div className="ai-head">
        <span className="micro">Deneb AI</span>
        <ModelPicker models={models} value={model} onChange={setModel} disabled={busy} />
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
                !busy &&
                turn.status !== "streaming" && (
                  <button className="row-btn ai-regen" onClick={regenerate} title="다시 생성">
                    <Icon name="refresh" size={12} /> 다시 생성
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
