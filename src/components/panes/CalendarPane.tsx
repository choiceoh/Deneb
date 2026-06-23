import { useMemo, useState } from "react";
import { useCreate, useUpdate } from "@refinedev/core";

import type { CalEvent } from "@/types";
import { serializeList } from "@/aiText";
import { useCachedList } from "@/cachedList";
import { calSpan, calStamp, dayKey, errText, eventDayKeys, eventTitle } from "@/format";
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

export function CalendarPane() {
  const { connected } = useWorkspace();
  const { result, query } = useCachedList<CalEvent>("calendar", connected);
  // Stable reference so the day-map memo below only recomputes when data changes.
  const events = useMemo(() => result?.data ?? [], [result?.data]);
  const { run, error, busy } = useAction(() => void query.refetch());
  // null = closed · {} = create · { ev } = open existing (editable when ev.local).
  const [edit, setEdit] = useState<{ ev?: CalEvent } | null>(null);

  const now = new Date();
  const todayKey = dayKey(now);
  const [cursor, setCursor] = useState({ y: now.getFullYear(), m: now.getMonth() });

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
  useRegisterPane("calendar", aiText);

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
  const step = (delta: number) =>
    setCursor((c) => {
      const d = new Date(c.y, c.m + delta, 1);
      return { y: d.getFullYear(), m: d.getMonth() };
    });

  return (
    <>
      <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
        <h2 style={{ margin: 0 }}>일정</h2>
        <button className="btn" onClick={() => setEdit({})} disabled={!connected} style={{ marginLeft: "auto" }}>
          새 일정
        </button>
      </div>
      {error && <p style={{ color: "var(--due)", fontSize: 12, margin: "8px 0 0" }}>오류: {error}</p>}

      {connected && (
        <div style={{ marginTop: 14 }}>
          <MonthGrid
            year={cursor.y}
            month0={cursor.m}
            eventsByDay={eventsByDay}
            todayKey={todayKey}
            onPrev={() => step(-1)}
            onNext={() => step(1)}
            onToday={() => {
              const t = new Date();
              setCursor({ y: t.getFullYear(), m: t.getMonth() });
            }}
          />
        </div>
      )}

      <h3 style={{ margin: "22px 0 10px", color: "var(--muted)" }}>다가오는 일정</h3>
      <GridNotice query={query} count={events.length} empty="다가오는 일정이 없습니다.">
        <Grid
          columns={columns}
          rows={events}
          getKey={(ev) => String(ev.id)}
          maxWidth={780}
          onRowClick={(ev) => setEdit({ ev })}
        />
      </GridNotice>

      {edit && <EventModal event={edit.ev} onClose={() => setEdit(null)} onSaved={() => void query.refetch()} />}
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
          {status && <span style={{ fontSize: 12, color: "var(--muted)", marginRight: "auto" }}>{status}</span>}
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
