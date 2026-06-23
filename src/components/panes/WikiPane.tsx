import { useEffect, useState } from "react";
import { clearCachedResource } from "@/cachedList";
import { MEMORY_RPC } from "@/resources";
import type { WikiCategory, WikiDiaryEntry, WikiPage } from "@/types";
import { useCachedRpc } from "@/useCachedRpc";
import { color, line, muted } from "@/theme";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { Field, Modal } from "@/components/Modal";
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
  const [creating, setCreating] = useState(false);
  const [moving, setMoving] = useState(false);
  const [merging, setMerging] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [preview, setPreview] = useState(false);

  useRegisterPane(WIKI_RESOURCE, content.trim() ? `[위키${path ? ` ${path}` : ""}]\n${content}` : "");

  useEffect(() => {
    if (!connected) return;
    void loadCategories();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, cfg.url, cfg.token]);

  // 인물 카드 / 검색 결과에서 넘어온 위키 경로를 열고 채널을 비운다.
  useEffect(() => {
    if (!connected || !wikiTarget) return;
    void openPath(wikiTarget);
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
    setPath(key);
    setContent(typeof page === "string" ? page : (page?.body ?? page?.content ?? ""));
  }

  async function save() {
    if (!path) return;
    const r = await call(MEMORY_RPC.writePage, { path, body: content }, "저장 중...");
    if (!r.ok) return;
    clearCachedResource(WIKI_RESOURCE);
    writeCache<WikiPageResponse>(MEMORY_RPC.getPage, { path }, { body: content });
    setStatus("저장됨");
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
    setCatPages((rows) => rows.filter((p) => keyOf(p) !== current));
    setPages((rows) => rows.filter((p) => keyOf(p) !== current));
    await loadCategories();
    setStatus("삭제됨");
  }

  const renderPage = (p: WikiPage) => (
    <button
      key={keyOf(p) || (p.title ?? "")}
      onClick={() => void openPath(keyOf(p))}
      className="wiki-list-row"
      style={{ background: keyOf(p) === path ? color.active : "transparent" }}
    >
      <span>{p.title ?? p.path ?? "(제목 없음)"}</span>
      {p.summary && <small>{p.summary}</small>}
    </button>
  );

  return (
    <div style={{ display: "grid", gridTemplateColumns: "280px 1fr", gap: 12, height: "100%" }}>
      <div style={{ borderRight: line, paddingRight: 12, overflow: "auto" }}>
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
            disabled={!connected}
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
                  onClick={() => void openPath(entry.file ?? entry.path ?? "")}
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
      <div style={{ display: "flex", flexDirection: "column", minWidth: 0 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8, minWidth: 0 }}>
          <h3 style={{ margin: 0, flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis" }}>
            {path ?? "위키"}
          </h3>
          <button
            className="btn"
            onClick={() => setPreview((p) => !p)}
            disabled={!path}
            style={{ padding: "5px 12px", fontSize: 12 }}
          >
            {preview ? "편집" : "미리보기"}
          </button>
          <button
            className="btn btn-accent"
            onClick={() => void save()}
            disabled={!path}
            style={{ padding: "5px 12px", fontSize: 12 }}
          >
            저장
          </button>
          <button className="row-btn" onClick={() => setMoving(true)} disabled={!path}>
            이동
          </button>
          <button className="row-btn" onClick={() => setMerging(true)} disabled={!path}>
            병합
          </button>
          <button
            className="row-btn"
            onClick={() => setDeleting(true)}
            disabled={!path}
            style={{ color: color.danger }}
          >
            삭제
          </button>
          {status && <span className="pane-status">{status}</span>}
        </div>
        <MarkdownEditor
          value={content}
          onChange={setContent}
          preview={preview}
          disabled={!path}
          fill
          ariaLabel="위키 미리보기"
        />
      </div>
      {creating && <NewPageModal onClose={() => setCreating(false)} onCreate={(p) => void createNewPage(p)} />}
      {moving && path && (
        <OneFieldModal
          title="페이지 이동"
          label="새 경로"
          initialValue={path}
          action="이동"
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
