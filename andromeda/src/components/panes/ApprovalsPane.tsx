import { useState } from "react";
import type { GroupwareApprovalRow } from "@/gen/miniappWire";
import { useCachedList } from "@/cachedList";
import { APPROVALS_RPC } from "@/resources";
import { addDays, dayLabel, errText, startOfDay } from "@/format";
import { analyzeApproval, cachedApprovalAnalysis, fetchApprovalBody } from "@/gateway";
import { useAction } from "@/useAction";
import { useAsyncOnOpen } from "@/useAsyncOnOpen";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { DayPager } from "@/components/DayPager";
import { Column, Grid, GridNotice } from "@/components/Grid";
import { Markdown } from "@/components/Markdown";

// Recent 전체 결재 snapshot; day-pager filters client-side (Amaranth list has no
// date-range API). Mirrors mail/feed lookback so empty days never trap the pager.
const APPROVALS_LIMIT = 100;
const APPROVALS_LOOKBACK_DAYS = 31;
const HOT_IMPORTANCE = /urgent|high|중요|긴급|priority/i;

/** Parse Amaranth date stamps (2026-07-16 / 2026.07.16 / 20260716) → local midnight ms. */
export function approvalDayMs(date?: string): number | null {
  const s = (date ?? "").trim();
  const m = /^(\d{4})[-./]?(\d{2})[-./]?(\d{2})/.exec(s);
  if (!m) return null;
  return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3])).getTime();
}

function rowLine(a: GroupwareApprovalRow): string {
  const bits = [a.title ?? "(제목 없음)", a.drafter ? `기안 ${a.drafter}` : "", a.status ?? "", a.docNo ?? ""].filter(
    Boolean,
  );
  return `- ${bits.join(" · ")}`;
}

export function ApprovalsPane() {
  const { connected } = useWorkspace();
  const [dayMs, setDayMs] = useState<number>(() => startOfDay());
  const { result, query } = useCachedList<GroupwareApprovalRow & { id?: string }>("approvals", connected, {
    meta: { rpcParams: { folder: "total", limit: APPROVALS_LIMIT } },
  });
  // id alias for Refine BaseRecord; plain map (no useMemo) — rows is already a
  // fresh array from the list cache snapshot each render.
  const rows = (result?.data ?? []).map((a) => ({ ...a, id: a.docId ?? a.id }));
  const [selectedId, setSelectedId] = useState<string | undefined>();
  const { run, error, busy } = useAction(() => void query.refetch());

  // eslint-disable-next-line react-hooks/purity -- wall-clock for day pager bounds
  const nowMs = Date.now();
  const todayMs = startOfDay(nowMs);

  // Inline filter like WorkfeedPane — avoid useMemo+todayMs (React Compiler
  // preserve-manual-memoization rejects a render-time clock dep).
  const dayRows = rows
    .filter((a) => {
      const ms = approvalDayMs(a.date);
      return (ms ?? todayMs) === dayMs;
    })
    .sort((a, b) => String(b.docId ?? "").localeCompare(String(a.docId ?? "")));

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
          onRowClick={(a) => setSelectedId((cur) => (String(cur) === String(a.docId) ? undefined : String(a.docId)))}
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
  const { cfg, connected } = useWorkspace();
  const docId = String(doc.docId ?? "").trim();
  const [view, setView] = useState<"analysis" | "body">("analysis");
  const [analysis, setAnalysis] = useAsyncOnOpen(
    async () => {
      const cached = await cachedApprovalAnalysis(cfg, docId);
      if (cached?.analysis?.trim()) return cached;
      return analyzeApproval(cfg, docId, {
        title: doc.title,
        drafter: doc.drafter,
        date: doc.date,
      });
    },
    [cfg, docId, doc.title, doc.drafter, doc.date],
    { enabled: connected && Boolean(docId) },
  );
  const [body] = useAsyncOnOpen(
    async () => {
      const r = await fetchApprovalBody(cfg, docId, doc.title);
      return r?.body ?? "";
    },
    [cfg, docId, doc.title],
    { enabled: connected && Boolean(docId) },
  );
  const [analyzing, setAnalyzing] = useState(false);
  const [analysisErr, setAnalysisErr] = useState("");

  async function rerun() {
    setAnalyzing(true);
    setAnalysisErr("");
    try {
      setAnalysis(
        await analyzeApproval(cfg, docId, {
          title: doc.title,
          force: true,
          drafter: doc.drafter,
          date: doc.date,
        }),
      );
    } catch (e) {
      setAnalysisErr(errText(e));
    } finally {
      setAnalyzing(false);
    }
  }

  const meta = [doc.status, doc.drafter && `기안 ${doc.drafter}`, doc.docNo, doc.docId && `id ${doc.docId}`]
    .filter(Boolean)
    .join(" · ");
  const text = analysis?.analysis?.trim() ? analysis.analysis : "";
  const importance = analysis?.importance?.trim();

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

      <div className="mail-view-tabs" role="group" aria-label="결재 보기 방식">
        <button
          className={"mail-view-tab" + (view === "analysis" ? " active" : "")}
          aria-pressed={view === "analysis"}
          onClick={() => setView("analysis")}
        >
          분석
        </button>
        <button
          className={"mail-view-tab" + (view === "body" ? " active" : "")}
          aria-pressed={view === "body"}
          onClick={() => setView("body")}
        >
          본문
        </button>
      </div>

      {view === "analysis" ? (
        <div className="mail-card">
          {(importance || (text && !analyzing)) && (
            <div className="mail-card-head">
              {importance && (
                <span className={"mail-badge" + (HOT_IMPORTANCE.test(importance) ? " hot" : "")}>{importance}</span>
              )}
              {text && !analyzing && (
                <button className="row-btn" onClick={() => void rerun()} disabled={!connected} title="다시 분석">
                  다시 분석
                </button>
              )}
            </div>
          )}
          {analyzing || (connected && analysis === null && !analysisErr) ? (
            <div className="mail-card-line">분석 중… (수십 초 걸릴 수 있어요)</div>
          ) : analysisErr ? (
            <div className="mail-card-line error">
              {analysisErr}{" "}
              <button className="row-btn" onClick={() => void rerun()}>
                다시 시도
              </button>
            </div>
          ) : text ? (
            <Markdown text={text} />
          ) : (
            <div className="mail-card-line">
              분석 없음{" "}
              <button className="row-btn" onClick={() => void rerun()} disabled={!connected}>
                분석하기
              </button>
            </div>
          )}
        </div>
      ) : body === null ? (
        <div className="mail-card-line">본문 불러오는 중…</div>
      ) : body ? (
        <div className="mail-body">
          <Markdown text={body} />
        </div>
      ) : (
        <div className="mail-body mail-detail-empty">본문 없음</div>
      )}
    </section>
  );
}
