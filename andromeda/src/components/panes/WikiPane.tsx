import { useEffect, useState } from "react";
import { clearCachedResource } from "@/cachedList";
import { MEMORY_RPC } from "@/resources";
import type { WikiCategory, WikiDiaryEntry, WikiPage } from "@/types";
import { useCachedRpc } from "@/useCachedRpc";
import { color, line, muted } from "@/theme";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { Field, Modal, ModalFooter } from "@/components/Modal";
import { MarkdownEditor } from "@/components/MarkdownEditor";
import { DeleteModal, OneFieldModal } from "./commonModals";

type BrowseMode = "categories" | "search" | "diary";

interface NewPageDraft {
  title: string;
  category: string;
  summary: string;
  body: string;
}

// Wiki editor over memory.* — browse pages by category, search, recent diary,
// open one into the editor, save back, and perform page-level maintenance.
export function WikiPane() {
  const { connected, cfg, wikiTarget, consumeWikiTarget } = useWorkspace();
  const { call, callCached, readCache, writeCache, status, setStatus } = useCachedRpc(cfg, WIKI_RESOURCE);
  const [categoriesSnapshot] = useState(() => readCache<WikiCategoriesResponse>(MEMORY_RPC.categories));
  const [mode, setMode] = useState<BrowseMode>("categories");
  const [q, setQ] = useState("");
  const [pages, setPages] = useState<WikiPage[]>([]);
  const [categories, setCategories] = useState<WikiCategory[]>(categoriesSnapshot?.data.categories ?? []);
  const [browseCat, setBrowseCat] = useState<string | null>(null);
  const [catPages, setCatPages] = useState<WikiPage[]>([]);
  const [diary, setDiary] = useState<WikiDiaryEntry[]>([]);
  const [path, setPath] = useState<string | null>(null);
  const [content, setContent] = useState("");
  const [savedContent, setSavedContent] = useState("");
  const [pendingPath, setPendingPath] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [moving, setMoving] = useState(false);
  const [merging, setMerging] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [preview, setPreview] = useState(false);
  const dirty = Boolean(path && content !== savedContent);

  useRegisterPane(WIKI_RESOURCE, content.trim() ? `[위키${path ? ` ${path}` : ""}]\n${content}` : "");

  useEffect(() => {
    if (!connected) return;
    void loadCategories();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, cfg.url, cfg.token]);

  // 인물 카드 / 검색 결과에서 넘어온 위키 경로를 열고 채널을 비운다.
  useEffect(() => {
    if (!connected || !wikiTarget) return;
    requestOpenPath(wikiTarget);
    consumeWikiTarget();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wikiTarget, connected]);

  async function loadCategories() {
    await callCached<WikiCategoriesResponse>(
      MEMORY_RPC.categories,
      {},
      {
        scope: "wiki:categories",
        apply: (data) => setCategories(data?.categories ?? []),
      },
    );
  }

  async function search() {
    if (!connected) return;
    const query = q.trim();
    if (!query) {
      showCategories();
      return;
    }
    await callCached<WikiSearchResponse>(
      MEMORY_RPC.search,
      { query },
      {
        pending: "검색 중...",
        scope: "wiki:browse",
        apply: applySearchResult,
      },
    );
  }

  function applySearchResult(data: WikiSearchResponse) {
    const list = Array.isArray(data) ? data : (data?.results ?? data?.pages ?? []);
    setPages(list);
    setMode("search");
    setBrowseCat(null);
    setStatus(list.length ? "" : "검색 결과 없음");
  }

  function showCategories() {
    setMode("categories");
    setBrowseCat(null);
    setPages([]);
    setQ("");
    setStatus("");
  }

  async function openCategory(name: string) {
    setBrowseCat(name);
    setMode("categories");
    setCatPages([]);
    const r = await callCached<WikiCategoryPagesResponse>(
      MEMORY_RPC.listInCategory,
      { category: name, limit: 200 },
      {
        pending: "불러오는 중...",
        scope: "wiki:browse",
        apply: (data) => setCatPages(data?.pages ?? []),
      },
    );
    if (r.ok && r.applied) setStatus("");
  }

  async function loadDiary() {
    setMode("diary");
    setBrowseCat(null);
    setDiary([]);
    const r = await callCached<WikiDiaryResponse>(
      MEMORY_RPC.diaryRecent,
      { limit: 40 },
      {
        pending: "불러오는 중...",
        scope: "wiki:browse",
        apply: (data) => setDiary(data?.entries ?? []),
      },
    );
    if (r.ok && r.applied) setStatus((r.data?.entries ?? []).length ? "" : "최근 일지 없음");
  }

  function requestOpenPath(key: string) {
    if (!key) return;
    if (dirty && key !== path) {
      setPendingPath(key);
      return;
    }
    void openPath(key);
  }

  async function openPath(key: string) {
    if (!key) return;
    const r = await callCached<WikiPageResponse>(
      MEMORY_RPC.getPage,
      { path: key },
      {
        pending: "불러오는 중...",
        scope: "wiki:page",
        apply: (data) => applyPage(key, data),
      },
    );
    if (r.ok && r.applied) setStatus("");
  }

  function applyPage(key: string, page: WikiPageResponse) {
    const body = typeof page === "string" ? page : (page?.body ?? page?.content ?? "");
    setPath(key);
    setContent(body);
    setSavedContent(body);
    setPreview(false);
  }

  function editContent(next: string) {
    setContent(next);
    if (status === "저장됨") setStatus("");
  }

  async function saveCurrent(): Promise<boolean> {
    if (!path) return false;
    const currentPath = path;
    const body = content;
    const r = await call(MEMORY_RPC.writePage, { path: currentPath, body }, "저장 중...");
    if (!r.ok) return false;
    setSavedContent(body);
    clearCachedResource(WIKI_RESOURCE);
    writeCache<WikiPageResponse>(MEMORY_RPC.getPage, { path: currentPath }, { body });
    setStatus("");
    return true;
  }

  async function save() {
    await saveCurrent();
  }

  async function saveThenOpenPending() {
    const target = pendingPath;
    if (!target) return;
    const ok = await saveCurrent();
    if (!ok) return;
    setPendingPath(null);
    await openPath(target);
  }

  function discardThenOpenPending() {
    const target = pendingPath;
    setPendingPath(null);
    if (target) void openPath(target);
  }

  async function createNewPage(draft: NewPageDraft) {
    const title = draft.title.trim();
    const category = draft.category.trim();
    if (!title || !category) return;
    const r = await call<{ path?: string }>(
      MEMORY_RPC.createPage,
      { title, category, summary: draft.summary.trim(), body: draft.body },
      "생성 중...",
    );
    if (!r.ok) return;
    setCreating(false);
    clearCachedResource(WIKI_RESOURCE);
    await loadCategories();
    const newPath = r.data?.path ?? `${category}/${title}`;
    await openPath(newPath);
  }

  async function movePage(to: string) {
    if (!path) return;
    const dst = to.trim();
    if (!dst) return;
    const r = await call<{ to?: string }>(MEMORY_RPC.movePage, { from: path, to: dst }, "이동 중...");
    if (!r.ok) return;
    setMoving(false);
    clearCachedResource(WIKI_RESOURCE);
    const nextPath = r.data?.to ?? dst;
    setPath(nextPath);
    await loadCategories();
    await openPath(nextPath);
    setStatus("이동됨");
  }

  async function mergePage(targetPath: string) {
    if (!path) return;
    const target = targetPath.trim();
    if (!target) return;
    const r = await call(MEMORY_RPC.merge, { targetPath: target, sourcePath: path }, "병합 중...");
    if (!r.ok) return;
    setMerging(false);
    clearCachedResource(WIKI_RESOURCE);
    setStatus("병합 시작됨");
  }

  async function deletePage() {
    if (!path) return;
    const current = path;
    const r = await call<{ ok?: boolean; deleted?: number }>(
      MEMORY_RPC.deletePages,
      { paths: [current] },
      "삭제 중...",
    );
    if (!r.ok) return;
    clearCachedResource(WIKI_RESOURCE);
    setDeleting(false);
    setPath(null);
    setContent("");
    setSavedContent("");
    setCatPages((rows) => rows.filter((p) => keyOf(p) !== current));
    setPages((rows) => rows.filter((p) => keyOf(p) !== current));
    await loadCategories();
    setStatus("삭제됨");
  }

  const renderPage = (p: WikiPage) => (
    <button
      key={keyOf(p) || (p.title ?? "")}
      onClick={() => requestOpenPath(keyOf(p))}
      className="wiki-list-row"
      style={{ background: keyOf(p) === path ? color.active : "transparent" }}
    >
      <span>{p.title ?? p.path ?? "(제목 없음)"}</span>
      {p.summary && <small>{p.summary}</small>}
    </button>
  );

  return (
    <div className="wiki-shell">
      <div className="wiki-rail">
        <input
          className="field"
          style={{ width: "100%", boxSizing: "border-box", fontSize: 12, marginBottom: 8 }}
          placeholder="위키 검색..."
          value={q}
          disabled={!connected}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") void search();
          }}
        />
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 6, marginBottom: 8 }}>
          <button
            className="btn"
            onClick={() => setCreating(true)}
            disabled={!connected || dirty}
            title={dirty ? "먼저 저장하거나 되돌리세요" : "새 페이지"}
            style={{ fontSize: 12, padding: "6px 0" }}
          >
            새 페이지
          </button>
          <button
            className="btn"
            onClick={() => void loadDiary()}
            disabled={!connected}
            style={{ fontSize: 12, padding: "6px 0" }}
          >
            최근 일지
          </button>
        </div>
        {!connected ? (
          <p style={muted}>게이트웨이에 연결하세요.</p>
        ) : mode === "search" ? (
          <>
            <button className="row-btn" onClick={showCategories} style={{ marginBottom: 6, padding: "3px 6px" }}>
              목록으로
            </button>
            {pages.length === 0 ? <p style={muted}>검색 결과 없음</p> : pages.map(renderPage)}
          </>
        ) : mode === "diary" ? (
          <>
            <button className="row-btn" onClick={showCategories} style={{ marginBottom: 6, padding: "3px 6px" }}>
              카테고리
            </button>
            {diary.length === 0 ? (
              <p style={muted}>최근 일지 없음</p>
            ) : (
              diary.map((entry, i) => (
                <button
                  key={entry.file ?? entry.path ?? i}
                  className="wiki-list-row"
                  onClick={() => requestOpenPath(entry.file ?? entry.path ?? "")}
                >
                  <span>{entry.header ?? entry.title ?? entry.file ?? entry.path ?? "일지"}</span>
                  {entry.content && <small>{entry.content}</small>}
                </button>
              ))
            )}
          </>
        ) : browseCat ? (
          <>
            <button
              className="row-btn"
              onClick={() => setBrowseCat(null)}
              style={{ marginBottom: 4, padding: "3px 6px" }}
            >
              카테고리
            </button>
            <div className="micro" style={{ margin: "0 0 6px 2px" }}>
              {browseCat}
            </div>
            {catPages.length === 0 ? <p style={muted}>페이지가 없습니다.</p> : catPages.map(renderPage)}
          </>
        ) : categories.length === 0 ? (
          <p style={muted}>위키 페이지가 없습니다.</p>
        ) : (
          categories.map((c) => {
            const name = categoryName(c);
            return (
              <button key={name} onClick={() => void openCategory(name)} className="wiki-category-row">
                <span>{name}</span>
                {categoryCount(c) != null && <small>{categoryCount(c)}</small>}
              </button>
            );
          })
        )}
      </div>
      <div className="wiki-editor">
        <div className="wiki-editor-head">
          <div className="wiki-title-line">
            <h3>{path ?? "위키"}</h3>
            {path && <span className={"wiki-save-state" + (dirty ? " dirty" : "")}>{dirty ? "수정됨" : "저장됨"}</span>}
          </div>
          <div className="wiki-mode-tabs" role="group" aria-label="위키 보기 방식">
            <button
              className={"wiki-mode-tab" + (!preview ? " active" : "")}
              onClick={() => setPreview(false)}
              disabled={!path}
              aria-pressed={!preview}
            >
              편집
            </button>
            <button
              className={"wiki-mode-tab" + (preview ? " active" : "")}
              onClick={() => setPreview(true)}
              disabled={!path}
              aria-pressed={preview}
            >
              미리보기
            </button>
          </div>
          <div className="wiki-editor-actions">
            <button className="btn btn-accent" onClick={() => void save()} disabled={!path || !dirty}>
              저장
            </button>
            <button className="row-btn" onClick={() => editContent(savedContent)} disabled={!dirty}>
              되돌리기
            </button>
            <button className="row-btn" onClick={() => setMoving(true)} disabled={!path || dirty}>
              이동
            </button>
            <button className="row-btn" onClick={() => setMerging(true)} disabled={!path || dirty}>
              병합
            </button>
            <button
              className="row-btn"
              onClick={() => setDeleting(true)}
              disabled={!path || dirty}
              style={{ color: color.danger }}
            >
              삭제
            </button>
            {status && <span className="pane-status">{status}</span>}
          </div>
        </div>
        <MarkdownEditor
          value={content}
          onChange={editContent}
          preview={preview}
          disabled={!path}
          fill
          ariaLabel="위키 미리보기"
        />
      </div>
      {creating && <NewPageModal onClose={() => setCreating(false)} onCreate={(p) => void createNewPage(p)} />}
      {moving && path && (
        <MovePageModal
          path={path}
          categories={categories}
          onClose={() => setMoving(false)}
          onSubmit={(v) => void movePage(v)}
        />
      )}
      {merging && path && (
        <OneFieldModal
          title="페이지 병합"
          label="병합 대상 경로"
          action="병합"
          onClose={() => setMerging(false)}
          onSubmit={(v) => void mergePage(v)}
        />
      )}
      {deleting && path && (
        <DeleteModal
          title="페이지 삭제"
          path={path}
          onClose={() => setDeleting(false)}
          onDelete={() => void deletePage()}
        />
      )}
      {pendingPath && (
        <UnsavedWikiModal
          path={path ?? ""}
          targetPath={pendingPath}
          onClose={() => setPendingPath(null)}
          onDiscard={discardThenOpenPending}
          onSave={() => void saveThenOpenPending()}
        />
      )}
    </div>
  );
}

const WIKI_RESOURCE = "wiki";

interface WikiCategoriesResponse {
  categories?: WikiCategory[];
}

type WikiSearchResponse = WikiPage[] | { results?: WikiPage[]; pages?: WikiPage[] };

interface WikiCategoryPagesResponse {
  pages?: WikiPage[];
}

interface WikiDiaryResponse {
  entries?: WikiDiaryEntry[];
}

type WikiPageResponse = { body?: string; content?: string } | string;

function keyOf(p: WikiPage): string {
  return p.path ?? String(p.id ?? "");
}

function categoryName(c: WikiCategory): string {
  return c.name ?? c.category ?? "(root)";
}

function categoryCount(c: WikiCategory): number | undefined {
  return c.pageCount ?? c.count ?? c.pages;
}

// Split a page path into its directory and filename so each is editable on its own.
function dirOf(path: string): string {
  const i = path.lastIndexOf("/");
  return i < 0 ? "" : path.slice(0, i);
}
function baseOf(path: string): string {
  const i = path.lastIndexOf("/");
  return i < 0 ? path : path.slice(i + 1);
}
function joinPath(dir: string, name: string): string {
  const d = dir.trim().replace(/^\/+|\/+$/g, "");
  const n = name.trim().replace(/^\/+|\/+$/g, "");
  return d ? `${d}/${n}` : n;
}

// Move a page to another folder. The destination 분류 is picked by clicking an
// existing category (or 최상위 / + 새 분류); the page keeps its filename. The
// resulting path is shown before applying.
function MovePageModal({
  path,
  categories,
  onClose,
  onSubmit,
}: {
  path: string;
  categories: WikiCategory[];
  onClose: () => void;
  onSubmit: (to: string) => void;
}) {
  const [dir, setDir] = useState(() => dirOf(path));
  const [addingCat, setAddingCat] = useState(false);
  const [newCat, setNewCat] = useState("");

  const name = baseOf(path);
  // Existing categories + the page's current directory, deduped — so the source
  // category is always offered even if the registry hasn't surfaced it.
  const options = Array.from(
    new Set([dirOf(path), ...categories.map(categoryName)].filter((c) => c && c !== "(root)")),
  ).sort((a, b) => a.localeCompare(b, "ko"));

  const effectiveDir = addingCat ? newCat : dir;
  const to = joinPath(effectiveDir, name);
  const ready = to !== path && (!addingCat || Boolean(newCat.trim()));

  const catRow = (label: string, selected: boolean, onClick: () => void) => (
    <button
      key={label}
      className="wiki-category-row"
      onClick={onClick}
      style={{ background: selected ? color.active : "transparent" }}
    >
      <span>{label}</span>
    </button>
  );

  return (
    <Modal
      title="페이지 이동"
      onClose={onClose}
      width={460}
      footer={<ModalFooter action="이동" canSubmit={ready} onClose={onClose} onSubmit={() => onSubmit(to)} />}
    >
      <div style={{ fontSize: 12, color: color.muted, marginBottom: 5 }}>분류</div>
      <div
        style={{ display: "grid", gap: 2, maxHeight: 240, overflow: "auto", border: line, borderRadius: 8, padding: 4 }}
      >
        {catRow("최상위", !addingCat && dir === "", () => {
          setAddingCat(false);
          setDir("");
        })}
        {options.map((c) =>
          catRow(c, !addingCat && dir === c, () => {
            setAddingCat(false);
            setDir(c);
          }),
        )}
        {catRow("+ 새 분류", addingCat, () => {
          setAddingCat(true);
          setNewCat("");
        })}
      </div>
      {addingCat && (
        <input
          className="field"
          value={newCat}
          onChange={(e) => setNewCat(e.target.value)}
          placeholder="새 분류 이름"
          autoFocus
          style={{ marginTop: 6 }}
        />
      )}
      <p style={{ ...muted, margin: "8px 0 0", fontSize: 12, wordBreak: "break-all" }}>→ {to || "—"}</p>
    </Modal>
  );
}

function NewPageModal({ onClose, onCreate }: { onClose: () => void; onCreate: (draft: NewPageDraft) => void }) {
  const [draft, setDraft] = useState<NewPageDraft>({ title: "", category: "", summary: "", body: "" });
  const ready = draft.title.trim() && draft.category.trim();
  const set = (key: keyof NewPageDraft, value: string) => setDraft((d) => ({ ...d, [key]: value }));
  return (
    <Modal
      title="새 위키 페이지"
      onClose={onClose}
      width={520}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            취소
          </button>
          <button className="btn btn-accent" onClick={() => onCreate(draft)} disabled={!ready}>
            생성
          </button>
        </>
      }
    >
      <Field label="제목">
        <input
          className="field"
          value={draft.title}
          onChange={(e) => set("title", e.target.value)}
          placeholder="예: Andromeda 개선 노트"
          autoFocus
        />
      </Field>
      <Field label="분류">
        <input
          className="field"
          value={draft.category}
          onChange={(e) => set("category", e.target.value)}
          placeholder="예: projects"
        />
      </Field>
      <Field label="요약">
        <input className="field" value={draft.summary} onChange={(e) => set("summary", e.target.value)} />
      </Field>
      <Field label="본문">
        <textarea
          className="field"
          value={draft.body}
          onChange={(e) => set("body", e.target.value)}
          rows={6}
          style={{ resize: "vertical" }}
        />
      </Field>
    </Modal>
  );
}

function UnsavedWikiModal({
  path,
  targetPath,
  onClose,
  onDiscard,
  onSave,
}: {
  path: string;
  targetPath: string;
  onClose: () => void;
  onDiscard: () => void;
  onSave: () => void;
}) {
  return (
    <Modal
      title="저장하지 않은 변경"
      onClose={onClose}
      width={460}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            계속 편집
          </button>
          <button className="btn" onClick={onDiscard}>
            버리고 열기
          </button>
          <button className="btn btn-accent" onClick={onSave}>
            저장 후 열기
          </button>
        </>
      }
    >
      <p style={{ ...muted, margin: 0 }}>
        {path}에 저장하지 않은 변경이 있습니다. {targetPath}을 열기 전에 처리하세요.
      </p>
    </Modal>
  );
}
