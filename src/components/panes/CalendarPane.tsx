import { useEffect, useMemo, useRef, useState } from "react";
import { useCreate, useInvalidate, useUpdate } from "@refinedev/core";

import type { CalEvent } from "@/types";
import { serializeList } from "@/aiText";
import { useCachedList } from "@/cachedList";
import { chatStream, type GatewayConfig } from "@/gateway";
import {
  calSpan,
  calStamp,
  dayKey,
  errText,
  eventDayKeys,
  eventEndMs,
  eventTitle,
  monthLabel,
  monthMatrix,
} from "@/format";
import { useAction } from "@/useAction";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { Column, Grid, GridNotice, RowBtn } from "@/components/Grid";
import { MonthGrid } from "@/components/MonthGrid";
import { Detail, Field, Modal } from "@/components/Modal";
import { Markdown } from "@/components/Markdown";

// RFC3339 → <input type="datetime-local"> value (local wall-clock, minute precision).
function toLocalInput(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function visibleRangeForMonth(year: number, month0: number) {
  const weeks = monthMatrix(year, month0);
  const first = weeks[0][0];
  const lastWeek = weeks[weeks.length - 1];
  const last = lastWeek[lastWeek.length - 1];
  const to = new Date(last);
  to.setDate(to.getDate() + 1);
  const from = first.toISOString();
  const toIso = to.toISOString();
  return {
    from,
    to: toIso,
    cacheKey: `calendar-range.${from}.${toIso}`,
    label: monthLabel(year, month0),
  };
}

function parseDayKey(key?: string): Date | null {
  const m = /^(\d{4})-(\d{1,2})-(\d{1,2})$/.exec(key ?? "");
  if (!m) return null;
  return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
}

export function CalendarPane() {
  const { connected, cfg, consumePaneTarget, paneTarget } = useWorkspace();
  const now = new Date();
  const todayKey = dayKey(now);
  const [cursor, setCursor] = useState({ y: now.getFullYear(), m: now.getMonth() });
  // A clicked day in the grid (dayKey) filters the list below to that date. null = visible month.
  const [selectedDay, setSelectedDay] = useState<string | null>(null);
  const visibleRange = useMemo(() => visibleRangeForMonth(cursor.y, cursor.m), [cursor.y, cursor.m]);
  const rangeMeta = useMemo(
    () => ({ rpcParams: { from: visibleRange.from, to: visibleRange.to } }),
    [visibleRange.from, visibleRange.to],
  );
  const { result, query } = useCachedList<CalEvent>("calendar-range", connected, {
    cacheKey: visibleRange.cacheKey,
    meta: rangeMeta,
  });
  const invalidate = useInvalidate();
  const refreshCalendarData = () => {
    void query.refetch();
    void invalidate({ resource: "calendar", invalidates: ["list"] });
  };
  // Stable reference so the day-map memo below only recomputes when data changes.
  const events = useMemo(() => result?.data ?? [], [result?.data]);
  const { run, error, busy } = useAction(refreshCalendarData);
  const [creating, setCreating] = useState(false);
  const [selectedEventId, setSelectedEventId] = useState<string | null>(null);
  const selectedEvent = useMemo(
    () => events.find((ev) => String(ev.id) === selectedEventId) ?? null,
    [events, selectedEventId],
  );

  useEffect(() => {
    if (paneTarget?.view !== "calendar") return;
    const matchedEvent =
      paneTarget.id !== undefined ? events.find((ev) => String(ev.id) === String(paneTarget.id)) : undefined;
    const matchedKey = paneTarget.dayKey ?? (matchedEvent ? eventDayKeys(matchedEvent.start, matchedEvent.end)[0] : "");
    const targetDate = parseDayKey(matchedKey);
    if (matchedKey && targetDate) {
      setCursor({ y: targetDate.getFullYear(), m: targetDate.getMonth() });
      setSelectedDay(matchedKey);
      setSelectedEventId(matchedEvent ? String(matchedEvent.id) : null);
      consumePaneTarget();
    } else if (!query.isLoading) {
      consumePaneTarget();
    }
  }, [consumePaneTarget, events, paneTarget, query.isLoading]);

  // Place each event on every day it spans, so the month grid can look a day up.
  const eventsByDay = useMemo(() => {
    const map = new Map<string, CalEvent[]>();
    for (const ev of events) {
      for (const k of eventDayKeys(ev.start, ev.end)) {
        const arr = map.get(k);
        if (arr) arr.push(ev);
        else map.set(k, [ev]);
      }
    }
    return map;
  }, [events]);

  const aiText = serializeList("일정", events, (ev) => {
    const span = calSpan(ev.start, ev.end);
    return `- ${eventTitle(ev)}${span ? ` (${span})` : ""}${ev.location ? ` @${ev.location}` : ""}`;
  });
  useRegisterPane("calendar-range", aiText);

  const columns: Column<CalEvent>[] = [
    {
      header: "시간",
      width: 230,
      tdStyle: { fontSize: 13, opacity: 0.8, whiteSpace: "nowrap" },
      cell: (ev) => calSpan(ev.start, ev.end) || "—",
    },
    { header: "일정", cell: (ev) => eventTitle(ev) },
    { header: "장소", width: 150, tdStyle: { fontSize: 13, opacity: 0.7 }, cell: (ev) => ev.location ?? "" },
    {
      header: "",
      width: 60,
      tdStyle: { textAlign: "right" },
      // Only locally-created events are deletable; Google-sourced events are read-only.
      // RowBtn stops click propagation itself, so it won't also open the row's modal.
      cell: (ev) =>
        ev.local ? (
          <RowBtn
            onClick={() => {
              if (selectedEventId === String(ev.id)) setSelectedEventId(null);
              run("miniapp.calendar.delete", { id: ev.id });
            }}
            disabled={busy}
            danger
            title="삭제"
          >
            삭제
          </RowBtn>
        ) : null,
    },
  ];

  // Step the visible month, normalizing year rollover via the Date constructor.
  // Clear any day selection — the picked day isn't in the new month's view.
  const step = (delta: number) => {
    setSelectedDay(null);
    setSelectedEventId(null);
    setCursor((c) => {
      const d = new Date(c.y, c.m + delta, 1);
      return { y: d.getFullYear(), m: d.getMonth() };
    });
  };

  // The list below shows the selected day's events, or the visible month. For the
  // CURRENT month, already-ended events are dropped so the list reads as "upcoming";
  // other months keep their full history, and a clicked day shows that day in full.
  // (The month grid above still marks every event.)
  const isCurrentMonth = cursor.y === now.getFullYear() && cursor.m === now.getMonth();
  const upcoming = isCurrentMonth
    ? events.filter((ev) => {
        const endMs = eventEndMs(ev.start, ev.end);
        return endMs === null || endMs > now.getTime();
      })
    : events;
  const listEvents = selectedDay ? (eventsByDay.get(selectedDay) ?? []) : upcoming;
  const listLabel = selectedDay
    ? `${selectedDay.split("-")[1]}월 ${selectedDay.split("-")[2]}일 일정`
    : `${visibleRange.label} 일정`;

  return (
    <>
      <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
        <h2 style={{ margin: 0 }}>일정</h2>
        <button className="btn" onClick={() => setCreating(true)} disabled={!connected} style={{ marginLeft: "auto" }}>
          새 일정
        </button>
      </div>
      {error && <p className="pane-error">오류: {error}</p>}

      {connected && (
        <div style={{ marginTop: 14 }}>
          <MonthGrid
            year={cursor.y}
            month0={cursor.m}
            eventsByDay={eventsByDay}
            todayKey={todayKey}
            selectedKey={selectedDay}
            onSelectDay={(k) => {
              setSelectedEventId(null);
              setSelectedDay((p) => (p === k ? null : k));
            }}
            onPrev={() => step(-1)}
            onNext={() => step(1)}
            onToday={() => {
              setSelectedDay(null);
              setSelectedEventId(null);
              const t = new Date();
              setCursor({ y: t.getFullYear(), m: t.getMonth() });
            }}
          />
        </div>
      )}

      <div style={{ display: "flex", alignItems: "baseline", gap: 10, margin: "22px 0 10px" }}>
        <h3 style={{ margin: 0, color: "var(--muted)" }}>{listLabel}</h3>
        {selectedDay && (
          <button
            className="row-btn"
            onClick={() => {
              setSelectedEventId(null);
              setSelectedDay(null);
            }}
          >
            ← 월 전체
          </button>
        )}
      </div>
      <GridNotice
        query={query}
        count={listEvents.length}
        empty={selectedDay ? "이 날 일정이 없습니다." : "이 달 일정이 없습니다."}
      >
        <Grid
          columns={columns}
          rows={listEvents}
          getKey={(ev) => String(ev.id)}
          maxWidth={780}
          onRowClick={(ev) => setSelectedEventId((id) => (id === String(ev.id) ? null : String(ev.id)))}
          isRowSelected={(ev) => selectedEventId === String(ev.id)}
        />
      </GridNotice>

      {selectedEvent && (
        <SelectedEventWorkspace
          key={String(selectedEvent.id)}
          event={selectedEvent}
          connected={connected}
          cfg={cfg}
          onClose={() => setSelectedEventId(null)}
          onSaved={refreshCalendarData}
        />
      )}
      {creating && <EventModal onClose={() => setCreating(false)} onSaved={refreshCalendarData} />}
    </>
  );
}

function SelectedEventWorkspace({
  event,
  connected,
  cfg,
  onClose,
  onSaved,
}: {
  event: CalEvent;
  connected: boolean;
  cfg: GatewayConfig;
  onClose: () => void;
  onSaved: () => void;
}) {
  return (
    <section className="calendar-workspace" aria-label="선택한 일정">
      <div className="calendar-workspace-panel">
        <EventForm event={event} onClose={onClose} onSaved={onSaved} stayOpenOnSave />
      </div>
      <div className="calendar-workspace-panel analysis">
        <EventAnalysis event={event} connected={connected} cfg={cfg} />
      </div>
    </section>
  );
}

// New events still use the focused modal. Existing rows open inline below the list.
function EventModal({ event, onClose, onSaved }: { event?: CalEvent; onClose: () => void; onSaved: () => void }) {
  return (
    <Modal title={event ? "일정 수정" : "새 일정"} onClose={onClose} width={560}>
      <EventForm event={event} onClose={onClose} onSaved={onSaved} />
    </Modal>
  );
}

function EventForm({
  event,
  onClose,
  onSaved,
  stayOpenOnSave = false,
}: {
  event?: CalEvent;
  onClose: () => void;
  onSaved: () => void;
  stayOpenOnSave?: boolean;
}) {
  const isNew = !event;
  const editable = isNew || event.local === true;

  const s = calStamp(event?.start);
  const e = calStamp(event?.end);
  const initAllDay = Boolean(event?.allDay || s.allDay);
  const [summary, setSummary] = useState(event?.summary ?? event?.title ?? "");
  const [allDay, setAllDay] = useState(initAllDay);
  // Stored as datetime-local strings; the date input binds to the YYYY-MM-DD slice.
  const [start, setStart] = useState(initAllDay ? (s.iso ? `${s.iso.slice(0, 10)}T00:00` : "") : toLocalInput(s.iso));
  const [end, setEnd] = useState(initAllDay ? (e.iso ? `${e.iso.slice(0, 10)}T00:00` : "") : toLocalInput(e.iso));
  const [location, setLocation] = useState(event?.location ?? "");
  const [description, setDescription] = useState(event?.description ?? "");
  const [status, setStatus] = useState("");
  const { mutate: createEvent } = useCreate();
  const { mutate: updateEvent } = useUpdate();

  function save() {
    const sum = summary.trim();
    if (!sum) return setStatus("제목을 입력하세요");
    if (!start) return setStatus("시작 시각을 입력하세요");
    // Field shapes are best-effort vs the live gateway (DESIGN §5) — verify live.
    const payload: Record<string, unknown> = {
      summary: sum,
      allDay,
      start: allDay ? start.slice(0, 10) : new Date(start).toISOString(),
    };
    if (end) payload.end = allDay ? end.slice(0, 10) : new Date(end).toISOString();
    if (location.trim()) payload.location = location.trim();
    if (description.trim()) payload.description = description.trim();
    setStatus("저장 중…");
    const opts = {
      onSuccess: () => {
        onSaved();
        setStatus("저장됨");
        if (!stayOpenOnSave) onClose();
      },
      onError: (err: unknown) => setStatus(`오류: ${errText(err)}`),
    };
    if (isNew) createEvent({ resource: "calendar", values: payload }, opts);
    else updateEvent({ resource: "calendar", id: event.id, values: payload }, opts);
  }

  // Read-only detail for Google-sourced events (not editable here).
  if (!editable && event) {
    return (
      <>
        <div className="calendar-workspace-head">
          <h3>일정 상세</h3>
          <button className="row-btn" onClick={onClose}>
            닫기
          </button>
        </div>
        <Detail label="제목" value={eventTitle(event)} />
        <Detail label="시간" value={calSpan(event.start, event.end) || "—"} />
        {event.location && <Detail label="장소" value={event.location} />}
        {event.description && <Detail label="설명" value={event.description} multiline />}
        <p style={{ fontSize: 12, color: "var(--muted-2)", marginTop: 4 }}>
          외부(구글) 일정은 여기서 수정할 수 없습니다.
        </p>
      </>
    );
  }

  return (
    <>
      {!isNew && (
        <div className="calendar-workspace-head">
          <h3>일정 편집</h3>
          <button className="row-btn" onClick={onClose}>
            닫기
          </button>
        </div>
      )}
      <Field label="제목">
        <input className="field" value={summary} onChange={(ev) => setSummary(ev.target.value)} autoFocus />
      </Field>
      <label style={{ display: "flex", alignItems: "center", gap: 7, fontSize: 13, marginBottom: 12 }}>
        <input type="checkbox" checked={allDay} onChange={(ev) => setAllDay(ev.target.checked)} />
        종일
      </label>
      <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 12 }}>
        <Field label="시작">
          {allDay ? (
            <input
              type="date"
              className="field"
              value={start.slice(0, 10)}
              onChange={(ev) => setStart(`${ev.target.value}T00:00`)}
            />
          ) : (
            <input type="datetime-local" className="field" value={start} onChange={(ev) => setStart(ev.target.value)} />
          )}
        </Field>
        <Field label="종료">
          {allDay ? (
            <input
              type="date"
              className="field"
              value={end.slice(0, 10)}
              onChange={(ev) => setEnd(`${ev.target.value}T00:00`)}
            />
          ) : (
            <input type="datetime-local" className="field" value={end} onChange={(ev) => setEnd(ev.target.value)} />
          )}
        </Field>
      </div>
      <Field label="장소">
        <input className="field" value={location} onChange={(ev) => setLocation(ev.target.value)} />
      </Field>
      <Field label="설명">
        <textarea
          className="field"
          value={description}
          onChange={(ev) => setDescription(ev.target.value)}
          rows={3}
          style={{ resize: "vertical", fontFamily: "inherit", lineHeight: 1.5 }}
        />
      </Field>
      <div className="calendar-form-actions">
        {status && (
          <span className="pane-status" style={{ marginRight: "auto" }}>
            {status}
          </span>
        )}
        <button className="btn" onClick={onClose}>
          {isNew ? "취소" : "닫기"}
        </button>
        <button className="btn btn-accent" onClick={save}>
          저장
        </button>
      </div>
    </>
  );
}

function eventContext(event: CalEvent): string {
  return [
    `제목: ${eventTitle(event)}`,
    `시간: ${calSpan(event.start, event.end) || "미정"}`,
    event.location ? `장소: ${event.location}` : "",
    event.description ? `설명:\n${event.description}` : "",
  ]
    .filter(Boolean)
    .join("\n");
}

function fallbackAnalysis(event: CalEvent): string {
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

function EventAnalysis({ event, connected, cfg }: { event: CalEvent; connected: boolean; cfg: GatewayConfig }) {
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
