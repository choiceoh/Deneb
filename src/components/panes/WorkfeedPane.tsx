import { useEffect, useState } from "react";
import type { WorkItem } from "@/types";
import { serializeList } from "@/aiText";
import { useCachedList } from "@/cachedList";
import { WORKFEED_RPC } from "@/resources";
import { fmtDate } from "@/format";
import { useAction } from "@/useAction";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { Column, Grid, GridNotice, RowBtn } from "@/components/Grid";

// Items sourced from a question expect a free-text reply, surfaced as an inline
// box (sent via workfeed.feedback — the gateway records it and runs a turn).
const isQuestion = (w: WorkItem) => (w.source ?? "").includes("question");

type RunFn = (method: string, params?: Record<string, unknown>) => void;

export function WorkfeedPane() {
  const { connected, consumePaneTarget, paneTarget } = useWorkspace();
  const { result, query } = useCachedList<WorkItem>("workfeed", connected);
  const items = result?.data ?? [];
  const [selectedId, setSelectedId] = useState<string | number | undefined>();
  const { run, error, busy } = useAction(() => void query.refetch());

  useEffect(() => {
    if (paneTarget?.view !== "workfeed" || paneTarget.id === undefined) return;
    setSelectedId(paneTarget.id);
    consumePaneTarget();
  }, [consumePaneTarget, paneTarget]);

  const aiText = serializeList(
    "작업피드",
    items,
    (w) => `- ${w.title ?? "(항목)"}${w.source ? ` [${w.source}]` : ""}${w.body ? `\n    ${w.body}` : ""}`,
  );
  useRegisterPane("workfeed", aiText);

  const columns: Column<WorkItem>[] = [
    {
      header: "출처",
      width: 100,
      tdStyle: { fontSize: 12, opacity: 0.6, whiteSpace: "nowrap" },
      cell: (w) => w.source ?? "",
    },
    {
      header: "항목",
      cell: (w) => (
        <>
          <div style={{ fontWeight: 500 }}>{w.title ?? "(항목)"}</div>
          {w.body && <div style={{ fontSize: 12, color: "var(--muted-2)", lineHeight: 1.45 }}>{w.body}</div>}
          <WorkItemActions w={w} busy={busy} run={run} />
        </>
      ),
    },
    {
      header: "시각",
      width: 120,
      tdStyle: { fontSize: 13, opacity: 0.7, whiteSpace: "nowrap" },
      cell: (w) => fmtDate(w.createdAtMs),
    },
    {
      header: "",
      width: 60,
      tdStyle: { textAlign: "right" },
      cell: (w) => (
        <RowBtn onClick={() => run(WORKFEED_RPC.ack, { id: w.id })} disabled={busy} title="처리(닫기)">
          처리
        </RowBtn>
      ),
    },
  ];

  return (
    <>
      <h2 style={{ marginTop: 2 }}>작업피드</h2>
      {error && <p className="pane-error">오류: {error}</p>}
      <GridNotice query={query} count={items.length} empty="작업피드가 비어 있습니다.">
        <Grid
          columns={columns}
          rows={items}
          getKey={(w) => String(w.id)}
          isRowSelected={(w) => String(w.id) === String(selectedId)}
        />
      </GridNotice>
    </>
  );
}

// Per-item actions: the card's own action chips (action.run), regenerate the
// card (rewrite), and a free-text feedback/answer box (feedback). Question items
// show the box by default; other cards reveal it behind a 정정 toggle.
function WorkItemActions({ w, busy, run }: { w: WorkItem; busy: boolean; run: RunFn }) {
  const question = isQuestion(w);
  const [open, setOpen] = useState(question);
  const [text, setText] = useState("");

  const submit = () => {
    const t = text.trim();
    if (!t) return;
    setText("");
    run(WORKFEED_RPC.feedback, { itemId: w.id, feedback: t });
  };

  return (
    <>
      <div style={{ display: "flex", gap: 5, marginTop: 6, flexWrap: "wrap" }}>
        {w.actions?.map((a) => (
          <button
            key={a.id}
            className="chip"
            disabled={busy}
            onClick={() => run(WORKFEED_RPC.actionRun, { itemId: w.id, actionId: a.id })}
          >
            {a.label}
          </button>
        ))}
        <RowBtn onClick={() => run(WORKFEED_RPC.rewrite, { itemId: w.id })} disabled={busy} title="카드 다시 작성">
          다시 작성
        </RowBtn>
        {!question && (
          <RowBtn onClick={() => setOpen((o) => !o)} disabled={busy} title="정정·피드백 남기기">
            정정
          </RowBtn>
        )}
      </div>
      {open && (
        <div style={{ display: "flex", gap: 5, marginTop: 6, maxWidth: 460 }}>
          <input
            className="field"
            style={{ flex: 1, fontSize: 12, padding: "5px 8px" }}
            placeholder={question ? "답변 입력…" : "정정·피드백 입력…"}
            value={text}
            disabled={busy}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") submit();
            }}
          />
          <button className="chip" onClick={submit} disabled={busy || !text.trim()}>
            {question ? "답변" : "보내기"}
          </button>
        </div>
      )}
    </>
  );
}
