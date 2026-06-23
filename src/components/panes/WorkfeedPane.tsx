import { useState } from "react";
import { useList } from "@refinedev/core";
import type { WorkItem } from "@/types";
import { serializeList } from "@/aiText";
import { WORKFEED_RPC } from "@/resources";
import { fmtDate } from "@/format";
import { useAction } from "@/useAction";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { Column, Grid, GridNotice, RowBtn } from "@/components/Grid";

// Items sourced from a question expect a free-text reply (workfeed.answer), not
// just an ack — surface an inline answer box for them.
const isQuestion = (w: WorkItem) => (w.source ?? "").includes("question");

export function WorkfeedPane() {
  const { connected } = useWorkspace();
  const { result, query } = useList<WorkItem>({ resource: "workfeed", queryOptions: { enabled: connected } });
  const items = result?.data ?? [];
  const { run, error, busy } = useAction(() => void query.refetch());

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
          {w.actions && w.actions.length > 0 && (
            <div style={{ display: "flex", gap: 5, marginTop: 6, flexWrap: "wrap" }}>
              {w.actions.map((a) => (
                <button
                  key={a.id}
                  className="chip"
                  disabled={busy}
                  onClick={() => run(WORKFEED_RPC.actionRun, { itemId: w.id, actionId: a.id })}
                >
                  {a.label}
                </button>
              ))}
            </div>
          )}
          {isQuestion(w) && (
            // answer params best-effort vs the live gateway (mirrors action.run's itemId).
            <AnswerBox busy={busy} onSubmit={(text) => run(WORKFEED_RPC.answer, { itemId: w.id, text })} />
          )}
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
      {error && <p style={{ color: "var(--due)", fontSize: 12, margin: "0 0 8px" }}>오류: {error}</p>}
      <GridNotice query={query} count={items.length} empty="작업피드가 비어 있습니다.">
        <Grid columns={columns} rows={items} getKey={(w) => String(w.id)} />
      </GridNotice>
    </>
  );
}

// Inline free-text reply for a question item. Clears on submit; the parent's
// useAction refetches the feed (an answered item typically drops off).
function AnswerBox({ busy, onSubmit }: { busy: boolean; onSubmit: (text: string) => void }) {
  const [text, setText] = useState("");
  const submit = () => {
    const t = text.trim();
    if (!t) return;
    setText("");
    onSubmit(t);
  };
  return (
    <div style={{ display: "flex", gap: 5, marginTop: 6, maxWidth: 460 }}>
      <input
        className="field"
        style={{ flex: 1, fontSize: 12, padding: "5px 8px" }}
        placeholder="답변 입력…"
        value={text}
        disabled={busy}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") submit();
        }}
      />
      <button className="chip" onClick={submit} disabled={busy || !text.trim()}>
        답변
      </button>
    </div>
  );
}
