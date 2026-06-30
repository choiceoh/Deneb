import { useCallback, useState } from "react";
import type { WorkItem } from "@/types";
import { useCachedList } from "@/cachedList";
import { callRpc, chatStream } from "@/gateway";
import { WORKFEED_RPC } from "@/resources";
import { dayLabel, fmtDate, fmtTime, startOfDay } from "@/format";
import { usePaneTarget } from "@/usePaneTarget";
import { useAction } from "@/useAction";
import { useRegisterPane, useWorkspace, type PaneTarget } from "@/workspaceContext";
import { Column, Grid, GridNotice } from "@/components/Grid";
import { AssistantText } from "@/components/DenebUi";

// The gateway clamps workfeed.list to 100 (maxWorkFeedLimit); a single day fits well
// under that, so request the full page and let the day-range scope it.
const WORKFEED_DAY_LIMIT = 100;
// How far back the day-pager can step into (possibly empty) days before ‹이전 stops.
// Mirrors the native feed's lookback so a quiet stretch never traps you on today.
const FEED_LOOKBACK_DAYS = 31;

// Local-midnight epoch `delta` days from `dayMs`, computed component-wise so day math
// stays DST-safe (never UTC arithmetic) — same rule as format.startOfDay.
function addDays(dayMs: number, delta: number): number {
  const d = new Date(dayMs);
  return new Date(d.getFullYear(), d.getMonth(), d.getDate() + delta).getTime();
}

// Items sourced from a question expect a free-text reply. The gateway settles the
// card via workfeed.answer/action.run, then returns a sessionKey+prompt to deliver.
const isQuestion = (w: WorkItem) => (w.source ?? "").includes("question");
const ignoreUiSubmit = () => {};

const SOURCE_LABELS: Record<string, string> = {
  alert: "알림",
  deal_question: "질문",
  followup: "후속",
  proactive: "제안",
};

function sourceLabel(source?: string) {
  const key = source?.trim();
  if (!key) return "피드";
  return SOURCE_LABELS[key] ?? key.replace(/[_-]+/g, " ");
}

function previewText(text?: string, max = 120) {
  const raw = (text ?? "").trim();
  if (!raw) return "";
  const hasStructuredBody = /```deneb-ui/i.test(raw) || /^\s*\|.+\|\s*$/m.test(raw);
  const withoutUi = raw.replace(/```deneb-ui[\s\S]*?```/gi, "");
  const line =
    withoutUi
      .split(/\r?\n/)
      .map((part) => part.trim())
      .find((part) => part && !/^\|.*\|$/.test(part) && !/^[-:|\s]+$/.test(part) && !/^```/.test(part)) ?? "";
  const compact = (line || (hasStructuredBody ? "표/도표 포함" : withoutUi))
    .replace(/^\s{0,3}#{1,6}\s+/, "")
    .replace(/^\s*[-*+]\s+/, "")
    .replace(/`([^`]+)`/g, "$1")
    .replace(/\*\*([^*]+)\*\*/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
    .replace(/\s+/g, " ")
    .trim();
  if (!compact) return "";
  return compact.length > max ? `${compact.slice(0, max - 1)}…` : compact;
}

// One line per item — shared by the AI text projection so the day's rows read the
// same as the on-screen list.
function itemLine(w: WorkItem): string {
  return `- ${w.title ?? "(항목)"}${w.source ? ` [${w.source}]` : ""}${w.body ? `\n    ${w.body}` : ""}`;
}

// Effective timestamp for day-bucketing: an item without a createdAtMs falls onto the
// current day (`now`) so a missing stamp never makes it unreachable in the day-pager.
function effectiveMs(w: WorkItem, now: number): number {
  return typeof w.createdAtMs === "number" ? w.createdAtMs : now;
}

type RunFn = (method: string, params?: Record<string, unknown>) => Promise<unknown>;

interface WorkfeedTurn {
  sessionKey?: string;
  prompt?: string;
}

export function WorkfeedPane() {
  const { connected, cfg } = useWorkspace();
  // The day currently in view (local midnight). Lands on today; prev/next step it.
  const [dayMs, setDayMs] = useState<number>(() => startOfDay());
  // Fetch ONLY the selected day's items, server-side ranged (sinceMs..beforeMs) at the
  // gateway's max page, refetched whenever the day changes. The old flat default fetch
  // (limit 20, server order — not newest-first) silently dropped a busy day's later
  // cards past position 20: a 6-mail morning showed just 2 here while the phone (which
  // already ranges by day) showed all of them. The per-day cacheKey snapshots each day
  // separately; the resource stays "workfeed" so sync.ts / useEvents invalidation still
  // refetches the visible day when new cards land.
  const { result, query } = useCachedList<WorkItem>("workfeed", connected, {
    cacheKey: `workfeed.${dayMs}`,
    meta: { rpcParams: { limit: WORKFEED_DAY_LIMIT, sinceMs: dayMs, beforeMs: addDays(dayMs, 1) } },
  });
  const items = result?.data ?? [];
  const [selectedId, setSelectedId] = useState<string | number | undefined>();
  // Optimistically-read ids so a row dims the instant it's opened, before the gateway
  // round-trip lands readAtMs in the cached list on the next refresh.
  const [readIds, setReadIds] = useState<ReadonlySet<string>>(() => new Set());
  const { run, error, busy } = useAction(() => void query.refetch(), {
    onResult: async (data) => {
      const turn = data as WorkfeedTurn;
      const sessionKey = typeof turn?.sessionKey === "string" ? turn.sessionKey.trim() : "";
      const prompt = typeof turn?.prompt === "string" ? turn.prompt.trim() : "";
      if (!sessionKey || !prompt) return;
      let streamError = "";
      await chatStream(
        cfg,
        prompt,
        {
          onError: (err) => {
            streamError = err;
          },
        },
        { sessionKey },
      );
      if (streamError) throw new Error(streamError);
    },
  });

  // An id-less workfeed target is meaningless — keep it pending rather than
  // clearing the current selection.
  const openTargetedItem = useCallback((t: PaneTarget) => {
    if (t.id === undefined) return false;
    setSelectedId(t.id);
  }, []);
  usePaneTarget("workfeed", openTargetedItem);

  const nowMs = Date.now();
  const todayMs = startOfDay(nowMs);
  // Navigation mirrors the native feed: freely steppable back across the lookback
  // window — even into days the current per-day fetch returned empty — so an empty day
  // never traps the user on today. Loaded items only EXTEND the range when they predate
  // the window. Forward stops at today (nothing is newer).
  const itemDays = items.map((w) => startOfDay(effectiveMs(w, nowMs)));
  const minDayMs = Math.min(addDays(todayMs, -FEED_LOOKBACK_DAYS), ...itemDays);
  const maxDayMs = Math.max(todayMs, ...itemDays);

  const dayItems = items
    .filter((w) => startOfDay(effectiveMs(w, nowMs)) === dayMs)
    .sort((a, b) => effectiveMs(b, nowMs) - effectiveMs(a, nowMs));

  const aiText =
    `[피드 · ${dayLabel(dayMs, nowMs)}]\n` +
    (dayItems.length ? dayItems.map(itemLine).join("\n") : "(이 날짜에는 항목이 없습니다)");
  useRegisterPane("workfeed", aiText);

  function goToDay(nextDayMs: number) {
    setDayMs(nextDayMs);
    setSelectedId(undefined); // the selected item likely isn't on the new day
  }

  function stepDay(delta: number) {
    goToDay(addDays(dayMs, delta));
  }

  const isRead = (w: WorkItem) => Boolean(w.readAtMs) || readIds.has(String(w.id));

  function markRead(w: WorkItem) {
    if (isRead(w)) return; // already read — no RPC, no re-dim
    setReadIds((prev) => new Set(prev).add(String(w.id)));
    // Durable read state lives in the gateway; fire-and-forget (dimming is optimistic).
    void callRpc(cfg, WORKFEED_RPC.read, { itemId: w.id }).catch(() => {});
  }

  function toggleSelected(w: WorkItem) {
    const opening = String(selectedId) !== String(w.id);
    setSelectedId(opening ? w.id : undefined);
    if (opening) markRead(w); // opening a card reads it
  }

  async function ackItem(w: WorkItem) {
    const result = await run(WORKFEED_RPC.ack, { id: w.id });
    if (result !== undefined) setSelectedId(undefined);
  }

  const columns: Column<WorkItem>[] = [
    {
      header: "유형",
      width: 92,
      tdStyle: { verticalAlign: "top" },
      cell: (w) => <span className="workfeed-kind">{sourceLabel(w.source)}</span>,
    },
    {
      header: "항목",
      cell: (w) => (
        <div className="workfeed-row-main">
          <div className={isRead(w) ? "workfeed-row-title workfeed-row-read" : "workfeed-row-title"}>
            {w.title ?? "(항목)"}
          </div>
          {w.body && <div className="workfeed-row-preview">{previewText(w.body)}</div>}
        </div>
      ),
    },
    {
      header: "시각",
      width: 76,
      tdStyle: { verticalAlign: "top" },
      // Day is shown by the navigator above, so the row keeps only the time.
      cell: (w) => <span className="workfeed-row-time">{fmtTime(w.createdAtMs)}</span>,
    },
  ];

  return (
    <>
      <h2 style={{ marginTop: 2 }}>피드</h2>
      {error && <p className="pane-error">오류: {error}</p>}
      {/* Day nav lives ABOVE the grid notice so it stays put while a day loads or comes
          back empty — GridNotice swaps its children for a loading/empty notice, and
          burying the pager inside it would strand the user on an empty day with no arrows. */}
      {connected && (
        <div className="workfeed-daynav">
          <button className="row-btn" onClick={() => stepDay(-1)} disabled={dayMs <= minDayMs} aria-label="이전 날">
            ‹ 이전
          </button>
          <div className="workfeed-daynav-label" aria-live="polite">
            <span className="workfeed-daynav-day">{dayLabel(dayMs, nowMs)}</span>
            <span className="workfeed-daynav-count">{dayItems.length}건</span>
          </div>
          <button className="row-btn" onClick={() => stepDay(1)} disabled={dayMs >= maxDayMs} aria-label="다음 날">
            다음 ›
          </button>
          <div className="workfeed-daynav-spacer" />
          {dayMs !== todayMs && (
            <button className="row-btn" onClick={() => goToDay(todayMs)}>
              오늘로
            </button>
          )}
        </div>
      )}
      <GridNotice query={query} count={dayItems.length} empty="이 날짜에는 항목이 없습니다.">
        <Grid
          columns={columns}
          rows={dayItems}
          getKey={(w) => String(w.id)}
          hideHeader
          onRowClick={toggleSelected}
          isRowSelected={(w) => String(w.id) === String(selectedId)}
          rowTitle={(w) => `${w.title ?? "(항목)"} 상세`}
          renderExpandedRow={(w) => (
            <WorkItemDetail
              w={w}
              busy={busy}
              run={run}
              onAck={() => void ackItem(w)}
              onClose={() => setSelectedId(undefined)}
            />
          )}
        />
      </GridNotice>
    </>
  );
}

// Selected-item detail. The row remains scan-only; all mutating actions live here.
function WorkItemDetail({
  w,
  busy,
  run,
  onAck,
  onClose,
}: {
  w: WorkItem;
  busy: boolean;
  run: RunFn;
  onAck: () => void;
  onClose: () => void;
}) {
  const question = isQuestion(w);
  const [text, setText] = useState("");
  const [feedback, setFeedback] = useState("");
  // AI 분석 본문은 기본 전체 펼침; 길면 "분석 접기"로 접는다 (스크롤 박스 대신 토글).
  const [bodyOpen, setBodyOpen] = useState(true);
  const created = fmtDate(w.createdAtMs);
  const meta = [sourceLabel(w.source), created, w.refId ? `ref ${w.refId}` : ""].filter(Boolean).join(" · ");

  const submit = () => {
    const t = text.trim();
    if (!t) return;
    setText("");
    void run(WORKFEED_RPC.answer, { itemId: w.id, answer: t });
  };

  const submitFeedback = () => {
    const t = feedback.trim();
    if (!t) return;
    setFeedback("");
    void run(WORKFEED_RPC.feedback, { itemId: w.id, feedback: t });
  };

  return (
    <section className="workfeed-detail" aria-label="피드 상세">
      <div className="workfeed-detail-head">
        <div className="workfeed-detail-heading">
          <div className="workfeed-detail-meta">{meta}</div>
          <div className="workfeed-detail-title">{w.title ?? "(항목)"}</div>
        </div>
        <div className="workfeed-detail-actions">
          {w.body && (
            <button className="row-btn" onClick={() => setBodyOpen((o) => !o)} aria-expanded={bodyOpen}>
              {bodyOpen ? "분석 접기" : "분석 펼치기"}
            </button>
          )}
          <button className="row-btn" onClick={onClose} disabled={busy}>
            닫기
          </button>
          <button className="btn" onClick={() => void run(WORKFEED_RPC.rewrite, { itemId: w.id })} disabled={busy}>
            다시 작성
          </button>
          <button className="btn btn-accent" onClick={onAck} disabled={busy}>
            처리
          </button>
        </div>
      </div>
      <div className="workfeed-detail-layout">
        {w.body ? (
          bodyOpen && (
            <div className="workfeed-detail-body">
              <AssistantText text={w.body} onUiSubmit={ignoreUiSubmit} busy />
            </div>
          )
        ) : (
          <div className="workfeed-detail-body">
            <p className="workfeed-empty-body">본문 없음</p>
          </div>
        )}
        {/* 본문 하단 풀폭 푸터: 액션 칩은 제거하고 답변(질문 한정)·정정만 남겨 본문을 와이드하게. */}
        <div className="workfeed-tools">
          {question && (
            <section className="workfeed-tool">
              <div className="workfeed-tool-title">답변</div>
              <div className="workfeed-form">
                <textarea
                  className="field"
                  placeholder="답변 입력…"
                  rows={3}
                  value={text}
                  disabled={busy}
                  onChange={(e) => setText(e.target.value)}
                />
                <button className="chip" onClick={submit} disabled={busy || !text.trim()}>
                  답변
                </button>
              </div>
            </section>
          )}
          <section className="workfeed-tool">
            <div className="workfeed-tool-title">정정</div>
            <div className="workfeed-form">
              <textarea
                className="field"
                placeholder="정정·피드백 입력…"
                rows={3}
                value={feedback}
                disabled={busy}
                onChange={(e) => setFeedback(e.target.value)}
              />
              <button className="chip" onClick={submitFeedback} disabled={busy || !feedback.trim()}>
                정정
              </button>
            </div>
          </section>
        </div>
      </div>
    </section>
  );
}
