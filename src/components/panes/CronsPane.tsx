import { useState } from "react";
import { useList } from "@refinedev/core";
import type { Cron } from "@/types";
import { serializeList } from "@/aiText";
import { CRON_RPC } from "@/resources";
import { color, ellipsis } from "@/theme";
import { fmtDate } from "@/format";
import { useAction } from "@/useAction";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { Column, Grid, GridNotice, RowBtn } from "@/components/Grid";
import { Detail, Modal } from "@/components/Modal";

const hasError = (c: Cron) => Boolean(c.lastError) || (c.consecutiveErrors ?? 0) > 0;

export function CronsPane() {
  const { connected } = useWorkspace();
  const { result, query } = useList<Cron>({ resource: "crons", queryOptions: { enabled: connected } });
  const crons = result?.data ?? [];
  const { run, error, busy } = useAction(() => void query.refetch());
  const [selected, setSelected] = useState<Cron | null>(null);

  const aiText = serializeList(
    "크론",
    crons,
    (c) =>
      `- ${c.name ?? "(이름 없음)"}${c.schedule ? ` (${c.schedule})` : ""}${c.enabled === false ? " [중지]" : ""}` +
      `${hasError(c) ? ` [오류${c.consecutiveErrors ? ` ${c.consecutiveErrors}회` : ""}${c.lastError ? `: ${c.lastError}` : ""}]` : ""}`,
  );
  useRegisterPane("crons", aiText);

  const columns: Column<Cron>[] = [
    {
      header: "이름",
      cell: (c) => (
        <>
          <div>{c.name ?? "—"}</div>
          {c.lastError && (
            <div style={{ fontSize: 12, color: color.danger, ...ellipsis(360) }} title={c.lastError}>
              {c.lastError}
            </div>
          )}
        </>
      ),
    },
    { header: "주기", width: 160, tdStyle: { fontSize: 13, opacity: 0.75 }, cell: (c) => c.schedule ?? "" },
    {
      header: "다음 실행",
      width: 128,
      tdStyle: { fontSize: 13, opacity: 0.7, whiteSpace: "nowrap" },
      cell: (c) => fmtDate(c.nextRunAtMs),
    },
    {
      header: "상태",
      width: 80,
      cell: (c) =>
        (c.consecutiveErrors ?? 0) > 0 ? (
          <span style={{ color: color.danger }}>오류 {c.consecutiveErrors}회</span>
        ) : c.enabled === false ? (
          "중지"
        ) : (
          "활성"
        ),
    },
    {
      header: "",
      width: 170,
      tdStyle: { whiteSpace: "nowrap", textAlign: "right" },
      cell: (c) => (
        <span style={{ display: "inline-flex", gap: 2, justifyContent: "flex-end" }}>
          <RowBtn onClick={() => run(CRON_RPC.run, { id: c.id })} disabled={busy} title="지금 실행">
            실행
          </RowBtn>
          <RowBtn
            onClick={() => run(CRON_RPC.update, { id: c.id, enabled: c.enabled === false })}
            disabled={busy}
            title={c.enabled === false ? "활성화" : "중지"}
          >
            {c.enabled === false ? "활성화" : "중지"}
          </RowBtn>
          <RowBtn onClick={() => run(CRON_RPC.remove, { id: c.id })} disabled={busy} danger title="삭제">
            삭제
          </RowBtn>
        </span>
      ),
    },
  ];

  return (
    <>
      <h2 style={{ marginTop: 2 }}>크론</h2>
      {error && <p style={{ color: "var(--due)", fontSize: 12, margin: "0 0 8px" }}>오류: {error}</p>}
      <GridNotice query={query} count={crons.length} empty="크론이 없습니다.">
        <Grid
          columns={columns}
          rows={crons}
          getKey={(c) => String(c.id)}
          maxWidth={840}
          onRowClick={(c) => setSelected(c)}
        />
      </GridNotice>
      {selected && <CronDetail cron={selected} onClose={() => setSelected(null)} />}
    </>
  );
}

// Full cron detail — payload + the error fields the grid only hints at.
function CronDetail({ cron, onClose }: { cron: Cron; onClose: () => void }) {
  return (
    <Modal
      title={cron.name ?? "크론"}
      onClose={onClose}
      footer={
        <button className="btn" onClick={onClose}>
          닫기
        </button>
      }
    >
      <Detail label="상태" value={cron.enabled === false ? "중지" : "활성"} />
      {cron.schedule && <Detail label="주기" value={cron.schedule} />}
      {cron.nextRunAtMs != null && <Detail label="다음 실행" value={fmtDate(cron.nextRunAtMs)} />}
      {cron.payloadKind && <Detail label="종류" value={cron.payloadKind} />}
      {cron.payloadPreview && <Detail label="페이로드" value={cron.payloadPreview} multiline />}
      {(cron.consecutiveErrors ?? 0) > 0 && (
        <Detail label="연속 오류" value={<span style={{ color: color.danger }}>{cron.consecutiveErrors}회</span>} />
      )}
      {cron.lastError && (
        <Detail label="마지막 오류" value={<span style={{ color: color.danger }}>{cron.lastError}</span>} multiline />
      )}
    </Modal>
  );
}
