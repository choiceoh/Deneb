import { useEffect, useState } from "react";
import { NOTEBOOK_RPC } from "@/resources";
import { projectList } from "@/aiText";
import type { Notebook, NotebookSource, NotebookSummary } from "@/types";
import { fmtDate } from "@/format";
import { useCachedRpc } from "@/useCachedRpc";
import { color, line, muted } from "@/theme";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { Icon } from "@/components/Icon";
import { Field, Modal, ModalFooter } from "@/components/Modal";
import { Markdown } from "@/components/Markdown";

// Notebook (노트북) — a browser over Deneb's deal notebooks (miniapp.notebook.*).
// Each notebook is a 거래 with cited source materials; opening one feeds its
// sources to the AI panel so Deneb answers grounded in that deal (NotebookLM-
// style). You can also create a notebook and pin (add) a citation source.
export function NotebookPane() {
  const { connected, cfg, openWiki } = useWorkspace();
  const { call, callCached, readCache, status } = useCachedRpc(cfg, NOTEBOOK_RESOURCE);
  const [listSnapshot] = useState(() => readCache<NotebookListResponse>(NOTEBOOK_RPC.list));
  const [notebooks, setNotebooks] = useState<NotebookSummary[]>(listSnapshot?.data.notebooks ?? []);
  const [active, setActive] = useState<Notebook | null>(null);
  const [creating, setCreating] = useState(false);
  const [addingSource, setAddingSource] = useState(false);

  // Reload the list and refresh its cache — used after create/add_source so the
  // left rail (and the cached snapshot it paints from) stays current.
  async function loadNotebooks() {
    await callCached<NotebookListResponse>(
      NOTEBOOK_RPC.list,
      {},
      {
        scope: "notebook:list",
        apply: (data) => setNotebooks(data?.notebooks ?? []),
      },
    );
  }

  // Load the notebook list on connect — the left rail, shown immediately. A cached
  // snapshot paints first; the live list overwrites it (and refreshes the cache).
  useEffect(() => {
    if (!connected) return;
    void loadNotebooks();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, cfg.url, cfg.token]);

  async function openNotebook(id: string) {
    await callCached<Notebook>(
      NOTEBOOK_RPC.get,
      { id },
      {
        pending: "불러오는 중…",
        scope: "notebook:detail",
        apply: setActive,
      },
    );
  }

  async function createNotebook(name: string, description: string) {
    const r = await call<NotebookSummary>(
      NOTEBOOK_RPC.create,
      { name: name.trim(), description: description.trim() },
      "생성 중…",
    );
    if (!r.ok) return;
    setCreating(false);
    await loadNotebooks();
    void openNotebook(r.data.id); // open the fresh notebook so the user can pin sources
  }

  async function addSource(src: NewSource) {
    if (!active) return;
    const r = await call(
      NOTEBOOK_RPC.addSource,
      { id: active.id, kind: src.kind, title: src.title, text: src.text, ref: src.ref },
      "추가 중…",
    );
    if (!r.ok) return;
    setAddingSource(false);
    await openNotebook(active.id); // reload to show the new source
    void loadNotebooks(); // refresh the list's source count
  }

  // Project the open notebook's sources (or the list) to the AI — ask Deneb about
  // this deal's materials directly. This is the "LM" half of the notebook.
  const aiText = active
    ? `[노트북 ${active.name}]\n` +
      (active.sources ?? [])
        .map((s) => {
          const head = `- [${s.cite ?? "?"}] ${s.title ?? ""}${s.kind ? ` (${s.kind})` : ""}`;
          return s.text ? `${head}\n  ${s.text}` : head;
        })
        .join("\n")
    : projectList(
        `[노트북 ${notebooks.length}개]`,
        notebooks,
        (n) => `- ${n.name}${n.sourceCount ? ` · 자료 ${n.sourceCount}` : ""}`,
      );
  useRegisterPane(NOTEBOOK_RESOURCE, aiText);

  return (
    <div style={{ display: "grid", gridTemplateColumns: "260px 1fr", gap: 12, height: "100%" }}>
      <div style={{ borderRight: line, paddingRight: 12, overflow: "auto" }}>
        <div style={{ display: "flex", alignItems: "center", gap: 6, margin: "2px 0 8px 2px" }}>
          <span className="micro">노트북</span>
          <button
            className="row-btn"
            onClick={() => setCreating(true)}
            disabled={!connected}
            aria-label="새 노트북"
            title="새 노트북"
            style={{ marginLeft: "auto", padding: "2px 7px", display: "inline-flex", alignItems: "center", gap: 3 }}
          >
            <Icon name="plus" size={12} /> 노트북
          </button>
        </div>
        {!connected ? (
          <p style={muted}>게이트웨이에 연결하세요.</p>
        ) : notebooks.length === 0 ? (
          <p style={muted}>노트북이 없습니다. “＋ 노트북”으로 만드세요.</p>
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
              <button
                className="row-btn"
                onClick={() => setAddingSource(true)}
                aria-label="인용자료 추가"
                title="인용자료 추가"
                style={{ marginLeft: "auto", padding: "3px 8px", display: "inline-flex", alignItems: "center", gap: 3 }}
              >
                <Icon name="plus" size={12} /> 인용자료
              </button>
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
              <p style={muted}>아직 자료가 없습니다. “＋ 인용자료”로 추가하세요.</p>
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

      {creating && (
        <CreateNotebookModal
          onClose={() => setCreating(false)}
          onCreate={(name, desc) => void createNotebook(name, desc)}
        />
      )}
      {addingSource && active && (
        <AddSourceModal
          notebook={active.name}
          onClose={() => setAddingSource(false)}
          onAdd={(src) => void addSource(src)}
        />
      )}
    </div>
  );
}

const NOTEBOOK_RESOURCE = "notebook";

// A notebook source to pin: a pasted "note" (text) or a "wiki" page (ref = path).
// Mirrors the gateway's notebook source kinds (KindNote / KindWiki).
type NewSource = { kind: "note" | "wiki"; title: string; text?: string; ref?: string };

const KIND_LABEL: Record<string, string> = { note: "노트", wiki: "위키" };

interface NotebookListResponse {
  notebooks?: NotebookSummary[];
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
        <span style={{ fontWeight: 600, fontSize: 14, minWidth: 0 }}>
          {source.title || source.ref || "(제목 없음)"}
        </span>
        {source.kind && (
          <span style={{ marginLeft: "auto", flex: "0 0 auto", fontSize: 11, color: "var(--muted-2)" }}>
            {KIND_LABEL[source.kind] ?? source.kind}
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

// Create a new (unanchored) notebook via miniapp.notebook.create.
function CreateNotebookModal({
  onClose,
  onCreate,
}: {
  onClose: () => void;
  onCreate: (name: string, desc: string) => void;
}) {
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const submit = () => name.trim() && onCreate(name, desc);
  return (
    <Modal
      title="새 노트북"
      onClose={onClose}
      width={460}
      footer={<ModalFooter action="생성" canSubmit={Boolean(name.trim())} onClose={onClose} onSubmit={submit} />}
    >
      <Field label="이름">
        <input
          className="field"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="예: 카이엠 2차 계약"
          autoFocus
          onKeyDown={(e) => {
            if (e.key === "Enter") submit();
          }}
        />
      </Field>
      <Field label="설명 (선택)">
        <input className="field" value={desc} onChange={(e) => setDesc(e.target.value)} />
      </Field>
    </Modal>
  );
}

// Pin a citation source via miniapp.notebook.add_source — a pasted note (text) or
// a wiki page (ref = path); the kind picker switches the input below.
function AddSourceModal({
  notebook,
  onClose,
  onAdd,
}: {
  notebook: string;
  onClose: () => void;
  onAdd: (src: NewSource) => void;
}) {
  const [kind, setKind] = useState<"note" | "wiki">("note");
  const [title, setTitle] = useState("");
  const [text, setText] = useState("");
  const [ref, setRef] = useState("");
  const canAdd = kind === "note" ? text.trim().length > 0 : ref.trim().length > 0;
  const add = () => {
    if (!canAdd) return;
    onAdd(kind === "note" ? { kind, title: title.trim(), text } : { kind, title: title.trim(), ref: ref.trim() });
  };
  return (
    <Modal
      title={`인용자료 추가 — ${notebook}`}
      onClose={onClose}
      width={560}
      footer={<ModalFooter action="추가" canSubmit={canAdd} onClose={onClose} onSubmit={add} />}
    >
      <Field label="종류">
        <div style={{ display: "flex", gap: 6 }}>
          {(
            [
              ["note", "노트"],
              ["wiki", "위키 페이지"],
            ] as const
          ).map(([k, label]) => (
            <button
              key={k}
              type="button"
              className={"btn" + (kind === k ? " btn-accent" : "")}
              onClick={() => setKind(k)}
              style={{ flex: 1 }}
            >
              {label}
            </button>
          ))}
        </div>
      </Field>
      <Field label="제목 (선택)">
        <input className="field" value={title} onChange={(e) => setTitle(e.target.value)} autoFocus />
      </Field>
      {kind === "note" ? (
        <Field label="내용">
          <textarea
            className="field"
            value={text}
            onChange={(e) => setText(e.target.value)}
            rows={8}
            placeholder="메일 본문·견적·메모 등 인용할 텍스트를 붙여넣으세요."
            style={{ resize: "vertical", fontFamily: "inherit", lineHeight: 1.5 }}
          />
        </Field>
      ) : (
        <Field label="위키 경로">
          <input
            className="field"
            value={ref}
            onChange={(e) => setRef(e.target.value)}
            placeholder="예: 프로젝트/topsolar.md"
            onKeyDown={(e) => {
              if (e.key === "Enter") add();
            }}
          />
        </Field>
      )}
    </Modal>
  );
}
