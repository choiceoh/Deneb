import { useMemo, useState } from "react";

import type { CalEvent } from "@/types";
import { serializeList } from "@/aiText";
import { useCachedList } from "@/cachedList";
import { calSpan, dayKey, eventDayKeys, eventTitle } from "@/format";
import { useAction } from "@/useAction";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { Column, Grid, GridNotice, RowBtn } from "@/components/Grid";
import { MonthGrid } from "@/components/MonthGrid";

export function CalendarPane() {
  const { connected } = useWorkspace();
  const { result, query } = useCachedList<CalEvent>("calendar", connected);
  // Stable reference so the day-map memo below only recomputes when data changes.
  const events = useMemo(() => result?.data ?? [], [result?.data]);
  const { run, error, busy } = useAction(() => void query.refetch());

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
      <h2 style={{ marginTop: 2 }}>일정</h2>
      {error && <p style={{ color: "var(--due)", fontSize: 12, margin: "0 0 8px" }}>오류: {error}</p>}

      {connected && (
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
      )}

      <h3 style={{ margin: "22px 0 10px", color: "var(--muted)" }}>다가오는 일정</h3>
      <GridNotice query={query} count={events.length} empty="다가오는 일정이 없습니다.">
        <Grid columns={columns} rows={events} getKey={(ev) => String(ev.id)} maxWidth={780} />
      </GridNotice>
    </>
  );
}
