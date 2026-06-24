import { useCallback, useState } from "react";
import type { WorkItem } from "@/types";
import { useCachedList } from "@/cachedList";
import { chatStream } from "@/gateway";
import { WORKFEED_RPC } from "@/resources";
import { dayKey, dayLabel, fmtDate, fmtTime } from "@/format";
import { usePaneTarget } from "@/usePaneTarget";
import { useAction } from "@/useAction";
import { useRegisterPane, useWorkspace, type PaneTarget } from "@/workspaceContext";
import { Column, Grid, GridNotice } from "@/components/Grid";
import { AssistantText } from "@/components/DenebUi";

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

interface WorkfeedDay {
  key: string;
  label: string;
  items: WorkItem[];
}

// One line per item — shared by the AI text projection so each day's rows read the
// same as the old flat list, just nested under a day heading.
function itemLine(w: WorkItem): string {
  return `- ${w.title ?? "(항목)"}${w.source ? ` [${w.source}]` : ""}${w.body ? `\n    ${w.body}` : ""}`;
}

// Group feed items into local-day sections, newest day first and newest item first
// within a day. Items with no timestamp collect into a trailing "날짜 미정" group so
// they don't all fall onto the epoch day. `now` flows to dayLabel for the 오늘/어제
// relative headings (injectable for tests).
function groupByDay(items: WorkItem[], now = Date.now()): WorkfeedDay[] {
  const dated = items.filter((w) => typeof w.createdAtMs === "number");
  const undated = items.filter((w) => typeof w.createdAtMs !== "number");
  dated.sort((a, b) => (b.createdAtMs ?? 0) - (a.createdAtMs ?? 0));

  const groups: WorkfeedDay[] = [];
  const byKey = new Map<string, WorkfeedDay>();
  for (const w of dated) {
    const ms = w.createdAtMs as number;
    const key = dayKey(new Date(ms));
    let group = byKey.get(key);
    if (!group) {
      group = { key, label: dayLabel(ms, now), items: [] };
      byKey.set(key, group);
      groups.push(group);
    }
    group.items.push(w);
  }
  if (undated.length) groups.push({ key: "no-date", label: "날짜 미정", items: undated });
  return groups;
}

type RunFn = (method: string, params?: Record<string, unknown>) => Promise<unknown>;

interface WorkfeedTurn {
  sessionKey?: string;
  prompt?: string;
}

export function WorkfeedPane() {
  const { connected, cfg } = useWorkspace();
  const { result, query } = useCachedList<WorkItem>("workfeed", connected);
  const items = result?.data ?? [];
  const [selectedId, setSelectedId] = useState<string | number | undefined>();
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

  const groups = groupByDay(items);
  const aiText = groups.length
    ? `[작업피드 ${items.length}건]\n` +
      groups.map((g) => `## ${g.label} (${g.items.length}건)\n${g.items.map(itemLine).join("\n")}`).join("\n\n")
    : "";
  useRegisterPane("workfeed", aiText);

  function toggleSelected(w: WorkItem) {
    setSelectedId((current) => (String(current) === String(w.id) ? undefined : w.id));
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
          <div className="workfeed-row-title">{w.title ?? "(항목)"}</div>
          {w.body && <div className="workfeed-row-preview">{previewText(w.body)}</div>}
        </div>
      ),
    },
    {
      header: "시각",
      width: 76,
      tdStyle: { verticalAlign: "top" },
      // Day is shown by the section header, so the row keeps only the time.
      cell: (w) => <span className="workfeed-row-time">{fmtTime(w.createdAtMs)}</span>,
    },
  ];

  return (
    <>
      <h2 style={{ marginTop: 2 }}>작업피드</h2>
      {error && <p className="pane-error">오류: {error}</p>}
      <GridNotice query={query} count={items.length} empty="작업피드가 비어 있습니다.">
        <div className="workfeed-days">
          {groups.map((g) => (
            <section key={g.key} className="workfeed-day-group" aria-label={g.label}>
              <h3 className="workfeed-day">
                {g.label}
                <span className="workfeed-day-count">{g.items.length}</span>
              </h3>
              <Grid
                columns={columns}
                rows={g.items}
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
            </section>
          ))}
        </div>
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
  const hasActions = (w.actions?.length ?? 0) > 0;
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
    <section className="workfeed-detail" aria-label="작업피드 상세">
      <div className="workfeed-detail-head">
        <div className="workfeed-detail-heading">
          <div className="workfeed-detail-meta">{meta}</div>
          <div className="workfeed-detail-title">{w.title ?? "(항목)"}</div>
        </div>
        <div className="workfeed-detail-actions">
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
        <div className="workfeed-detail-body">
          {w.body ? (
            <AssistantText text={w.body} onUiSubmit={ignoreUiSubmit} busy />
          ) : (
            <p className="workfeed-empty-body">본문 없음</p>
          )}
        </div>
        <div className="workfeed-tools">
          {hasActions && (
            <section className="workfeed-tool">
              <div className="workfeed-tool-title">액션</div>
              <div className="workfeed-chips">
                {w.actions?.map((a) => (
                  <button
                    key={a.id}
                    className="chip"
                    disabled={busy}
                    onClick={() => void run(WORKFEED_RPC.actionRun, { itemId: w.id, actionId: a.id })}
                  >
                    {a.label}
                  </button>
                ))}
              </div>
            </section>
          )}
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
