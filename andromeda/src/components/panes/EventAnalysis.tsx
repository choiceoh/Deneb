// Inline AI analysis of a single calendar event — extracted from CalendarPane so
// the calendar view's own state/rendering stays separate from this self-contained
// streaming feature (its own status/busy state, AbortController, fallback text).
// Streams a short analysis via chatStream; when the gateway is offline (or the
// event lacks a description), shows a heuristic fallback instead.
import { useEffect, useMemo, useRef, useState } from "react";

import type { CalEvent } from "@/types";
import { chatStream, type GatewayConfig } from "@/gateway";
import { calSpan, errText, eventDayKeys, eventEndMs, eventTitle } from "@/format";
import { Markdown } from "@/components/Markdown";

// One-line-per-field summary of the event, used as the AI's workspace context.
export function eventContext(event: CalEvent): string {
  return [
    `제목: ${eventTitle(event)}`,
    `시간: ${calSpan(event.start, event.end) || "미정"}`,
    event.location ? `장소: ${event.location}` : "",
    event.description ? `설명:\n${event.description}` : "",
  ]
    .filter(Boolean)
    .join("\n");
}

// Heuristic analysis used as a placeholder before the AI responds (or when the
// gateway is offline). Infers meeting/deadline/timing/prep/risk/next from the
// title + description keywords and the event's span.
export function fallbackAnalysis(event: CalEvent): string {
  const title = eventTitle(event);
  const span = calSpan(event.start, event.end);
  const days = eventDayKeys(event.start, event.end);
  const endMs = eventEndMs(event.start, event.end);
  const now = Date.now();
  const text = `${title} ${event.description ?? ""}`.toLowerCase();
  const meeting = /회의|미팅|리뷰|싱크|면담|콜|세션/.test(text);
  const deadline = /마감|제출|deadline|due/.test(text);
  const past = endMs !== null && endMs <= now;
  const timing = past
    ? "이미 끝난 일정입니다."
    : days.length > 1
      ? `${days.length}일에 걸친 일정입니다.`
      : event.allDay
        ? "종일 일정입니다."
        : "시간이 정해진 일정입니다.";
  const prep = deadline
    ? "마감 산출물과 제출 경로를 먼저 확인하세요."
    : meeting
      ? "안건, 결정할 항목, 공유 자료를 미리 정리하세요."
      : event.location
        ? "장소와 이동 시간을 먼저 확인하세요."
        : "참석자와 필요한 자료를 확인하세요.";
  const risk = past
    ? "후속 기록이나 액션 아이템만 남기면 됩니다."
    : event.location
      ? "앞뒤 일정과 이동 시간이 겹치지 않는지 확인하세요."
      : "장소 정보가 없으면 직전에 확인 비용이 생길 수 있습니다.";
  const next = meeting
    ? "회의 후 결정사항과 담당자를 남기는 것이 좋습니다."
    : deadline
      ? "완료 여부를 체크하고 관련 할일을 닫으세요."
      : "필요하면 이 일정에서 파생되는 할일을 하나로 분리하세요.";

  return [
    `**${title}**${span ? `  \n${span}` : ""}`,
    "",
    `- **시간**: ${timing}`,
    `- **준비**: ${prep}`,
    `- **주의**: ${risk}`,
    `- **후속**: ${next}`,
  ].join("\n");
}

export function EventAnalysis({ event, connected, cfg }: { event: CalEvent; connected: boolean; cfg: GatewayConfig }) {
  const title = eventTitle(event);
  const span = calSpan(event.start, event.end);
  const fallback = useMemo(() => fallbackAnalysis(event), [event]);
  const hasAutoAnalysisInput = Boolean(title.trim() && event.description?.trim());
  const [answer, setAnswer] = useState("");
  const [status, setStatus] = useState("");
  const [busy, setBusy] = useState(false);
  const abortRef = useRef<AbortController | null>(null);

  async function analyze() {
    if (!connected) {
      setStatus("게이트웨이에 연결하면 AI 분석을 갱신합니다.");
      return;
    }
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    setAnswer("");
    setBusy(true);
    setStatus("AI 분석 중…");
    let streamed = "";
    let streamError = "";
    try {
      await chatStream(
        cfg,
        "이 일정 하나만 보고 준비할 것, 충돌 가능성, 바로 할 후속 조치를 한국어로 짧게 정리해줘. 답은 4줄 이내로.",
        {
          onDelta: (delta) => {
            streamed += delta;
            setAnswer(streamed);
          },
          onDone: (final) => {
            if (!streamed.trim() && final.text.trim()) setAnswer(final.text);
          },
          onError: (err) => {
            streamError = err;
          },
        },
        {
          sessionKey: `calendar:inline:${event.id ?? title}`,
          workspaceContext: `[선택한 일정]\n${eventContext(event)}`,
          signal: controller.signal,
        },
      );
      if (streamError) throw new Error(streamError);
      if (!controller.signal.aborted) setStatus("");
    } catch (err) {
      if (!controller.signal.aborted) setStatus(`AI 분석 실패: ${errText(err)}`);
    } finally {
      if (abortRef.current === controller) {
        abortRef.current = null;
        setBusy(false);
      }
    }
  }

  useEffect(() => {
    setAnswer("");
    setStatus("");
    if (hasAutoAnalysisInput) void analyze();
    return () => abortRef.current?.abort();
    // Re-run only when the chosen event changes or connectivity/config changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, cfg.url, cfg.token, event, hasAutoAnalysisInput]);

  return (
    <>
      <div className="calendar-workspace-head">
        <h3>AI 분석</h3>
        <button className="row-btn" onClick={() => void analyze()} disabled={!connected || busy}>
          {answer || busy ? "다시 분석" : "AI 분석"}
        </button>
      </div>
      <div className="calendar-analysis-title">{title}</div>
      {span && <div className="calendar-analysis-time">{span}</div>}
      {status && <div className="calendar-analysis-status">{status}</div>}
      <div className={"calendar-analysis-answer" + (busy ? " streaming" : "")}>
        <Markdown text={answer.trim() ? answer : fallback} />
      </div>
    </>
  );
}
