import { useList } from "@refinedev/core";
import type { CalEvent } from "../../types";
import { calSpan } from "../../format";
import { useRegisterPane, useWorkspace } from "../../workspaceContext";
import { Column, Grid, GridNotice } from "../Grid";

export function CalendarPane() {
  const { connected } = useWorkspace();
  const { result, query } = useList<CalEvent>({ resource: "calendar", queryOptions: { enabled: connected } });
  const events = result?.data ?? [];

  const aiText = events.length
    ? `[일정 ${events.length}건]\n` +
      events
        .map((ev) => {
          const span = calSpan(ev.start, ev.end);
          return `- ${ev.title ?? ev.summary ?? "(제목 없음)"}${span ? ` (${span})` : ""}${
            ev.location ? ` @${ev.location}` : ""
          }`;
        })
        .join("\n")
    : "";
  useRegisterPane("calendar", aiText);

  const columns: Column<CalEvent>[] = [
    {
      header: "시간",
      width: 240,
      tdStyle: { fontSize: 13, opacity: 0.8, whiteSpace: "nowrap" },
      cell: (ev) => calSpan(ev.start, ev.end) || "—",
    },
    { header: "일정", cell: (ev) => ev.title ?? ev.summary ?? "(제목 없음)" },
    { header: "장소", width: 160, tdStyle: { fontSize: 13, opacity: 0.7 }, cell: (ev) => ev.location ?? "" },
  ];

  return (
    <>
      <h2 style={{ marginTop: 2 }}>일정</h2>
      <GridNotice query={query} count={events.length} empty="다가오는 일정이 없습니다.">
        <Grid columns={columns} rows={events} getKey={(ev) => String(ev.id)} maxWidth={760} />
      </GridNotice>
    </>
  );
}
