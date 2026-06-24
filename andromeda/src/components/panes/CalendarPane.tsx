import { useCallback, useEffect, useMemo, useState } from "react";
import { useCreate, useInvalidate, useUpdate } from "@refinedev/core";

import type { CalEvent } from "@/types";
import { serializeList } from "@/aiText";
import { useCachedList } from "@/cachedList";
import {
  type CalendarProposal,
  type GatewayConfig,
  acceptCalendarProposal,
  listCalendarProposals,
  rejectCalendarProposal,
} from "@/gateway";
import { calSpan, calStamp, dayKey, errText, eventDayKeys, eventEndMs, eventTitle } from "@/format";
import { useAction } from "@/useAction";
import { usePaneTarget } from "@/usePaneTarget";
import { useRegisterPane, useWorkspace, type PaneTarget } from "@/workspaceContext";
import { GridNotice, RowBtn } from "@/components/Grid";
import { MonthGrid } from "@/components/MonthGrid";
import { Detail, Field, Modal } from "@/components/Modal";
import { EventAnalysis } from "./EventAnalysis";
import { parseDayKey, toLocalInput, visibleRangeForMonth } from "./calendarHelpers";

export function CalendarPane() {
  const { connected, cfg } = useWorkspace();
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
  const [proposals, setProposals] = useState<CalendarProposal[]>([]);
  const [proposalStatus, setProposalStatus] = useState("");
  const [proposalBusyId, setProposalBusyId] = useState<string | null>(null);
  const selectedEvent = useMemo(
    () => events.find((ev) => String(ev.id) === selectedEventId) ?? null,
    [events, selectedEventId],
  );

  async function refreshProposals() {
    if (!connected) {
      setProposals([]);
      return;
    }
    try {
      setProposals(await listCalendarProposals(cfg));
    } catch {
      // Older or partially-wired gateways simply don't show the proposal tray.
      setProposals([]);
    }
  }

  async function decideProposal(p: CalendarProposal, decision: "accept" | "reject") {
    setProposalBusyId(p.id);
    setProposalStatus(decision === "accept" ? "일정에 추가하는 중…" : "제안을 거절하는 중…");
    try {
      if (decision === "accept") {
        await acceptCalendarProposal(cfg, p.id);
        setProposalStatus("일정에 추가됨");
        refreshCalendarData();
      } else {
        await rejectCalendarProposal(cfg, p.id);
        setProposalStatus("제안 거절됨");
      }
      setProposals((rows) => rows.filter((row) => row.id !== p.id));
      void refreshProposals();
    } catch (err) {
      setProposalStatus(`오류: ${errText(err)}`);
    } finally {
      setProposalBusyId(null);
    }
  }

  useEffect(() => {
    void refreshProposals();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, cfg.url, cfg.token]);

  // Deep-link: focus the targeted day/event when another pane opens the calendar.
  // When a dayKey is present (the common case) we resolve the day immediately and
  // set selectedEventId from the id — the selectedEvent memo binds the event once
  // events load — so the target is consumed at once and never re-applies over the
  // user's later navigation. An id WITHOUT a dayKey needs its event to find the
  // day, so we keep the target pending (return false) while events are loading.
  const applyTarget = useCallback(
    (t: PaneTarget) => {
      const matchedEvent = t.id !== undefined ? events.find((ev) => String(ev.id) === String(t.id)) : undefined;
      const matchedKey = t.dayKey ?? (matchedEvent ? eventDayKeys(matchedEvent.start, matchedEvent.end)[0] : "");
      if (!matchedKey) return t.id !== undefined && query.isLoading ? false : undefined;
      const targetDate = parseDayKey(matchedKey);
      if (!targetDate) return;
      setCursor({ y: targetDate.getFullYear(), m: targetDate.getMonth() });
      setSelectedDay(matchedKey);
      setSelectedEventId(t.id !== undefined ? String(t.id) : null);
    },
    [events, query.isLoading],
  );
  usePaneTarget("calendar", applyTarget);

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

  const calendarText = serializeList("일정", events, (ev) => {
    const span = calSpan(ev.start, ev.end);
    return `- ${eventTitle(ev)}${span ? ` (${span})` : ""}${ev.location ? ` @${ev.location}` : ""}`;
  });
  const proposalText = serializeList(
    "일정 제안",
    proposals,
    (p) =>
      `- ${p.title}${p.start ? ` (${formatProposalStart(p)})` : ""}${p.sourceSubject ? ` · ${p.sourceSubject}` : ""}`,
  );
  const aiText = [calendarText, proposalText].filter(Boolean).join("\n\n");
  useRegisterPane("calendar-range", aiText);

  // Category → agenda dot tint, mirroring the month-grid markers.
  const agendaDotClass = (ev: CalEvent): string => {
    const c = ev.category === "deadline" ? "deadline" : ev.category === "others" ? "others" : "mine";
    return `cal-agenda-dot ${c}`;
  };

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

      <div className="cal-layout">
        {connected && (
          <div className="cal-cal">
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

        <div className="cal-agenda-col">
          <CalendarProposalTray
            proposals={proposals}
            status={proposalStatus}
            busyId={proposalBusyId}
            onAccept={(p) => void decideProposal(p, "accept")}
            onReject={(p) => void decideProposal(p, "reject")}
          />
          {connected && (
            <div style={{ display: "flex", alignItems: "baseline", gap: 10, marginBottom: 10 }}>
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
          )}
          <GridNotice
            query={query}
            count={listEvents.length}
            empty={selectedDay ? "이 날 일정이 없습니다." : "이 달 일정이 없습니다."}
          >
            <div className="cal-agenda">
              {listEvents.map((ev) => {
                const id = String(ev.id);
                const sel = selectedEventId === id;
                const span = calSpan(ev.start, ev.end) || "—";
                const toggle = () => setSelectedEventId((p) => (p === id ? null : id));
                return (
                  <div
                    key={id}
                    className={"cal-agenda-item" + (sel ? " selected" : "")}
                    role="button"
                    tabIndex={0}
                    aria-pressed={sel}
                    onClick={toggle}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        toggle();
                      }
                    }}
                  >
                    <span className={agendaDotClass(ev)} />
                    <div className="cal-agenda-body">
                      <div className="cal-agenda-title">{eventTitle(ev)}</div>
                      <div className="cal-agenda-meta">
                        {span}
                        {ev.location ? ` · ${ev.location}` : ""}
                      </div>
                    </div>
                    {ev.local && (
                      <RowBtn
                        onClick={() => {
                          if (selectedEventId === id) setSelectedEventId(null);
                          run("miniapp.calendar.delete", { id: ev.id });
                        }}
                        disabled={busy}
                        danger
                        title="삭제"
                      >
                        삭제
                      </RowBtn>
                    )}
                  </div>
                );
              })}
            </div>
          </GridNotice>
        </div>
      </div>

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

function CalendarProposalTray({
  proposals,
  status,
  busyId,
  onAccept,
  onReject,
}: {
  proposals: CalendarProposal[];
  status: string;
  busyId: string | null;
  onAccept: (proposal: CalendarProposal) => void;
  onReject: (proposal: CalendarProposal) => void;
}) {
  if (proposals.length === 0 && !status) return null;

  return (
    <section className="cal-proposals" aria-label="일정 제안">
      <div className="cal-proposals-head">
        <h3>일정 제안</h3>
        {proposals.length > 0 && <span>{proposals.length}</span>}
      </div>
      {proposals.length > 0 && (
        <div className="cal-proposal-list">
          {proposals.map((p) => {
            const busy = busyId === p.id;
            return (
              <div key={p.id} className="cal-proposal">
                <div className="cal-proposal-body">
                  <div className="cal-proposal-title">{p.title || "(제목 없음)"}</div>
                  <div className="cal-proposal-meta">
                    {[proposalKindLabel(p.kind), formatProposalStart(p), p.sourceFrom, p.sourceSubject]
                      .filter(Boolean)
                      .join(" · ")}
                  </div>
                </div>
                <div className="cal-proposal-actions">
                  <button className="row-btn" onClick={() => onReject(p)} disabled={Boolean(busyId)}>
                    거절
                  </button>
                  <button
                    className="row-btn"
                    onClick={() => onAccept(p)}
                    disabled={Boolean(busyId)}
                    title="일정에 추가"
                  >
                    {busy ? "처리 중" : "수락"}
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      )}
      {status && <div className="pane-status cal-proposal-status">{status}</div>}
    </section>
  );
}

function proposalKindLabel(kind?: string): string {
  if (kind === "deadline") return "마감";
  if (kind === "meeting") return "회의";
  return "";
}

function formatProposalStart(p: CalendarProposal): string {
  if (!p.start) return "";
  if (p.allDay || /^\d{4}-\d{2}-\d{2}$/.test(p.start)) return p.start;
  return calSpan(p.start, undefined);
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
    const payload: Record<string, unknown> = {
      summary: sum,
      allDay,
      start: localInputToRpcIso(start),
    };
    if (end) payload.end = localInputToRpcIso(end);
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

function localInputToRpcIso(value: string): string {
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? value : d.toISOString();
}
