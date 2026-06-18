import { useList } from "@refinedev/core";
import type { Cron } from "../../types";
import { fmtDate } from "../../format";
import { useRegisterPane, useWorkspace } from "../../workspaceContext";
import { Column, Grid, GridNotice } from "../Grid";

export function CronsPane() {
  const { connected } = useWorkspace();
  const { result, query } = useList<Cron>({ resource: "crons", queryOptions: { enabled: connected } });
  const crons = result?.data ?? [];

  const aiText = crons.length
    ? `[크론 ${crons.length}건]\n` +
      crons
        .map(
          (c) =>
            `- ${c.name ?? "(이름 없음)"}${c.schedule ? ` (${c.schedule})` : ""}${c.enabled === false ? " [중지]" : ""}`,
        )
        .join("\n")
    : "";
  useRegisterPane("crons", aiText);

  const columns: Column<Cron>[] = [
    { header: "이름", cell: (c) => c.name ?? "—" },
    { header: "주기", width: 200, tdStyle: { fontSize: 13, opacity: 0.75 }, cell: (c) => c.schedule ?? "" },
    {
      header: "다음 실행",
      width: 140,
      tdStyle: { fontSize: 13, opacity: 0.7, whiteSpace: "nowrap" },
      cell: (c) => fmtDate(c.nextRun),
    },
    { header: "상태", width: 60, cell: (c) => (c.enabled === false ? "중지" : "활성") },
  ];

  return (
    <>
      <h2 style={{ marginTop: 2 }}>크론</h2>
      <GridNotice query={query} count={crons.length} empty="크론이 없습니다.">
        <Grid columns={columns} rows={crons} getKey={(c) => String(c.id)} maxWidth={760} />
      </GridNotice>
    </>
  );
}
