import { useEffect, useMemo, useState } from "react";
import { NOTEBOOK_RPC } from "@/resources";
import { projectList } from "@/aiText";
import type { Notebook, NotebookSource, NotebookSummary } from "@/types";
import { useCachedRpc } from "@/useCachedRpc";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { Icon } from "@/components/Icon";
import { Field, Modal, ModalFooter } from "@/components/Modal";
import { Markdown } from "@/components/Markdown";
import { DeleteModal } from "./commonModals";

// Notebook (노트북) — a browser over Deneb's deal notebooks (miniapp.notebook.*).
// Each notebook is a 거래 with cited source materials; opening one feeds its
// sources to the AI panel so Deneb answers grounded in that deal (NotebookLM-
// style). You can also create a notebook and pin (add) a citation source.
export function NotebookPane() {
  const { connected, cfg, openWiki, setNoteSink } = useWorkspace();
  const { call, callCached, readCache, writeCache, status } = useCachedRpc(cfg, NOTEBOOK_RESOURCE);
  const [listSnapshot] = useState(() => readCache<NotebookListResponse>(NOTEBOOK_RPC.list));
  const [notebooks, setNotebooks] = useState<NotebookSummary[]>(listSnapshot?.data.notebooks ?? []);
  const [active, setActive] = useState<Notebook | null>(null);
  const [creating, setCreating] = useState(false);
  const [addingSource, setAddingSource] = useState(false);
  const [deleting, setDeleting] = useState<Notebook | null>(null);
  const [deletingSource, setDeletingSource] = useState<NotebookSource | null>(null);
  // Which source chip is expanded into the preview below (by its stable key).
  // "" = all chips folded — the default, so the sources read as a light strip and
  // the pane's height stays with the actual work (the chat below). Goes stale
  // harmlessly on notebook switch / source deletion (preview just closes).
  const [previewKey, setPreviewKey] = useState("");

  // Reload the list and refresh its cache — used after writes so the top picker
  // (and the cached snapshot it paints from) stays current.
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

  // Load the notebook list on connect — feeds the top picker. A cached snapshot
  // paints first; the live list overwrites it (and refreshes the cache).
  useEffect(() => {
    if (!connected) return;
    void loadNotebooks();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, cfg.url, cfg.token]);

  // Most-recently-updated first — drives the top picker and the auto-open default.
  const sortedNotebooks = useMemo(
    () => [...notebooks].sort((a, b) => (b.updated ?? 0) - (a.updated ?? 0)),
    [notebooks],
  );

  // Pick once: auto-open the freshest notebook so the pane lands ready to work. The
  // user rarely switches mid-task; the top dropdown handles the occasional change.
  // Re-runs if the active notebook is deleted (active → null), opening the next one.
  useEffect(() => {
    if (!connected || active || sortedNotebooks.length === 0) return;
    void openNotebook(sortedNotebooks[0].id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sortedNotebooks, connected, active]);

  async function openNotebook(id: string) {
    setPreviewKey(""); // a different notebook's preview key is meaningless
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

  // AI-answer → note sink: while a notebook is open, register a save function so
  // the docked AI panel can offer 노트에 저장 on every finished answer. Saving pins
  // the answer as a kind=note source (title falls back to a first-line snippet) —
  // the notebook's OUTPUT loop: material made with the AI stays in the notebook
  // instead of scrolling away in the chat.
  useEffect(() => {
    if (!connected || !active) {
      setNoteSink(null);
      return;
    }
    const id = active.id;
    setNoteSink((text: string) => {
      void (async () => {
        const r = await call(NOTEBOOK_RPC.addSource, { id, kind: "note", title: "", text }, "노트 저장 중…");
        if (!r.ok) return;
        await openNotebook(id);
        void loadNotebooks();
      })();
    });
    return () => setNoteSink(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, active?.id]);

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

  async function removeSource() {
    if (!active || !deletingSource?.cite) return;
    const id = active.id;
    const r = await call<Notebook>(NOTEBOOK_RPC.removeSource, { id, cite: deletingSource.cite }, "삭제 중…");
    if (!r.ok) return;
    setDeletingSource(null);
    setActive(r.data);
    writeCache(NOTEBOOK_RPC.get, { id }, r.data);
    setNotebooks((current) =>
      current.map((notebook) =>
        notebook.id === id
          ? {
              ...notebook,
              sourceCount: r.data.sources?.length ?? 0,
              updated: r.data.updated ?? notebook.updated,
            }
          : notebook,
      ),
    );
    void loadNotebooks();
  }

  async function deleteNotebook() {
    if (!deleting) return;
    const id = deleting.id;
    const r = await call<{ deleted?: boolean; id?: string }>(NOTEBOOK_RPC.delete, { id }, "삭제 중…");
    if (!r.ok) return;
    setDeleting(null);
    // Refresh the list BEFORE clearing the active notebook: the auto-open effect
    // fires on active → null, and if the list still holds the deleted notebook at
    // that commit (the await boundary splits React's batching) it would reopen it.
    await loadNotebooks();
    setNotebooks((current) => current.filter((notebook) => notebook.id !== id));
    setActive((current) => (current?.id === id ? null : current));
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

  // Sources render as a light chip strip; clicking a chip expands (or folds) its
  // preview below. A stale key (notebook switch / deletion) simply closes the preview.
  const sources = active?.sources ?? [];
  const preview = previewKey ? sources.find((s) => srcKey(s) === previewKey) : undefined;

  return (
    <div className="notebook-pane">
      {/* One-line header — the pane shares its cell with the docked chat below, so
          the chrome is pressed flat: name + deal jump + delete on the left, the
          once-per-task notebook switcher (only when there is something to switch
          to) + create on the right. The remaining height belongs to the sources. */}
      <div className="notebook-bar">
        {active ? <h2 className="notebook-title">{active.name}</h2> : <span className="micro">노트북</span>}
        {active?.dealRef && (
          <button className="row-btn" onClick={() => openWiki(active.dealRef as string)} title="딜 페이지 열기">
            딜 페이지 →
          </button>
        )}
        {active && (
          <button
            className="row-btn notebook-danger"
            onClick={() => setDeleting(active)}
            aria-label="노트북 삭제"
            title="노트북 삭제"
          >
            <Icon name="trash" size={12} />
          </button>
        )}
        {status && <span className="pane-status">{status}</span>}
        <span className="notebook-bar-spring" />
        {connected && sortedNotebooks.length > 1 && (
          <select
            className="field notebook-select"
            aria-label="노트북 선택"
            value={active?.id ?? ""}
            onChange={(e) => void openNotebook(e.target.value)}
          >
            {!active && <option value="">노트북 선택…</option>}
            {sortedNotebooks.map((n) => (
              <option key={n.id} value={n.id}>
                {n.name}
                {n.sourceCount ? ` · 자료 ${n.sourceCount}` : ""}
              </option>
            ))}
          </select>
        )}
        <button
          className="row-btn notebook-new"
          onClick={() => setCreating(true)}
          disabled={!connected}
          aria-label="새 노트북"
          title="새 노트북"
        >
          <Icon name="plus" size={12} /> 새 노트북
        </button>
      </div>

      {!connected ? (
        <p className="notebook-empty">게이트웨이에 연결하세요.</p>
      ) : notebooks.length === 0 ? (
        <p className="notebook-empty">노트북이 없습니다. “＋ 새 노트북”으로 만드세요.</p>
      ) : !active ? (
        <p className="notebook-empty">위에서 노트북을 선택하세요.</p>
      ) : (
        <>
          {/* The sources as a wrapping chip strip: enough to SEE what material is in
              the notebook and add more, without spending the pane on reading it —
              the main work happens in the chat below (ask → answer → 노트에 저장).
              Click a chip to peek at that source; click again (or ×) to fold it. */}
          <div className="notebook-strip" role="list" aria-label="자료 목록">
            {sources.map((s, i) => (
              <NotebookSourceChip
                key={srcKey(s) || i}
                source={s}
                open={srcKey(s) === previewKey}
                onToggle={() => setPreviewKey((k) => (k === srcKey(s) ? "" : srcKey(s)))}
                onDelete={s.cite ? () => setDeletingSource(s) : undefined}
              />
            ))}
            <button
              className="row-btn notebook-add"
              onClick={() => setAddingSource(true)}
              title="인용자료 추가"
              aria-label="자료 추가"
            >
              <Icon name="plus" size={12} /> 자료
            </button>
          </div>
          {sources.length === 0 && (
            <p className="notebook-empty">아직 자료가 없습니다. “＋ 자료”로 메일·견적·메모·위키 등을 담으세요.</p>
          )}
          {preview && (
            <div className="notebook-preview" role="group" aria-label="자료 내용">
              <NotebookSourceDetail source={preview} onClose={() => setPreviewKey("")} />
            </div>
          )}
        </>
      )}

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
      {deleting && (
        <DeleteModal
          title="노트북 삭제"
          path={deleting.name}
          onClose={() => setDeleting(null)}
          onDelete={() => void deleteNotebook()}
        />
      )}
      {deletingSource && (
        <DeleteModal
          title="인용자료 삭제"
          path={sourceLabel(deletingSource)}
          onClose={() => setDeletingSource(null)}
          onDelete={() => void removeSource()}
        />
      )}
    </div>
  );
}

const NOTEBOOK_RESOURCE = "notebook";

const SOURCE_KIND_OPTIONS = [
  {
    kind: "note",
    label: "노트",
    refLabel: "내용",
    placeholder: "메일 본문·견적·메모 등 인용할 텍스트를 붙여넣으세요.",
  },
  { kind: "wiki", label: "위키", refLabel: "위키 경로", placeholder: "예: 프로젝트/topsolar.md" },
  { kind: "file", label: "파일", refLabel: "파일 경로", placeholder: "예: 계약서/topsolar.pdf" },
  { kind: "url", label: "URL", refLabel: "URL", placeholder: "https://example.com/article" },
  { kind: "mail", label: "메일", refLabel: "메일 ID", placeholder: "스레드 또는 메시지 ID" },
  { kind: "diary", label: "일기", refLabel: "일기 날짜/ID", placeholder: "예: 2026-06-24" },
] as const;

type SourceKind = (typeof SOURCE_KIND_OPTIONS)[number]["kind"];
type NewSource = { kind: SourceKind; title: string; text?: string; ref?: string };

const KIND_LABEL: Record<SourceKind, string> = Object.fromEntries(
  SOURCE_KIND_OPTIONS.map((option) => [option.kind, option.label]),
) as Record<SourceKind, string>;

interface NotebookListResponse {
  notebooks?: NotebookSummary[];
}

function sourceLabel(source: NotebookSource) {
  return [source.cite, source.title || source.ref || "(제목 없음)"].filter(Boolean).join(" · ");
}

// A source's stable key for selection. Pinned sources always carry a cite (S1…);
// ref/title are fallbacks for robustness.
function srcKey(s: NotebookSource): string {
  return s.cite || s.ref || s.title || "";
}

// A pasted note often has no title — show a one-line snippet of its text instead of
// "(제목 없음)" so the row/header is still meaningful at a glance.
function sourceTitle(s: NotebookSource): string {
  return s.title?.trim() || s.ref?.trim() || s.text?.split("\n")[0]?.slice(0, 80).trim() || "(제목 없음)";
}

// One cited source as a compact chip: citation badge + title + kind, with an ×
// to unpin. Clicking the chip body expands its content in the preview below;
// clicking again folds it. Light on purpose — the strip answers "what material
// is in here", not "read it all".
function NotebookSourceChip({
  source,
  open,
  onToggle,
  onDelete,
}: {
  source: NotebookSource;
  open: boolean;
  onToggle: () => void;
  onDelete?: () => void;
}) {
  return (
    <div className={"notebook-chip" + (open ? " active" : "")} role="listitem">
      <button
        type="button"
        className="notebook-chip-main"
        onClick={onToggle}
        aria-pressed={open}
        title={sourceTitle(source)}
      >
        {source.cite && <span className="notebook-cite">{source.cite}</span>}
        <span className="notebook-chip-title">{sourceTitle(source)}</span>
        {source.kind && (
          <span className="notebook-source-kind">{KIND_LABEL[source.kind as SourceKind] ?? source.kind}</span>
        )}
      </button>
      {onDelete && (
        <button
          type="button"
          className="notebook-chip-x"
          onClick={onDelete}
          aria-label={`인용자료 삭제 ${source.cite}`}
          title="인용자료 삭제"
        >
          <Icon name="close" size={10} />
        </button>
      )}
    </div>
  );
}

// The expanded preview under the chip strip: the chosen source's title/kind +
// full content (markdown text, or a ref line for gateway-ingested sources).
function NotebookSourceDetail({ source, onClose }: { source: NotebookSource; onClose: () => void }) {
  const kindLabel = source.kind ? (KIND_LABEL[source.kind as SourceKind] ?? source.kind) : "";
  return (
    <>
      <div className="notebook-detail-head">
        {source.cite && <span className="notebook-cite">{source.cite}</span>}
        <span className="notebook-detail-title">{sourceTitle(source)}</span>
        {kindLabel && <span className="notebook-source-kind">{kindLabel}</span>}
        <button
          type="button"
          className="row-btn notebook-preview-close"
          onClick={onClose}
          aria-label="미리보기 닫기"
          title="미리보기 닫기"
        >
          <Icon name="close" size={12} />
        </button>
      </div>
      {source.text ? (
        <div className="notebook-detail-body">
          <Markdown text={source.text} />
        </div>
      ) : source.ref ? (
        <div className="notebook-detail-ref">{source.ref}</div>
      ) : (
        <p className="notebook-empty">표시할 내용이 없습니다.</p>
      )}
    </>
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
  const [kind, setKind] = useState<SourceKind>("note");
  const [title, setTitle] = useState("");
  const [text, setText] = useState("");
  const [ref, setRef] = useState("");
  const kindOption = SOURCE_KIND_OPTIONS.find((option) => option.kind === kind) ?? SOURCE_KIND_OPTIONS[0];
  const canAdd = kind === "note" ? text.trim().length > 0 : ref.trim().length > 0;
  const add = () => {
    if (!canAdd) return;
    onAdd(
      kind === "note"
        ? { kind, title: title.trim(), text: text.trim() }
        : { kind, title: title.trim(), ref: ref.trim() },
    );
  };
  return (
    <Modal
      title={`인용자료 추가 — ${notebook}`}
      onClose={onClose}
      width={560}
      footer={<ModalFooter action="추가" canSubmit={canAdd} onClose={onClose} onSubmit={add} />}
    >
      <div style={{ marginBottom: 12 }}>
        <div style={{ fontSize: 12, color: "var(--muted)", marginBottom: 5 }}>종류</div>
        <div
          role="group"
          aria-label="종류"
          style={{ display: "grid", gridTemplateColumns: "repeat(3, minmax(0, 1fr))", gap: 6 }}
        >
          {SOURCE_KIND_OPTIONS.map(({ kind: k, label }) => (
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
      </div>
      <Field label="제목 (선택)">
        <input className="field" value={title} onChange={(e) => setTitle(e.target.value)} autoFocus />
      </Field>
      {kind === "note" ? (
        <Field label={kindOption.refLabel}>
          <textarea
            className="field"
            value={text}
            onChange={(e) => setText(e.target.value)}
            rows={8}
            placeholder={kindOption.placeholder}
            style={{ resize: "vertical", fontFamily: "inherit", lineHeight: 1.5 }}
          />
        </Field>
      ) : (
        <Field label={kindOption.refLabel}>
          <input
            className="field"
            value={ref}
            onChange={(e) => setRef(e.target.value)}
            placeholder={kindOption.placeholder}
            onKeyDown={(e) => {
              if (e.key === "Enter") add();
            }}
          />
        </Field>
      )}
    </Modal>
  );
}
