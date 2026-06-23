import { useEffect, useMemo, useState } from "react";
import { useCreate, useInvalidate, useUpdate } from "@refinedev/core";

import type { CalEvent } from "@/types";
import { serializeList } from "@/aiText";
import { useCachedList } from "@/cachedList";
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
  const { connected, consumePaneTarget, paneTarget } = useWorkspace();
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
  // null = closed · {} = create · { ev } = open existing (editable when ev.local).
  const [edit, setEdit] = useState<{ ev?: CalEvent } | null>(null);

  useEffect(() => {
    if (paneTarget?.view !== "calendar") return;
    const matchedEvent =
      paneTarget.id !== undefined ? events.find((ev) => String(ev.id) === String(paneTarget.id)) : undefined;
    const matchedKey = paneTarget.dayKey ?? (matchedEvent ? eventDayKeys(matchedEvent.start, matchedEvent.end)[0] : "");
    const targetDate = parseDayKey(matchedKey);
    if (matchedKey && targetDate) {
      setCursor({ y: targetDate.getFullYear(), m: targetDate.getMonth() });
      setSelectedDay(matchedKey);
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
          <RowBtn onClick={() => run("miniapp.calendar.delete", { id: ev.id })} disabled={busy} danger title="삭제">
            삭제
          </RowBtn>
        ) : null,
    },
  ];

  // Step the visible month, normalizing year rollover via the Date constructor.
  // Clear any day selection — the picked day isn't in the new month's view.
  const step = (delta: number) => {
    setSelectedDay(null);
    setCursor((c) => {
      const d = new Date(c.y, c.m + delta, 1);
      return { y: d.getFullYear(), m: d.getMonth() };
    });
  };

  // The list below shows the selected day's events, or the visible month. For the
  // CURRENT month, already-ended events are dropped so the list reads as "upcoming";
  // other months keep their full history, and a clicked day shows that day in full.
  // (The month grid above still renders every event as a chip.)
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
        <button className="btn" onClick={() => setEdit({})} disabled={!connected} style={{ marginLeft: "auto" }}>
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
            onSelectDay={(k) => setSelectedDay((p) => (p === k ? null : k))}
            onPrev={() => step(-1)}
            onNext={() => step(1)}
            onToday={() => {
              setSelectedDay(null);
              const t = new Date();
              setCursor({ y: t.getFullYear(), m: t.getMonth() });
            }}
          />
        </div>
      )}

      <div style={{ display: "flex", alignItems: "baseline", gap: 10, margin: "22px 0 10px" }}>
        <h3 style={{ margin: 0, color: "var(--muted)" }}>{listLabel}</h3>
        {selectedDay && (
          <button className="row-btn" onClick={() => setSelectedDay(null)}>
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
          onRowClick={(ev) => setEdit({ ev })}
        />
      </GridNotice>

      {edit && <EventModal event={edit.ev} onClose={() => setEdit(null)} onSaved={refreshCalendarData} />}
    </>
  );
}

// Create (no event), edit (local event), or read-only detail (Google event) — one
// modal. Write paths use the already-wired calendar.create / calendar.update RPCs.
function EventModal({ event, onClose, onSaved }: { event?: CalEvent; onClose: () => void; onSaved: () => void }) {
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
        onClose();
      },
      onError: (err: unknown) => setStatus(`오류: ${errText(err)}`),
    };
    if (isNew) createEvent({ resource: "calendar", values: payload }, opts);
    else updateEvent({ resource: "calendar", id: event.id, values: payload }, opts);
  }

  // Read-only detail for Google-sourced events (not editable here).
  if (!editable) {
    return (
      <Modal
        title={eventTitle(event)}
        onClose={onClose}
        footer={
          <button className="btn" onClick={onClose}>
            닫기
          </button>
        }
      >
        <Detail label="시간" value={calSpan(event.start, event.end) || "—"} />
        {event.location && <Detail label="장소" value={event.location} />}
        {event.description && <Detail label="설명" value={event.description} multiline />}
        <p style={{ fontSize: 12, color: "var(--muted-2)", marginTop: 4 }}>
          외부(구글) 일정은 여기서 수정할 수 없습니다.
        </p>
      </Modal>
    );
  }

  return (
    <Modal
      title={isNew ? "새 일정" : "일정 수정"}
      onClose={onClose}
      footer={
        <>
          {status && (
            <span className="pane-status" style={{ marginRight: "auto" }}>
              {status}
            </span>
          )}
          <button className="btn" onClick={onClose}>
            취소
          </button>
          <button className="btn btn-accent" onClick={save}>
            저장
          </button>
        </>
      }
    >
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
    </Modal>
  );
}
