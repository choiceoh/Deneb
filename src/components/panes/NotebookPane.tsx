import { useEffect, useState } from "react";
import { NOTEBOOK_RPC } from "@/resources";
import type { Notebook, NotebookSource, NotebookSummary } from "@/types";
import { fmtDate } from "@/format";
import { useRpc } from "@/useRpc";
import { color, line, muted } from "@/theme";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { Markdown } from "@/components/Markdown";

// Notebook (노트북) — a read-only browser over Deneb's deal notebooks
// (miniapp.notebook.list/get). Each notebook is a 거래 with cited source
// materials; opening one feeds its sources to the AI panel so Deneb answers
// grounded in that deal (NotebookLM-style). Replaces the old scratch doc pane.
export function NotebookPane() {
  const { connected, cfg, openWiki } = useWorkspace();
  const { call, status } = useRpc(cfg);
  const [notebooks, setNotebooks] = useState<NotebookSummary[]>([]);
  const [active, setActive] = useState<Notebook | null>(null);

  // Load the notebook list on connect — the left rail, shown immediately (no
  // search needed, unlike the old wiki).
  useEffect(() => {
    if (!connected) return;
    let cancelled = false;
    void call<{ notebooks?: NotebookSummary[] }>(NOTEBOOK_RPC.list, {}).then((r) => {
      if (!cancelled && r.ok) setNotebooks(r.data?.notebooks ?? []);
    });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, cfg.url, cfg.token]);

  async function openNotebook(id: string) {
    const r = await call<Notebook>(NOTEBOOK_RPC.get, { id }, "불러오는 중…");
    if (r.ok) setActive(r.data);
  }

  // Project the open notebook's sources (or the list) to the AI — ask Deneb about
  // this deal's materials directly. This is the "LM" half of the notebook.
  const aiText = active
    ? `[노트북 ${active.name}]\n` +
      (active.sources ?? [])
        .map(
          (s) => `- [${s.cite ?? "?"}] ${s.title ?? ""}${s.kind ? ` (${s.kind})` : ""}${s.text ? `\n  ${s.text}` : ""}`,
        )
        .join("\n")
    : notebooks.length
      ? `[노트북 ${notebooks.length}개]\n` +
        notebooks.map((n) => `- ${n.name}${n.sourceCount ? ` · 자료 ${n.sourceCount}` : ""}`).join("\n")
      : "";
  useRegisterPane(undefined, aiText);

  return (
    <div style={{ display: "grid", gridTemplateColumns: "260px 1fr", gap: 12, height: "100%" }}>
      <div style={{ borderRight: line, paddingRight: 12, overflow: "auto" }}>
        <div className="micro" style={{ margin: "2px 0 8px 2px" }}>
          노트북
        </div>
        {!connected ? (
          <p style={muted}>게이트웨이에 연결하세요.</p>
        ) : notebooks.length === 0 ? (
          <p style={muted}>노트북이 없습니다.</p>
        ) : (
          notebooks.map((n) => (
            <button
              key={n.id}
              onClick={() => void openNotebook(n.id)}
              style={{
                display: "block",
                width: "100%",
                textAlign: "left",
                background: active?.id === n.id ? color.active : "transparent",
                border: "none",
                borderRadius: 5,
                padding: "7px 8px",
                cursor: "pointer",
              }}
            >
              <div
                style={{
                  fontSize: 13,
                  color: color.text,
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                }}
              >
                {n.name}
              </div>
              <div style={{ fontSize: 11, color: "var(--muted-2)", marginTop: 1 }}>
                {[n.sourceCount ? `자료 ${n.sourceCount}` : "", fmtDate(n.updated)].filter(Boolean).join(" · ")}
              </div>
            </button>
          ))
        )}
      </div>

      <div style={{ overflow: "auto", minWidth: 0 }}>
        {!active ? (
          <p style={{ ...muted, fontSize: 13, marginTop: 2, lineHeight: 1.6 }}>
            {connected ? "왼쪽에서 노트북을 선택하면 자료가 여기에 표시됩니다." : "게이트웨이 연결 대기 중"}
          </p>
        ) : (
          <>
            <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 4 }}>
              <h2 style={{ margin: 0, fontSize: 21 }}>{active.name}</h2>
              {active.dealRef && (
                <button
                  className="row-btn"
                  onClick={() => openWiki(active.dealRef as string)}
                  title="딜 페이지 열기"
                  style={{ padding: "3px 8px" }}
                >
                  딜 페이지 →
                </button>
              )}
              {status && <span className="pane-status">{status}</span>}
            </div>
            <div style={{ fontSize: 12, color: "var(--muted-2)", marginBottom: 14 }}>
              자료 {(active.sources ?? []).length}건{active.updated ? ` · ${fmtDate(active.updated)}` : ""}
            </div>
            {(active.sources ?? []).length === 0 ? (
              <p style={muted}>자료가 없습니다.</p>
            ) : (
              <div style={{ display: "flex", flexDirection: "column", gap: 12, maxWidth: 820 }}>
                {(active.sources ?? []).map((s, i) => (
                  <SourceCard key={s.cite || s.ref || i} source={s} />
                ))}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

// One cited source inside a notebook: a citation badge + title + kind, then the
// source text rendered as Markdown.
function SourceCard({ source }: { source: NotebookSource }) {
  return (
    <section style={{ border: "1px solid var(--line)", borderRadius: "var(--radius-ctl)", padding: "12px 14px" }}>
      <div style={{ display: "flex", alignItems: "baseline", gap: 8, marginBottom: source.text ? 7 : 0 }}>
        {source.cite && (
          <span
            style={{
              flex: "0 0 auto",
              background: "var(--accent-soft)",
              color: "var(--accent-deep)",
              fontSize: 11,
              fontWeight: 600,
              padding: "1px 7px",
              borderRadius: "var(--radius-pill)",
            }}
          >
            {source.cite}
          </span>
        )}
        <span style={{ fontWeight: 600, fontSize: 14, minWidth: 0 }}>{source.title ?? "(제목 없음)"}</span>
        {source.kind && (
          <span style={{ marginLeft: "auto", flex: "0 0 auto", fontSize: 11, color: "var(--muted-2)" }}>
            {source.kind}
          </span>
        )}
      </div>
      {source.text && (
        <div style={{ fontSize: 13, color: "var(--ink-2)", lineHeight: 1.6 }}>
          <Markdown text={source.text} />
        </div>
      )}
    </section>
  );
}
