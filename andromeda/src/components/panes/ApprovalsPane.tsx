import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { GroupwareApprovalRow } from "@/gen/miniappWire";
import { useCachedList } from "@/cachedList";
import { matchEntities } from "@/entityMatch";
import { APPROVALS_RPC } from "@/resources";
import type { Person, ProjectDigest } from "@/types";
import { addDays, dayLabel, errText, startOfDay } from "@/format";
import {
  analyzeApproval,
  approvalAttachmentUrl,
  cachedApprovalAnalysis,
  fetchApprovalAttachment,
  fetchApprovalBody,
  fetchGatewayBlob,
} from "@/gateway";
import { parseApprovalDocBody, parseAttachmentRows } from "@/approvalBody";
import { useAction } from "@/useAction";
import { useAsyncOnOpen } from "@/useAsyncOnOpen";
import { usePaneTarget } from "@/usePaneTarget";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { DayPager } from "@/components/DayPager";
import { Column, Grid, GridNotice } from "@/components/Grid";
import { FileViewer } from "@/components/FileViewer";
import { viewKindFor } from "@/components/fileView";
import { Markdown } from "@/components/Markdown";
import { ConfirmModal } from "@/components/ConfirmModal";
import { Field, Modal } from "@/components/Modal";
import { approvalDayMs } from "../../approvalBody";

// Recent 전체 결재 snapshot; day-pager filters client-side (Amaranth list has no
// date-range API). Mirrors mail/feed lookback so empty days never trap the pager.
const APPROVALS_LIMIT = 100;
const APPROVALS_LOOKBACK_DAYS = 31;
const HOT_IMPORTANCE = /urgent|high|중요|긴급|priority/i;

function rowLine(a: GroupwareApprovalRow): string {
  const bits = [a.title ?? "(제목 없음)", a.drafter ? `기안 ${a.drafter}` : "", a.status ?? "", a.docNo ?? ""].filter(
    Boolean,
  );
  return `- ${bits.join(" · ")}`;
}

export function ApprovalsPane() {
  const { connected, splitWiki } = useWorkspace();
  const [dayMs, setDayMs] = useState<number>(() => startOfDay());
  const [pendingOnly, setPendingOnly] = useState(false);
  const { result, query } = useCachedList<GroupwareApprovalRow & { id?: string }>("approvals", connected, {
    meta: { rpcParams: { folder: "total", limit: APPROVALS_LIMIT } },
  });
  // id alias for Refine BaseRecord; plain map (no useMemo) — rows is already a
  // fresh array from the list cache snapshot each render.
  const rows = (result?.data ?? []).map((a) => ({ ...a, id: a.docId ?? a.id }));
  const [selectedId, setSelectedId] = useState<string | undefined>();
  const { run, error, busy } = useAction(() => void query.refetch());
  // 자동 랜딩 원샷 플래그 — 아래 렌더 조정 블록과 date 점프 타깃이 공유한다.
  const [landed, setLanded] = useState(false);

  // 결재 검토 모드: 미결(canAct) 문서를 열면 제목과 매칭되는 프로젝트/인물
  // 위키를 옆 타일로 자동 배치 — 승인·반려 근거가 클릭 없이 보인다.
  // 기안자 이름은 매칭 대상이 아니다: 기안자는 거의 항상 인물 위키에 있어
  // 문서를 열 때마다 기안자 프로필이 떠 "결재 내용 대신 사람 정보"로 읽혔다.
  // 근거는 문서가 다루는 대상(제목)에서만 찾는다. 읽기 전용 조종(위키 열람)
  // 뿐이라 수용 게이트와 무관. 문서당 1회만, 매칭 실패 시 조용히 아무것도 안 한다.
  const reviewProjects = useCachedList<ProjectDigest>("progress", connected);
  const reviewPeople = useCachedList<Person>("people", connected);
  const reviewedRef = useRef(new Set<string>());
  const selForReview = selectedId != null ? rows.find((a) => String(a.id) === String(selectedId)) : undefined;
  const reviewTitle = selForReview?.canAct ? (selForReview.title ?? "") : "";
  useEffect(() => {
    if (!selectedId || !reviewTitle.trim()) return;
    const key = String(selectedId);
    if (reviewedRef.current.has(key)) return;
    const projects = (reviewProjects.result?.data ?? []).map((d) => ({ name: d.project, path: d.path }));
    const people = (reviewPeople.result?.data ?? []).map((p) => ({ name: p.name, path: p.wikiPath }));
    if (projects.length === 0 && people.length === 0) return; // 목록 로드 전 — 다음 렌더에서 재시도
    reviewedRef.current.add(key);
    const hit = matchEntities(reviewTitle, { projects, people }, 1)[0];
    if (hit) splitWiki(hit.path);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedId, reviewTitle, reviewProjects.result, reviewPeople.result]);

  // Deep link (오늘 KPI/섹션 · workspace 커맨드): query="pending" opens the 미결
  // inbox; an id selects that document (waits for rows to load — return false
  // keeps the target pending until data arrives).
  usePaneTarget(
    "approvals",
    useCallback(
      (target) => {
        if (target.query === "pending") setPendingOnly(true);
        if (target.date) {
          const ms = new Date(`${target.date}T00:00:00`).getTime();
          if (Number.isFinite(ms)) {
            setLanded(true); // 날짜 점프는 자동 랜딩보다 우선
            setDayMs(startOfDay(ms));
            // 미결만 모드는 day-pager를 숨긴다 — 명시적 pending 요청이 아니면
            // 날짜 점프가 보이도록 해제.
            if (target.query !== "pending") setPendingOnly(false);
          }
        }
        if (target.id != null) {
          if (!rows.some((a) => String(a.id) === String(target.id))) return rows.length === 0 ? false : undefined;
          setSelectedId(String(target.id));
        }
      },
      // rows is rebuilt each render; keying on its length is enough for the
      // "retry once data lands" contract without thrashing the effect.
      // eslint-disable-next-line react-hooks/exhaustive-deps
      [rows.length],
    ),
  );

  // eslint-disable-next-line react-hooks/purity -- wall-clock for day pager bounds
  const nowMs = Date.now();
  const todayMs = startOfDay(nowMs);

  // Land on the latest day that actually has documents: entering on an empty
  // "오늘 0건" while the cockpit shows a full approval list reads broken. One
  // shot on first data, render-phase adjustment (same pattern as useSessions
  // prevConnected); only while still parked on today — manual paging wins.
  if (!landed && rows.length > 0) {
    setLanded(true);
    if (dayMs === todayMs) {
      const days = rows.map((a) => approvalDayMs(a.date) ?? todayMs);
      if (!days.includes(todayMs)) {
        const past = days.filter((d) => d < todayMs);
        const target = past.length > 0 ? Math.max(...past) : Math.min(...days);
        if (Number.isFinite(target)) setDayMs(target);
      }
    }
  }

  // Inline filter like WorkfeedPane — avoid useMemo+todayMs (React Compiler
  // preserve-manual-memoization rejects a render-time clock dep).
  const dayRows = rows
    .filter((a) => {
      const ms = approvalDayMs(a.date);
      return (ms ?? todayMs) === dayMs;
    })
    .sort((a, b) => {
      // 미결 first, then newest docId.
      if (a.canAct !== b.canAct) return a.canAct ? -1 : 1;
      return String(b.docId ?? "").localeCompare(String(a.docId ?? ""));
    });

  // 미결만: cross-day inbox — a pending doc from days ago must not hide
  // behind the day pager.
  const pendingRows = rows
    .filter((a) => a.canAct)
    .sort((a, b) => String(b.docId ?? "").localeCompare(String(a.docId ?? "")));
  const shownRows = pendingOnly ? pendingRows : dayRows;

  const pendingCount = dayRows.filter((a) => a.canAct).length;

  const itemDays = rows.map((a) => approvalDayMs(a.date) ?? todayMs);
  const minDayMs = Math.min(addDays(todayMs, -APPROVALS_LOOKBACK_DAYS), ...itemDays, todayMs);
  const maxDayMs = Math.max(todayMs, ...itemDays);

  const aiText = pendingOnly
    ? `[결재 · 미결만]\n` + (pendingRows.length ? pendingRows.map(rowLine).join("\n") : "(미결 문서가 없습니다)")
    : `[결재 · ${dayLabel(dayMs, nowMs)}]\n` +
      (dayRows.length ? dayRows.map(rowLine).join("\n") : "(이 날짜에는 결재 문서가 없습니다)");
  useRegisterPane("approvals", aiText);

  function goToDay(next: number) {
    setDayMs(next);
    setSelectedId(undefined);
  }

  const [rejectDoc, setRejectDoc] = useState<GroupwareApprovalRow | null>(null);
  const [rejectComment, setRejectComment] = useState("");

  // App-styled confirm (window.confirm의 OS 다이얼로그 대체) — 승인은 그룹웨어에
  // 즉시 반영되는 불가역 액션이라 확인은 유지한다.
  const [approving, setApproving] = useState<GroupwareApprovalRow | null>(null);

  async function actApprove(doc: GroupwareApprovalRow) {
    const id = String(doc.docId ?? "").trim();
    if (!id) return;
    const result = await run(APPROVALS_RPC.act, { docId: id, decision: "approve" });
    if (result !== undefined) setSelectedId(undefined);
  }

  function openReject(doc: GroupwareApprovalRow) {
    setRejectComment("");
    setRejectDoc(doc);
  }

  function closeReject() {
    if (busy) return;
    setRejectDoc(null);
    setRejectComment("");
  }

  async function submitReject() {
    if (!rejectDoc || busy) return;
    const id = String(rejectDoc.docId ?? "").trim();
    if (!id) return;
    const comment = rejectComment.trim();
    const params: Record<string, unknown> = { docId: id, decision: "reject" };
    if (comment) params.comment = comment;
    const result = await run(APPROVALS_RPC.act, params);
    if (result !== undefined) {
      closeReject();
      setSelectedId(undefined);
    }
  }

  const columns: Column<GroupwareApprovalRow>[] = [
    {
      header: "상태",
      width: 88,
      tdStyle: { verticalAlign: "top" },
      cell: (a) => {
        const label = a.canAct ? "미결" : a.status?.trim() || "—";
        return <span className={"approval-status" + (a.canAct ? " pending" : "")}>{label}</span>;
      },
    },
    {
      header: "제목",
      cell: (a) => (
        <div>
          <div style={{ fontWeight: a.canAct ? 600 : undefined }}>{a.title || "(제목 없음)"}</div>
          {(a.drafter || a.docNo || (pendingOnly && a.date)) && (
            <div style={{ fontSize: 12, opacity: 0.65, marginTop: 2 }}>
              {[a.drafter && `기안 ${a.drafter}`, pendingOnly ? a.date : "", a.docNo].filter(Boolean).join(" · ")}
            </div>
          )}
        </div>
      ),
    },
  ];

  const selected = shownRows.find((a) => String(a.docId) === String(selectedId));

  return (
    <>
      <h2 style={{ marginTop: 2 }}>결재</h2>
      <p className="groupware-lede">
        {pendingCount > 0
          ? `이 날 미결 ${pendingCount}건 · 행을 열어 분석·승인/반려`
          : "날짜를 옮기며 문서를 보고, 미결은 펼쳐서 승인·반려"}
      </p>
      {error && <p className="pane-error">오류: {error}</p>}
      <div className="approval-filter-row">
        <button
          type="button"
          className={"row-btn" + (pendingOnly ? " active" : "")}
          aria-pressed={pendingOnly}
          onClick={() => {
            setPendingOnly((v) => !v);
            setSelectedId(undefined);
          }}
        >
          {pendingRows.length > 0 ? `미결만 ${pendingRows.length}` : "미결만"}
        </button>
        {pendingOnly && <span className="groupware-status">전체 기간의 미결 문서</span>}
      </div>
      {connected && !pendingOnly && (
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
      <GridNotice
        query={query}
        count={shownRows.length}
        empty={pendingOnly ? "미결 문서가 없습니다." : "이 날짜에는 결재 문서가 없습니다."}
      >
        <Grid
          columns={columns}
          rows={shownRows}
          getKey={(a) => String(a.docId ?? "")}
          onRowClick={(a) => setSelectedId((cur) => (String(cur) === String(a.docId) ? undefined : String(a.docId)))}
          isRowSelected={(a) => String(a.docId) === String(selectedId)}
          rowTitle={(a) => a.title ?? "(제목 없음)"}
          renderExpandedRow={() =>
            selected ? (
              <ApprovalDetail
                doc={selected}
                busy={busy}
                onApprove={() => setApproving(selected)}
                onReject={() => openReject(selected)}
                onClose={() => setSelectedId(undefined)}
              />
            ) : null
          }
        />
      </GridNotice>
      {approving && (
        <ConfirmModal
          title="결재 승인"
          message={`${approving.title || "이 결재 문서"} (doc ${String(approving.docId ?? "")})\n그룹웨어에 즉시 반영됩니다.`}
          action="승인"
          onConfirm={() => void actApprove(approving)}
          onClose={() => setApproving(null)}
        />
      )}
      {rejectDoc && (
        <Modal
          title="결재 반려"
          onClose={closeReject}
          footer={
            <>
              <button className="btn" onClick={closeReject} disabled={busy}>
                취소
              </button>
              <button className="btn btn-accent" onClick={() => void submitReject()} disabled={busy}>
                {busy ? "반려 중…" : "반려"}
              </button>
            </>
          }
        >
          <div className="workfeed-detail-meta">문서</div>
          <div className="workfeed-detail-title">{rejectDoc.title || "이 결재 문서"}</div>
          {rejectDoc.docId && <p className="pane-status">doc {rejectDoc.docId}</p>}
          <p>그룹웨어에 즉시 반영됩니다.</p>
          <Field label="반려 사유 (선택)">
            <textarea
              className="field"
              rows={4}
              value={rejectComment}
              maxLength={500}
              autoFocus
              disabled={busy}
              onChange={(e) => setRejectComment(e.target.value)}
            />
          </Field>
          <div className="pane-status" aria-live="polite">
            {rejectComment.length}/500
          </div>
          {error && (
            <p className="pane-error" role="alert">
              오류: {error}
            </p>
          )}
        </Modal>
      )}
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
  const [lineOpen, setLineOpen] = useState(false);
  const [attachOpen, setAttachOpen] = useState(false);
  // fetch = analysis_cached round-trip; run = analyze after miss (Get+LLM possible).
  // Treating any null as "분석 중… (수십 초)" made cache hits look like cold LLM runs.
  const [analysisPhase, setAnalysisPhase] = useState<"idle" | "fetch" | "run">("idle");
  const analysisLoadGen = useRef(0);
  const [analysis, setAnalysis] = useAsyncOnOpen(
    async () => {
      const gen = ++analysisLoadGen.current;
      const setPhase = (phase: "idle" | "fetch" | "run") => {
        if (analysisLoadGen.current === gen) setAnalysisPhase(phase);
      };
      setPhase("fetch");
      try {
        const cached = await cachedApprovalAnalysis(cfg, docId);
        if (cached?.analysis?.trim()) return cached;
        setPhase("run");
        return await analyzeApproval(cfg, docId, {
          title: doc.title,
          drafter: doc.drafter,
          date: doc.date,
        });
      } finally {
        setPhase("idle");
      }
    },
    [cfg, docId, doc.title, doc.drafter, doc.date],
    { enabled: connected && Boolean(docId) },
  );
  const [body] = useAsyncOnOpen(
    async () => {
      const r = await fetchApprovalBody(cfg, docId, doc.title, doc.folder);
      return r?.body ?? "";
    },
    [cfg, docId, doc.title, doc.folder],
    { enabled: connected && Boolean(docId) },
  );
  const [analyzing, setAnalyzing] = useState(false);
  const [analysisErr, setAnalysisErr] = useState("");
  const [attachBusy, setAttachBusy] = useState("");
  const [attachErr, setAttachErr] = useState("");
  const [attachPreview, setAttachPreview] = useState<{ name: string; text: string } | null>(null);
  const [viewer, setViewer] = useState<{ index: number; name: string } | null>(null);
  const sections = useMemo(() => parseApprovalDocBody(body ?? ""), [body]);
  const attachmentRows = useMemo(() => parseAttachmentRows(sections.attachments), [sections.attachments]);
  const analysisRunning = analyzing || analysisPhase === "run";
  const analysisFetching =
    analysisPhase === "fetch" || (connected && analysis === null && !analysisErr && !analysisRunning);

  async function openAttachment(row: { index: number; name: string }) {
    setAttachBusy(row.name);
    setAttachErr("");
    setAttachPreview(null);
    try {
      const r = await fetchApprovalAttachment(cfg, docId, String(row.index));
      setAttachPreview({ name: row.name, text: r?.text?.trim() || "(내용 없음)" });
    } catch (e) {
      setAttachErr(errText(e));
    } finally {
      setAttachBusy("");
    }
  }

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

  // 문서번호/id are agent plumbing — the header carries 상태·양식·기안·기안일 only.
  const meta = [
    doc.status,
    sections.form,
    (sections.drafter || doc.drafter) && `기안 ${sections.drafter || doc.drafter}`,
    sections.draftedAt || doc.date,
  ]
    .filter(Boolean)
    .join(" · ");
  const text = analysis?.analysis?.trim() ? analysis.analysis : "";
  const importance = analysis?.importance?.trim();
  const lineTeaser = sections.lineCount > 0 ? `${sections.lineCount}명` : "";
  const attachTeaser =
    sections.attachmentCount > 0
      ? `${sections.attachmentCount}건`
      : sections.attachmentHeader
        ? sections.attachmentHeader.replace(/^첨부\s*/, "")
        : "";

  return (
    <section className="workfeed-detail" aria-label="결재 상세">
      <div className="workfeed-detail-head">
        <div className="workfeed-detail-heading">
          <div className="workfeed-detail-meta">{meta}</div>
          <div className="workfeed-detail-title">{sections.title || doc.title || "(제목 없음)"}</div>
        </div>
        <div className="workfeed-detail-actions">
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
          {(importance || (text && !analysisRunning)) && (
            <div className="mail-card-head">
              {importance && (
                <span className={"mail-badge" + (HOT_IMPORTANCE.test(importance) ? " hot" : "")}>{importance}</span>
              )}
              {text && !analysisRunning && (
                <button className="row-btn" onClick={() => void rerun()} disabled={!connected} title="다시 분석">
                  다시 분석
                </button>
              )}
            </div>
          )}
          {analysisRunning ? (
            <div className="mail-card-line">분석 중… (수십 초 걸릴 수 있어요)</div>
          ) : analysisFetching ? (
            <div className="mail-card-line">분석 불러오는 중…</div>
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
      ) : (
        <div className="approval-doc">
          <div className="mail-body">
            {sections.body || body ? (
              <Markdown text={sections.body || body || ""} />
            ) : (
              <div className="mail-detail-empty">본문 없음</div>
            )}
          </div>

          {/* Reference sections live below the read: 결재선·첨부 fold at the bottom. */}
          {sections.line ? (
            <div className="mail-card">
              <button
                className={"mail-card-disclosure" + (lineOpen ? " open" : "")}
                onClick={() => setLineOpen((v) => !v)}
                aria-expanded={lineOpen}
                title={lineOpen ? "결재선 접기" : "결재선 펼치기"}
              >
                <span className="mail-card-title">결재선</span>
                {!lineOpen && lineTeaser ? <span className="mail-card-teaser">{lineTeaser}</span> : null}
                <span className="mail-card-caret">{lineOpen ? "▾" : "▸"}</span>
              </button>
              {lineOpen ? (
                <div className="mail-body approval-doc-block">
                  <Markdown text={sections.line} />
                </div>
              ) : null}
            </div>
          ) : null}

          {sections.attachments ? (
            <div className="mail-card">
              <button
                className={"mail-card-disclosure" + (attachOpen ? " open" : "")}
                onClick={() => setAttachOpen((v) => !v)}
                aria-expanded={attachOpen}
                title={attachOpen ? "첨부 접기" : "첨부 펼치기"}
              >
                <span className="mail-card-title">첨부</span>
                {!attachOpen && attachTeaser ? <span className="mail-card-teaser">{attachTeaser}</span> : null}
                <span className="mail-card-caret">{attachOpen ? "▾" : "▸"}</span>
              </button>
              {attachOpen ? (
                <div className="mail-body approval-doc-block">
                  {attachmentRows.length ? (
                    <div className="mail-attachments">
                      {attachmentRows.map((row) => {
                        const previewable = viewKindFor(row.name) !== "none";
                        return previewable ? (
                          <button
                            key={row.index}
                            type="button"
                            className="mail-attachment"
                            disabled={!connected}
                            onClick={() => setViewer({ index: row.index, name: row.name })}
                            title="첨부 미리보기"
                          >
                            <span>{row.name}</span>
                            <span>{row.meta || "열기"}</span>
                          </button>
                        ) : (
                          <a
                            key={row.index}
                            className="mail-attachment"
                            href={approvalAttachmentUrl(cfg, docId, String(row.index), { filename: row.name })}
                            target="_blank"
                            rel="noreferrer"
                            download={row.name}
                            title="첨부 다운로드"
                          >
                            <span>{row.name}</span>
                            <span>{row.meta || "다운로드"}</span>
                          </a>
                        );
                      })}
                    </div>
                  ) : (
                    <Markdown text={sections.attachments} />
                  )}
                  {attachErr ? <div className="mail-card-line error">{attachErr}</div> : null}
                  {attachBusy ? <div className="mail-card-line">추출 중… {attachBusy}</div> : null}
                  {attachPreview ? (
                    <div className="mail-card" style={{ marginTop: 8 }}>
                      <div className="mail-card-head">
                        <span className="mail-card-title">{attachPreview.name} · 추출</span>
                        <button className="row-btn" type="button" onClick={() => setAttachPreview(null)}>
                          닫기
                        </button>
                      </div>
                      <div className="mail-body approval-doc-block">
                        <Markdown text={attachPreview.text} />
                      </div>
                    </div>
                  ) : null}
                  {viewer ? (
                    <div className="mail-attachment-preview">
                      <FileViewer
                        name={viewer.name}
                        load={() =>
                          fetchGatewayBlob(
                            approvalAttachmentUrl(cfg, docId, String(viewer.index), { filename: viewer.name }),
                          )
                        }
                        downloadUrl={approvalAttachmentUrl(cfg, docId, String(viewer.index), { filename: viewer.name })}
                      />
                      <div className="mail-card-head" style={{ marginTop: 8 }}>
                        <button className="row-btn" type="button" onClick={() => setViewer(null)}>
                          미리보기 닫기
                        </button>
                        <button
                          className="row-btn"
                          type="button"
                          disabled={!connected || Boolean(attachBusy)}
                          onClick={() => void openAttachment(viewer)}
                        >
                          텍스트 추출
                        </button>
                      </div>
                    </div>
                  ) : null}
                </div>
              ) : null}
            </div>
          ) : null}
        </div>
      )}

      {doc.canAct ? (
        <div className="approval-act-bar" role="group" aria-label="결재 처리">
          <button className="btn" disabled={busy} onClick={onApprove}>
            승인
          </button>
          <button className="btn" disabled={busy} onClick={onReject}>
            반려
          </button>
        </div>
      ) : null}
    </section>
  );
}
