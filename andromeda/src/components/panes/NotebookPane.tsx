import { useEffect, useMemo, useRef, useState } from "react";
import { NOTEBOOK_RPC } from "@/resources";
import { readFileBase64, splitAttachable } from "@/attachments";
import { inferAttachmentMimeType } from "@/attachmentMime";
import { projectList } from "@/aiText";
import type { Notebook, NotebookSource, NotebookSummary } from "@/types";
import { useCachedRpc } from "@/useCachedRpc";
import { useRegisterPane, useWorkspace, type NotebookTop } from "@/workspaceContext";
import { Icon, type IconName } from "@/components/Icon";
import { Field, Modal, ModalFooter } from "@/components/Modal";
import { Markdown } from "@/components/Markdown";
import { DeleteModal } from "./commonModals";
import { formatBytes } from "./fileHelpers";

// Notebook (노트북) — a browser over Deneb's deal notebooks (miniapp.notebook.*).
// Each notebook is a 거래 with cited source materials; opening one feeds its
// sources to the AI panel so Deneb answers grounded in that deal (NotebookLM-
// style). You can also create a notebook and pin (add) a citation source.
// 자료 영역 높이 버튼의 순환 순서(접힘→기본→확대→접힘)와, 각 상태에서 버튼이 할
// "다음 동작"을 나타내는 아이콘·라벨. 확대 단계는 win-max(확대), 나머지는 셰브런 방향.
const NEXT_TOP: Record<NotebookTop, NotebookTop> = { folded: "default", default: "expanded", expanded: "folded" };
const TOP_ACTION: Record<NotebookTop, { icon: IconName; label: string }> = {
  folded: { icon: "chevron-down", label: "자료 영역 펼치기" },
  default: { icon: "win-max", label: "자료 영역 확대" },
  expanded: { icon: "chevron-up", label: "자료 영역 접기" },
};

export function NotebookPane() {
  const { connected, cfg, openWiki, setNoteSink, notebookTop, setNotebookTop } = useWorkspace();
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

  // The active notebook id as a render-time ref mirror — the note sink below runs
  // asynchronously and must see the CURRENT notebook at completion time, not the
  // one captured when it was registered.
  const activeIdRef = useRef<string | null>(null);
  activeIdRef.current = active?.id ?? null;

  // AI-answer → note sink: while a notebook is open, register a save function so
  // the docked AI panel can offer 노트에 저장 on every finished answer. Saving pins
  // the answer as a kind=note source (title falls back to a first-line snippet) —
  // the notebook's OUTPUT loop: material made with the AI stays in the notebook
  // instead of scrolling away in the chat. Resolves with whether the pin landed so
  // the AI panel only marks 저장됨 on success (failure stays retryable). `call` is
  // cfg-keyed (useCallback), so a gateway change re-registers a fresh sink instead
  // of leaving a stale client captured here.
  useEffect(() => {
    if (!connected || !active) {
      setNoteSink(null);
      return;
    }
    const id = active.id;
    setNoteSink(async (text: string) => {
      const body = text.trim();
      if (!body) return false;
      const r = await call(NOTEBOOK_RPC.addSource, { id, kind: "note", title: "", text: body }, "노트 저장 중…");
      if (!r.ok) return false;
      // Refresh only if this notebook is still the open one — the user may have
      // switched during the RPC, and re-opening would yank them back.
      if (activeIdRef.current === id) await openNotebook(id);
      void loadNotebooks();
      return true;
    });
    return () => setNoteSink(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, active?.id, call]);

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
    // url/mail/diary carry only a ref the user typed — the gateway must READ that
    // ref into text (add_ref), because add_source rejects them for lack of text.
    // note (text) and wiki (ref read live at brief time) still go through add_source.
    const ingested = src.kind === "url" || src.kind === "mail" || src.kind === "diary";
    const r = ingested
      ? await call(
          NOTEBOOK_RPC.addRef,
          { id: active.id, kind: src.kind, ref: src.ref, title: src.title },
          "가져오는 중…",
        )
      : await call(
          NOTEBOOK_RPC.addSource,
          { id: active.id, kind: src.kind, title: src.title, text: src.text, ref: src.ref },
          "추가 중…",
        );
    if (!r.ok) return;
    setAddingSource(false);
    await openNotebook(active.id); // reload to show the new source
    void loadNotebooks(); // refresh the list's source count
  }

  // Pin PICKED (or dropped) local files: read each to base64 client-side, then let
  // the gateway extract its text — documents via the doc extractor, images via OCR,
  // audio/video via ASR — and pin it as a kind=file source, no path typed. Multiple
  // files pin one source each; a shared title only applies to a single file.
  async function addFiles(files: File[], title: string) {
    if (!active || files.length === 0) return;
    const id = active.id;
    let anyOk = false;
    for (let i = 0; i < files.length; i++) {
      const file = files[i];
      const dataBase64 = await readFileBase64(file);
      const r = await call(
        NOTEBOOK_RPC.addFile,
        {
          id,
          filename: file.name,
          mimeType: inferAttachmentMimeType(file.name, file.type),
          dataBase64,
          title: files.length === 1 ? title.trim() : "",
        },
        files.length === 1 ? "파일 읽는 중…" : `파일 읽는 중… (${i + 1}/${files.length})`,
      );
      if (r.ok) anyOk = true;
    }
    if (!anyOk) return;
    setAddingSource(false);
    await openNotebook(id);
    void loadNotebooks();
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
        {/* Switcher shows when there is something to switch to — and also whenever
            nothing is open (e.g. the auto-open fetch failed with a single notebook):
            otherwise the body says "위에서 노트북을 선택하세요" with nothing to select. */}
        {connected && sortedNotebooks.length > 0 && (!active || sortedNotebooks.length > 1) && (
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
        {/* Cycle the materials area's height — 접힘(바) → 기본(30%) → 확대(70%) → 접힘.
            Workstation sizes the grid's top row to match; the docked chat below takes
            whatever height is left. Local state (previewKey 등) survives the cycle, so
            growing/shrinking restores the exact view. */}
        <button
          className="row-btn notebook-fold"
          onClick={() => setNotebookTop(NEXT_TOP[notebookTop])}
          aria-expanded={notebookTop !== "folded"}
          aria-label={TOP_ACTION[notebookTop].label}
          title={TOP_ACTION[notebookTop].label}
        >
          <Icon name={TOP_ACTION[notebookTop].icon} size={12} />
        </button>
      </div>

      {notebookTop !== "folded" &&
        (!connected ? (
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
        ))}

      {creating && (
        <CreateNotebookModal onClose={() => setCreating(false)} onCreate={(name, desc) => createNotebook(name, desc)} />
      )}
      {addingSource && active && (
        <AddSourceModal
          notebook={active.name}
          onClose={() => setAddingSource(false)}
          onAdd={(src) => addSource(src)}
          onAddFiles={(files, title) => addFiles(files, title)}
        />
      )}
      {deleting && (
        <DeleteModal
          title="노트북 삭제"
          path={deleting.name}
          onClose={() => setDeleting(null)}
          onDelete={() => deleteNotebook()}
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
  { kind: "file", label: "파일", refLabel: "파일", placeholder: "" },
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
  onCreate: (name: string, desc: string) => void | Promise<unknown>;
}) {
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = () => {
    if (busy || !name.trim()) return;
    const r = onCreate(name, desc);
    if (r && typeof r.then === "function") {
      setBusy(true);
      void r.finally(() => setBusy(false));
    }
  };
  return (
    <Modal
      title="새 노트북"
      onClose={onClose}
      width={460}
      footer={
        <ModalFooter action="생성" canSubmit={Boolean(name.trim())} busy={busy} onClose={onClose} onSubmit={submit} />
      }
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

// Files the gateway can turn into text — documents (doc extractor), images (OCR),
// and audio/video (ASR). Mirrors the chat composer's accept list so the notebook
// picker takes the same materials; drops/pastes are re-validated by splitAttachable.
const FILE_ACCEPT =
  "image/*,audio/*,video/*,.png,.jpg,.jpeg,.webp,.gif,.mp3,.m4a,.wav,.ogg,.webm,.mp4,.mov,.mkv,.avi,.m4v," +
  ".pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.rtf,.odt,.ods,.odp,.hwp,.hwpx,.csv,.txt,.md";

// Pin a citation source via miniapp.notebook.add_source (pasted note / wiki ref /
// url / mail / diary), or PICKED/DROPPED files via miniapp.notebook.add_file. The
// kind picker switches the input below; the 파일 kind opens a native file dialog (or
// accepts a drop) instead of asking the user to type a path — the gateway extracts
// each file's text (documents, image OCR, audio/video transcription).
function AddSourceModal({
  notebook,
  onClose,
  onAdd,
  onAddFiles,
}: {
  notebook: string;
  onClose: () => void;
  onAdd: (src: NewSource) => void | Promise<unknown>;
  onAddFiles: (files: File[], title: string) => void | Promise<unknown>;
}) {
  const [kind, setKind] = useState<SourceKind>("note");
  const [title, setTitle] = useState("");
  const [text, setText] = useState("");
  const [ref, setRef] = useState("");
  const [files, setFiles] = useState<File[]>([]);
  const [fileErr, setFileErr] = useState("");
  const [dragOver, setDragOver] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const kindOption = SOURCE_KIND_OPTIONS.find((option) => option.kind === kind) ?? SOURCE_KIND_OPTIONS[0];
  const canAdd = kind === "note" ? text.trim().length > 0 : kind === "file" ? files.length > 0 : ref.trim().length > 0;
  const [busy, setBusy] = useState(false);

  // Intake picked/dropped files through the shared chat rules (supported type +
  // size), appending the accepted ones and surfacing any skip reasons in-dialog.
  function intakeFiles(list: FileList | File[] | null) {
    if (!list) return;
    const { ok, skipped } = splitAttachable(Array.from(list));
    setFileErr(skipped.join(" · "));
    if (ok.length) setFiles((prev) => [...prev, ...ok]);
  }

  const add = () => {
    if (!canAdd || busy) return;
    const r =
      kind === "file"
        ? onAddFiles(files, title)
        : onAdd(
            kind === "note"
              ? { kind, title: title.trim(), text: text.trim() }
              : { kind, title: title.trim(), ref: ref.trim() },
          );
    if (r && typeof r.then === "function") {
      setBusy(true);
      void r.finally(() => setBusy(false));
    }
  };
  return (
    <Modal
      title={`인용자료 추가 — ${notebook}`}
      onClose={onClose}
      width={560}
      footer={<ModalFooter action="추가" canSubmit={canAdd} busy={busy} onClose={onClose} onSubmit={add} />}
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
      ) : kind === "file" ? (
        // A plain block (not <Field>, whose <label> would fold the picker button's
        // accessible name into the label text) — the drop zone holds a real button.
        <div style={{ marginBottom: 12 }}>
          <div style={{ fontSize: 12, color: "var(--muted)", marginBottom: 5 }}>{kindOption.refLabel}</div>
          <div
            onDragOver={(e) => {
              e.preventDefault();
              setDragOver(true);
            }}
            onDragLeave={() => setDragOver(false)}
            onDrop={(e) => {
              e.preventDefault();
              setDragOver(false);
              intakeFiles(e.dataTransfer.files);
            }}
            style={{
              border: `1px dashed ${dragOver ? "var(--accent)" : "var(--line)"}`,
              borderRadius: 8,
              padding: 12,
              display: "flex",
              alignItems: "center",
              gap: 8,
            }}
          >
            <button type="button" className="btn" onClick={() => fileRef.current?.click()}>
              파일 선택
            </button>
            <span className="micro" style={{ color: "var(--muted)" }}>
              또는 여기로 끌어다 놓기 (여러 개 가능)
            </span>
            <input
              ref={fileRef}
              type="file"
              multiple
              accept={FILE_ACCEPT}
              style={{ display: "none" }}
              onChange={(e) => {
                const list = e.target.files;
                e.currentTarget.value = "";
                intakeFiles(list);
              }}
            />
          </div>
          {files.length > 0 && (
            <ul
              style={{
                listStyle: "none",
                margin: "8px 0 0",
                padding: 0,
                display: "flex",
                flexDirection: "column",
                gap: 4,
              }}
            >
              {files.map((f, i) => (
                <li key={`${f.name}-${i}`} style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13 }}>
                  <span
                    style={{ flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}
                    title={f.name}
                  >
                    {f.name} · {formatBytes(f.size)}
                  </span>
                  <button
                    type="button"
                    className="row-btn"
                    aria-label={`${f.name} 제거`}
                    title="제거"
                    onClick={() => setFiles((prev) => prev.filter((_, j) => j !== i))}
                  >
                    <Icon name="close" size={10} />
                  </button>
                </li>
              ))}
            </ul>
          )}
          {fileErr ? (
            <p className="pane-status" style={{ color: "var(--danger)", marginTop: 6 }}>
              {fileErr}
            </p>
          ) : (
            <p className="micro" style={{ color: "var(--muted)", marginTop: 6 }}>
              문서·이미지·음성/영상을 담을 수 있습니다 (이미지는 OCR, 음성·영상은 자동 전사).
            </p>
          )}
        </div>
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
