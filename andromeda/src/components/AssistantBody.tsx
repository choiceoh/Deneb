import { useState, type ReactNode } from "react";

import type { AttachmentPart, ChatTurn, VariantView } from "@/hooks";
import { printClosest } from "@/print";
import { DenebStatus } from "./DenebStatus";
import { AssistantText } from "./DenebUi";
import { Icon } from "./Icon";
import { ToolChip } from "./ToolChip";

// Collapsible chain-of-thought shown above a completed answer (frontier-chatbot
// "추론 보기"). Native <details> so it needs no state and stays accessible; the
// gateway carries the reasoning on the done frame and in the transcript wire.
function ReasoningBlock({ text }: { text: string }) {
  return (
    // Styling lives in styles.css (.ai-reasoning) rather than inline so a
    // surface skin — the Cygnus companion — can quiet it without !important.
    <details className="ai-reasoning no-print">
      <summary>추론 보기</summary>
      <div className="ai-reasoning-body">{text}</div>
    </details>
  );
}

// Assistant-turn rendering shared by the two chat surfaces (chat tab · AI side
// panel) — previously lived inside AIPanel.tsx with ChatView cross-importing it.

function attachmentKindLabel(kind: AttachmentPart["captureKind"]) {
  if (kind === "image") return "이미지 분석";
  if (kind === "audio") return "녹음 전사";
  return "문서 추출";
}

function AttachmentResult({
  part,
  onUiSubmit,
  busy,
  interactive = true,
}: {
  part: AttachmentPart;
  onUiSubmit: (msg: string) => void;
  busy: boolean;
  interactive?: boolean;
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
        <AssistantText text={part.text} onUiSubmit={onUiSubmit} busy={busy} interactive={interactive} />
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
  // false on non-last assistant turns: the cards stay explorable but their
  // agent round-trip (callbacks/inputs) locks — native BotMessage parity.
  interactive = true,
}: {
  turn: ChatTurn;
  thinking?: string;
  onUiSubmit: (msg: string) => void;
  busy: boolean;
  interactive?: boolean;
}) {
  const parts = turn.parts;
  // Reasoning block grows live while streaming (fed by the `reasoning` SSE frames)
  // and settles on the done frame — it rides above the answer/status throughout.
  const reasoning = turn.reasoning?.trim() ? <ReasoningBlock text={turn.reasoning} /> : null;
  if (!parts || parts.length === 0) {
    // Pre-content stream → Deneb's "응답 중" sparkle, with the gateway-owned
    // progress phase as its inline summary; the live reasoning block sits above it.
    if (turn.status === "streaming") {
      return (
        <div className="ai-turn-body">
          {reasoning}
          <DenebStatus summary={thinking?.trim() ? thinking : undefined} startedAt={turn.startedAt} />
        </div>
      );
    }
    return (
      <div className="ai-turn-body">
        {reasoning}
        <AssistantText text={turn.text || ""} onUiSubmit={onUiSubmit} busy={busy} interactive={interactive} />
      </div>
    );
  }
  return (
    <div className="ai-turn-body">
      {reasoning}
      {parts.map((p, i) =>
        p.kind === "text" ? (
          <AssistantText key={i} text={p.text} onUiSubmit={onUiSubmit} busy={busy} interactive={interactive} />
        ) : p.kind === "attachment" ? (
          <AttachmentResult key={p.id || i} part={p} onUiSubmit={onUiSubmit} busy={busy} interactive={interactive} />
        ) : (
          <ToolChip key={p.id || i} part={p} />
        ),
      )}
      {turn.status === "streaming" ? (
        <DenebStatus summary={thinking?.trim() ? thinking : undefined} startedAt={turn.startedAt} />
      ) : null}
    </div>
  );
}

// 답변 밑 공용 액션 — 마지막 스트림 답변의 다시 생성(transcript-loaded turns have no
// parts) + 완료된 답변의 인쇄(모닝레터·브리핑 카드 포함, .ai-turn subtree만 프린트).
// children으로 surface 고유 액션이 뒤에 붙는다 (예: AIPanel의 노트 저장).
export function AssistantTurnActions({
  turn,
  lastId,
  busy,
  onRegenerate,
  variants,
  onVariant,
  children,
}: {
  turn: ChatTurn;
  lastId?: string;
  busy: boolean;
  onRegenerate: () => void;
  variants?: VariantView | null;
  onVariant?: (dir: -1 | 1) => void;
  children?: ReactNode;
}) {
  const [copied, setCopied] = useState(false);
  if (turn.role !== "assistant") return null;
  async function copyAnswer() {
    try {
      await navigator.clipboard.writeText(turn.text);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard blocked — no-op */
    }
  }
  return (
    <>
      {turn.id === lastId && variants && onVariant && !busy && turn.status !== "streaming" && (
        <span className="variant-nav no-print">
          <button
            className="row-btn"
            disabled={variants.index === 0}
            onClick={() => onVariant(-1)}
            title="이전 답변 변형"
          >
            ‹
          </button>
          <span className="variant-count">
            {variants.index + 1}/{variants.count}
          </span>
          <button
            className="row-btn"
            disabled={variants.index === variants.count - 1}
            onClick={() => onVariant(1)}
            title="다음 답변 변형"
          >
            ›
          </button>
        </span>
      )}
      {turn.id === lastId && turn.parts && turn.canRegenerate !== false && !busy && turn.status !== "streaming" && (
        <button className="row-btn ai-regen no-print" onClick={onRegenerate} title="다시 생성">
          <Icon name="refresh" size={12} /> 다시 생성
        </button>
      )}
      {turn.status !== "streaming" && turn.text.trim().length > 0 && (
        <button className="row-btn ai-copy no-print" onClick={() => void copyAnswer()} title="답변 전체를 복사">
          <Icon name={copied ? "check" : "copy"} size={12} /> {copied ? "복사됨" : "복사"}
        </button>
      )}
      {turn.status !== "streaming" && (turn.text.trim().length > 0 || (turn.parts?.length ?? 0) > 0) && (
        <button
          className="row-btn ai-print no-print"
          onClick={(e) => printClosest(e.currentTarget, ".ai-turn")}
          title="이 답변을 인쇄 (프린터 또는 PDF)"
        >
          <Icon name="printer" size={12} /> 인쇄
        </button>
      )}
      {children}
    </>
  );
}
