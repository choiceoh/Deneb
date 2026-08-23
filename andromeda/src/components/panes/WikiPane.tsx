import { useEffect, useRef, useState, type ReactNode } from "react";
import { clearCachedResource } from "@/cachedList";
import { printElement } from "@/print";
import { MEMORY_RPC } from "@/resources";
import type { WikiCategory, WikiDiaryEntry, WikiPage } from "@/types";
import { useCachedRpc } from "@/useCachedRpc";
import { color, muted } from "@/theme";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { Icon } from "@/components/Icon";
import { MarkdownEditor } from "@/components/MarkdownEditor";
import { DeleteModal, OneFieldModal } from "./commonModals";
import { MovePageModal, type NewPageDraft, NewPageModal, UnsavedWikiModal } from "./WikiModals";
import { ancestorsOf, buildWikiTree, fileLabel, folderLabel, type WikiTreeFolder } from "./wikiTree";

type BrowseMode = "tree" | "search" | "diary";

// One list call covers the whole wiki (~450 pages today); the gateway caps at
// maxMemoryListLimit. `total` in the response tells us when the cap truncated.
const TREE_FETCH_LIMIT = 2000;

// Wiki editor over memory.* — browse pages by category, search, recent diary,
// open one into the editor, save back, and perform page-level maintenance.
// Page-level modals (이동/새 페이지/저장하지 않은 변경) live in WikiModals.tsx.
export function WikiPane() {
  const { connected, cfg, wikiTarget, consumeWikiTarget } = useWorkspace();
  const { call, callCached, readCache, writeCache, status, setStatus } = useCachedRpc(cfg, WIKI_RESOURCE);
  const [categoriesSnapshot] = useState(() => readCache<WikiCategoriesResponse>(MEMORY_RPC.categories));
  const [mode, setMode] = useState<BrowseMode>("tree");
  const [q, setQ] = useState("");
  const [pages, setPages] = useState<WikiPage[]>([]);
  const [categories, setCategories] = useState<WikiCategory[]>(categoriesSnapshot?.data.categories ?? []);
  const [tree, setTree] = useState<WikiTreeFolder | null>(null);
  const [treeTotal, setTreeTotal] = useState(0);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [diary, setDiary] = useState<WikiDiaryEntry[]>([]);
  const [path, setPath] = useState<string | null>(null);
  const [content, setContent] = useState("");
  const [savedContent, setSavedContent] = useState("");
  const [currentReadOnly, setCurrentReadOnly] = useState(false);
  const [pendingPath, setPendingPath] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [moving, setMoving] = useState(false);
  const [merging, setMerging] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [preview, setPreview] = useState(true);
  const dirty = Boolean(path && !currentReadOnly && content !== savedContent);
  const editorRef = useRef<HTMLDivElement>(null);
  const pageOpenSeqRef = useRef(0);

  // 페이지 인쇄: 편집 중이었다면 미리보기(렌더된 마크다운)로 전환한 뒤, 그 렌더가 커밋된
  // 다음 프레임에 인쇄한다 — raw 마크다운 대신 읽기용 문서가 나가도록.
  function printPage() {
    setPreview(true);
    requestAnimationFrame(() => printElement(editorRef.current));
  }

  // Synthetic facts are a read-only trust projection, not editable workspace
  // prose. Never copy their body into the generic AI workspace context.
  useRegisterPane(
    WIKI_RESOURCE,
    !currentReadOnly && !isFactDerivedPath(path ?? "") && content.trim()
      ? `[위키${path ? ` ${path}` : ""}]\n${content}`
      : "",
  );

  useEffect(() => {
    if (!connected) return;
    void loadCategories(); // move-modal destinations
    void loadTree();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, cfg.url, cfg.token]);

  // Opening a page reveals it in the tree: expand every ancestor folder.
  // Adjusted during render so the reveal lands in the same pass as the path
  // change (the Set updater is pure).
  const [prevRevealPath, setPrevRevealPath] = useState(path);
  if (prevRevealPath !== path) {
    setPrevRevealPath(path);
    if (path) {
      setExpanded((prev) => {
        const next = new Set(prev);
        for (const dir of ancestorsOf(path)) next.add(dir);
        return next;
      });
    }
  }

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

  // The whole wiki in one call, folded into the folder tree the store keeps
  // on disk (프로젝트/<이름>/{대표, 로그, 기자재, 메일분석…}).
  async function loadTree() {
    await callCached<WikiCategoryPagesResponse>(
      MEMORY_RPC.listInCategory,
      { limit: TREE_FETCH_LIMIT },
      {
        scope: "wiki:tree",
        apply: (data) => {
          const rows = data?.pages ?? [];
          setTree(buildWikiTree(rows));
          setTreeTotal(data?.total ?? rows.length);
        },
      },
    );
  }

  async function search() {
    if (!connected) return;
    const query = q.trim();
    if (!query) {
      showTree();
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
    setStatus(list.length ? "" : "검색 결과 없음");
  }

  function showTree() {
    setMode("tree");
    setPages([]);
    setQ("");
    setStatus("");
  }

  function toggleFolder(folderPath: string) {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(folderPath)) next.delete(folderPath);
      else next.add(folderPath);
      return next;
    });
  }

  async function loadDiary() {
    setMode("diary");
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
    if (isFactDerivedPath(key)) {
      requestOpenFactRef(key);
      return;
    }
    if (dirty && key !== path) {
      setPendingPath(key);
      return;
    }
    void openPath(key);
  }

  function requestOpenSearchHit(hit: WikiPage) {
    const ref = keyOf(hit);
    if (hit.resultKind !== "fact" && !hit.readOnly && !isFactDerivedPath(ref)) {
      requestOpenPath(ref);
      return;
    }
    requestOpenFactRef(ref);
  }

  function requestOpenFactRef(ref: string) {
    if (dirty) {
      setStatus("먼저 현재 페이지를 저장하거나 되돌리세요");
      return;
    }
    void openFactRef(ref);
  }

  async function openFactRef(rawRef: string) {
    const ref = normalizeWikiRef(rawRef);
    if (!ref) {
      setStatus("사실 참조가 없습니다. 다시 검색하세요.");
      return;
    }
    const requestID = ++pageOpenSeqRef.current;
    // Never leave a previously-open fact visible while revalidating an old
    // search ref. get_page bypasses the client cache so Store.ReadPage can
    // reject a superseded claim ID.
    setPath(null);
    setContent("");
    setSavedContent("");
    setCurrentReadOnly(false);
    const r = await call<WikiPageResponse>(MEMORY_RPC.getPage, { path: ref }, "현재 사실 확인 중...");
    if (pageOpenSeqRef.current !== requestID) return;
    if (!r.ok) {
      setStatus("사실이 변경되었습니다. 다시 검색하세요.");
      return;
    }
    if (typeof r.data === "string" || canonicalFactRef(r.data?.path ?? "") !== canonicalFactRef(ref)) {
      setStatus("사실이 변경되었습니다. 다시 검색하세요.");
      return;
    }
    const body = r.data?.body ?? r.data?.content ?? "";
    setPath(r.data.path ?? ref);
    setContent(body);
    setSavedContent(body);
    setCurrentReadOnly(true);
    setPreview(true);
    setStatus("읽기 전용 사실");
  }

  async function openPath(key: string) {
    if (!key) return;
    if (isFactDerivedPath(key)) {
      await openFactRef(key);
      return;
    }
    const requestID = ++pageOpenSeqRef.current;
    const r = await callCached<WikiPageResponse>(
      MEMORY_RPC.getPage,
      { path: key },
      {
        pending: "불러오는 중...",
        scope: "wiki:page",
        apply: (data) => {
          if (pageOpenSeqRef.current === requestID) applyPage(key, data);
        },
      },
    );
    if (r.ok && r.applied && pageOpenSeqRef.current === requestID) setStatus("");
  }

  function applyPage(key: string, page: WikiPageResponse) {
    const body = typeof page === "string" ? page : (page?.body ?? page?.content ?? "");
    setPath(key);
    setContent(body);
    setSavedContent(body);
    setCurrentReadOnly(false);
    setPreview(true); // 페이지를 열면 미리보기가 기본 — 편집은 "편집" 탭으로 전환
  }

  function editContent(next: string) {
    setContent(next);
    if (status === "저장됨") setStatus("");
  }

  async function saveCurrent(): Promise<boolean> {
    if (!path || currentReadOnly) return false;
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
    await loadTree();
    const newPath = r.data?.path ?? `${category}/${title}`;
    await openPath(newPath);
    setPreview(false); // a freshly created page opens in edit — you just made it to write
  }

  async function movePage(to: string) {
    if (!path || currentReadOnly) return;
    const dst = to.trim();
    if (!dst) return;
    const r = await call<{ to?: string }>(MEMORY_RPC.movePage, { from: path, to: dst }, "이동 중...");
    if (!r.ok) return;
    setMoving(false);
    clearCachedResource(WIKI_RESOURCE);
    const nextPath = r.data?.to ?? dst;
    setPath(nextPath);
    await loadCategories();
    await loadTree();
    await openPath(nextPath);
    setStatus("이동됨");
  }

  async function mergePage(targetPath: string) {
    if (!path || currentReadOnly) return;
    const target = targetPath.trim();
    if (!target) return;
    const r = await call(MEMORY_RPC.merge, { targetPath: target, sourcePath: path }, "병합 중...");
    if (!r.ok) return;
    setMerging(false);
    clearCachedResource(WIKI_RESOURCE);
    setStatus("병합 시작됨");
  }

  async function deletePage() {
    if (!path || currentReadOnly) return;
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
    setCurrentReadOnly(false);
    setPages((rows) => rows.filter((p) => keyOf(p) !== current));
    await loadCategories();
    await loadTree();
    setStatus("삭제됨");
  }

  // Left-rail rows show the title only — the summary under it is dropped so the list
  // stays compact and the rail can be narrower (the body/AI carry the detail).
  const renderPage = (p: WikiPage) => (
    <button
      key={keyOf(p) || (p.title ?? "")}
      onClick={() => requestOpenSearchHit(p)}
      className="wiki-list-row"
      style={{ background: keyOf(p) === path ? color.active : "transparent" }}
    >
      <span>{p.title ?? p.subjectId ?? p.path ?? "(제목 없음)"}</span>
      {(p.resultKind === "fact" || p.readOnly) && <small>읽기 전용 사실</small>}
    </button>
  );

  // The tree renders the wiki's real on-disk hierarchy: folders toggle open,
  // files open in the editor. Slot files (대표/로그) lead each project folder.
  const renderTree = (folder: WikiTreeFolder, depth: number): ReactNode[] => {
    const rows: ReactNode[] = [];
    for (const sub of folder.folders) {
      const open = expanded.has(sub.path);
      rows.push(
        <button
          key={sub.path}
          className="wiki-tree-row"
          style={{ paddingLeft: 8 + depth * 14 }}
          onClick={() => toggleFolder(sub.path)}
          aria-expanded={open}
        >
          <span className="wiki-tree-caret" aria-hidden>
            {open ? "▾" : "▸"}
          </span>
          <span>{folderLabel(sub)}</span>
          <small>{sub.count}</small>
        </button>,
      );
      if (open) rows.push(...renderTree(sub, depth + 1));
    }
    for (const file of folder.files) {
      const label = fileLabel(file);
      rows.push(
        <button
          key={file.path}
          className="wiki-tree-row wiki-tree-file"
          style={{
            paddingLeft: 8 + depth * 14 + 16,
            background: file.path === path ? color.active : undefined,
          }}
          title={file.title && file.title !== label ? file.title : undefined}
          onClick={() => requestOpenPath(file.path)}
        >
          <span>{label}</span>
        </button>,
      );
    }
    return rows;
  };

  return (
    <>
      {/* The rail+editor split lives in its own size container so it collapses on the
          work-area width, not the viewport. Modals stay OUTSIDE it — a container is a
          containing block for position:fixed, which would trap the modal backdrops. */}
      <div className="wiki-split">
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
                <button className="row-btn" onClick={showTree} style={{ marginBottom: 6, padding: "3px 6px" }}>
                  트리로
                </button>
                {pages.length === 0 ? <p style={muted}>검색 결과 없음</p> : pages.map(renderPage)}
              </>
            ) : mode === "diary" ? (
              <>
                <button className="row-btn" onClick={showTree} style={{ marginBottom: 6, padding: "3px 6px" }}>
                  트리로
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
            ) : !tree ? (
              // null = the first tree fetch hasn't landed yet (or failed — status
              // shows the error) — not the same as a genuinely empty wiki.
              <p style={muted}>불러오는 중...</p>
            ) : tree.count === 0 ? (
              <p style={muted}>위키 페이지가 없습니다.</p>
            ) : (
              <>
                {renderTree(tree, 0)}
                {treeTotal > tree.count && (
                  <p className="micro" style={{ margin: "6px 2px 0" }}>
                    전체 {treeTotal}건 중 {tree.count}건 표시
                  </p>
                )}
              </>
            )}
          </div>
          <div className="wiki-editor" ref={editorRef}>
            <div className="wiki-editor-head">
              <div className="wiki-title-line">
                <h3>{path ?? "위키"}</h3>
                {path && (
                  <span className={"wiki-save-state no-print" + (dirty ? " dirty" : "")}>
                    {currentReadOnly ? "읽기 전용" : dirty ? "수정됨" : "저장됨"}
                  </span>
                )}
              </div>
              <div className="wiki-mode-tabs no-print" role="group" aria-label="위키 보기 방식">
                <button
                  className={"wiki-mode-tab" + (!preview ? " active" : "")}
                  onClick={() => setPreview(false)}
                  disabled={!path || currentReadOnly}
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
              <div className="wiki-editor-actions no-print">
                <button
                  className="btn btn-accent"
                  onClick={() => void save()}
                  disabled={!path || !dirty || currentReadOnly}
                >
                  저장
                </button>
                <button
                  className="row-btn"
                  onClick={() => editContent(savedContent)}
                  disabled={!dirty || currentReadOnly}
                >
                  되돌리기
                </button>
                <button
                  className="row-btn"
                  onClick={printPage}
                  disabled={!path}
                  title="이 페이지를 인쇄 (프린터 또는 PDF)"
                >
                  인쇄
                </button>
                <button
                  className="row-btn"
                  onClick={() => setMoving(true)}
                  disabled={!path || dirty || currentReadOnly}
                >
                  이동
                </button>
                <button
                  className="row-btn"
                  onClick={() => setMerging(true)}
                  disabled={!path || dirty || currentReadOnly}
                >
                  병합
                </button>
                <button
                  className="row-btn"
                  onClick={() => setDeleting(true)}
                  disabled={!path || dirty || currentReadOnly}
                  style={{ color: color.danger }}
                >
                  삭제
                </button>
                {status && <span className="pane-status">{status}</span>}
              </div>
            </div>
            {path ? (
              <MarkdownEditor
                value={content}
                onChange={editContent}
                preview={preview}
                disabled={currentReadOnly}
                fill
                ariaLabel="위키 미리보기"
              />
            ) : (
              // No page selected: a designed empty beats a dead blank editor
              // under an armed-looking toolbar.
              <div className="wiki-empty">
                <Icon name="wiki" size={28} />
                <p>페이지를 선택하세요</p>
                <span>왼쪽 트리에서 페이지를 고르거나 새로 시작하세요</span>
                <button className="btn" onClick={() => setCreating(true)}>
                  새 페이지
                </button>
              </div>
            )}
          </div>
        </div>
      </div>
      {creating && <NewPageModal onClose={() => setCreating(false)} onCreate={(p) => void createNewPage(p)} />}
      {moving && path && !currentReadOnly && (
        <MovePageModal
          path={path}
          categories={categories}
          onClose={() => setMoving(false)}
          onSubmit={(v) => void movePage(v)}
        />
      )}
      {merging && path && !currentReadOnly && (
        <OneFieldModal
          title="페이지 병합"
          label="병합 대상 경로"
          action="병합"
          onClose={() => setMerging(false)}
          onSubmit={(v) => void mergePage(v)}
        />
      )}
      {deleting && path && !currentReadOnly && (
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
    </>
  );
}

const WIKI_RESOURCE = "wiki";
const FACT_PROFILE_PATH = "사용자/현행-사실.md";

function isFactDerivedPath(path: string): boolean {
  return canonicalFactRef(path) !== "";
}

function canonicalFactRef(path: string): string {
  const trimmed = normalizeWikiRef(path);
  if (trimmed === FACT_PROFILE_PATH || trimmed === FACT_PROFILE_PATH.slice(0, -3)) return FACT_PROFILE_PATH;
  if (!trimmed.startsWith("@facts/")) return "";
  return trimmed.endsWith(".md") ? trimmed : `${trimmed}.md`;
}

function normalizeWikiRef(path: string): string {
  return path.trim().replaceAll("\\", "/");
}

interface WikiCategoriesResponse {
  categories?: WikiCategory[];
}

type WikiSearchResponse = WikiPage[] | { results?: WikiPage[]; pages?: WikiPage[] };

interface WikiCategoryPagesResponse {
  pages?: WikiPage[];
  total?: number; // full corpus size — larger than pages.length when the server cap truncated
}

interface WikiDiaryResponse {
  entries?: WikiDiaryEntry[];
}

type WikiPageResponse = { path?: string; body?: string; content?: string } | string;

function keyOf(p: WikiPage): string {
  return p.path ?? String(p.id ?? "");
}
