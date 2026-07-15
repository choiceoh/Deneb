import { useMemo, useState } from "react";
import type { GroupwareApprovalRow } from "@/gen/miniappWire";
import { useCachedList } from "@/cachedList";
import { APPROVALS_RPC } from "@/resources";
import { addDays, dayLabel, startOfDay } from "@/format";
import { useAction } from "@/useAction";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { DayPager } from "@/components/DayPager";
import { Column, Grid, GridNotice } from "@/components/Grid";

// Recent 전체 결재 snapshot; day-pager filters client-side (Amaranth list has no
// date-range API). Mirrors mail/feed lookback so empty days never trap the pager.
const APPROVALS_LIMIT = 100;
const APPROVALS_LOOKBACK_DAYS = 31;

/** Parse Amaranth date stamps (2026-07-16 / 2026.07.16 / 20260716) → local midnight ms. */
export function approvalDayMs(date?: string): number | null {
  const s = (date ?? "").trim();
  const m = /^(\d{4})[-./]?(\d{2})[-./]?(\d{2})/.exec(s);
  if (!m) return null;
  return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3])).getTime();
}

function rowLine(a: GroupwareApprovalRow): string {
  const bits = [
    a.title ?? "(제목 없음)",
    a.drafter ? `기안 ${a.drafter}` : "",
    a.status ?? "",
    a.docNo ?? "",
  ].filter(Boolean);
  return `- ${bits.join(" · ")}`;
}

export function ApprovalsPane() {
  const { connected } = useWorkspace();
  const [dayMs, setDayMs] = useState<number>(() => startOfDay());
  const { result, query } = useCachedList<GroupwareApprovalRow & { id?: string }>("approvals", connected, {
    meta: { rpcParams: { folder: "total", limit: APPROVALS_LIMIT } },
  });
  const rows = useMemo(
    () => (result?.data ?? []).map((a) => ({ ...a, id: a.docId ?? a.id })),
    [result?.data],
  );
  const [selectedId, setSelectedId] = useState<string | undefined>();
  const { run, error, busy } = useAction(() => void query.refetch());

  // eslint-disable-next-line react-hooks/purity -- wall-clock for day pager bounds
  const nowMs = Date.now();
  const todayMs = startOfDay(nowMs);

  const dayRows = useMemo(() => {
    return rows
      .filter((a) => {
        const ms = approvalDayMs(a.date);
        return (ms ?? todayMs) === dayMs;
      })
      .sort((a, b) => String(b.docId ?? "").localeCompare(String(a.docId ?? "")));
  }, [rows, dayMs, todayMs]);

  const itemDays = rows.map((a) => approvalDayMs(a.date) ?? todayMs);
  const minDayMs = Math.min(addDays(todayMs, -APPROVALS_LOOKBACK_DAYS), ...itemDays, todayMs);
  const maxDayMs = Math.max(todayMs, ...itemDays);

  const aiText =
    `[결재 · ${dayLabel(dayMs, nowMs)}]\n` +
    (dayRows.length ? dayRows.map(rowLine).join("\n") : "(이 날짜에는 결재 문서가 없습니다)");
  useRegisterPane("approvals", aiText);

  function goToDay(next: number) {
    setDayMs(next);
    setSelectedId(undefined);
  }

  async function act(doc: GroupwareApprovalRow, decision: "approve" | "reject") {
    const id = String(doc.docId ?? "").trim();
    if (!id) return;
    const label = decision === "approve" ? "승인" : "반려";
    const title = doc.title || "이 결재 문서";
    if (!window.confirm(`${label}할까요?\n\n${title} (doc ${id})\n그룹웨어에 즉시 반영됩니다.`)) return;
    const result = await run(APPROVALS_RPC.act, { docId: id, decision });
    if (result !== undefined) setSelectedId(undefined);
  }

  const columns: Column<GroupwareApprovalRow>[] = [
    {
      header: "상태",
      width: 88,
      tdStyle: { verticalAlign: "top", fontSize: 13, opacity: 0.75 },
      cell: (a) => a.status || (a.folder === "pending" ? "미결" : "—"),
    },
    {
      header: "제목",
      cell: (a) => (
        <div>
          <div>{a.title || "(제목 없음)"}</div>
          {(a.drafter || a.docNo) && (
            <div style={{ fontSize: 12, opacity: 0.65, marginTop: 2 }}>
              {[a.drafter && `기안 ${a.drafter}`, a.docNo].filter(Boolean).join(" · ")}
            </div>
          )}
        </div>
      ),
    },
  ];

  const selected = dayRows.find((a) => String(a.docId) === String(selectedId));

  return (
    <>
      <h2 style={{ marginTop: 2 }}>결재</h2>
      {error && <p className="pane-error">오류: {error}</p>}
      {connected && (
        <DayPager
          label={dayLabel(dayMs, nowMs)}
          count={dayRows.length}
          canPrev={dayMs > minDayMs}
          canNext={dayMs < maxDayMs}
          atToday={dayMs === todayMs}
          onPrev={() => goToDay(addDays(dayMs, -1))}
          onNext={() => goToDay(addDays(dayMs, 1))}
          onToday={() => goToDay(todayMs)}
        />
      )}
      <GridNotice query={query} count={dayRows.length} empty="이 날짜에는 결재 문서가 없습니다.">
        <Grid
          columns={columns}
          rows={dayRows}
          getKey={(a) => String(a.docId ?? "")}
          onRowClick={(a) =>
            setSelectedId((cur) => (String(cur) === String(a.docId) ? undefined : String(a.docId)))
          }
          isRowSelected={(a) => String(a.docId) === String(selectedId)}
          rowTitle={(a) => a.title ?? "(제목 없음)"}
          renderExpandedRow={() =>
            selected ? (
              <ApprovalDetail
                doc={selected}
                busy={busy}
                onApprove={() => void act(selected, "approve")}
                onReject={() => void act(selected, "reject")}
                onClose={() => setSelectedId(undefined)}
              />
            ) : null
          }
        />
      </GridNotice>
    </>
  );
}

function ApprovalDetail({
  doc,
  busy,
  onApprove,
  onReject,
  onClose,
}: {
  doc: GroupwareApprovalRow;
  busy: boolean;
  onApprove: () => void;
  onReject: () => void;
  onClose: () => void;
}) {
  const meta = [doc.status, doc.drafter && `기안 ${doc.drafter}`, doc.docNo, doc.docId && `id ${doc.docId}`]
    .filter(Boolean)
    .join(" · ");
  return (
    <section className="workfeed-detail" aria-label="결재 상세">
      <div className="workfeed-detail-head">
        <div className="workfeed-detail-heading">
          <div className="workfeed-detail-meta">{meta}</div>
          <div className="workfeed-detail-title">{doc.title ?? "(제목 없음)"}</div>
        </div>
        <div className="workfeed-detail-actions">
          {doc.canAct && (
            <>
              <button className="row-btn" disabled={busy} onClick={onApprove}>
                승인
              </button>
              <button className="row-btn" disabled={busy} onClick={onReject}>
                반려
              </button>
            </>
          )}
          <button className="row-btn" onClick={onClose}>
            닫기
          </button>
        </div>
      </div>
    </section>
  );
}
