import { useEffect, useRef, useState } from "react";
import { inferAttachmentMimeType } from "@/attachmentMime";
import { clearCachedResource } from "@/cachedList";
import { projectList } from "@/aiText";
import { fetchGatewayBlob, filesDownloadUrl } from "@/gateway";
import { FILES_RPC } from "@/resources";
import type { FileEntry } from "@/types";
import { useCachedRpc } from "@/useCachedRpc";
import { color, ellipsis, muted } from "@/theme";
import { fmtDate } from "@/format";
import { useWorkspace } from "@/workspaceContext";
import { Column, Grid, GridNotice, RowBtn } from "@/components/Grid";
import { FileViewer } from "@/components/FileViewer";
import { textToBase64 } from "@/components/fileView";
import { Modal } from "@/components/Modal";
import { DeleteModal, OneFieldModal } from "./commonModals";
import { entryPath, formatBytes, isFolder, joinPath, parentPath } from "./fileHelpers";

// One open viewer tab. Viewer content state lives in the (kept-mounted)
// FileViewer; the tab records identity + dirty flag for the close guard, and
// the listed byte size so the viewer can refuse oversized files pre-download.
interface FileTab {
  path: string;
  name: string;
  dirty: boolean;
  size?: number;
}

// FilesPane stays mounted across pane switches (Workstation renders it like
// chat/code) so open viewer tabs and their unsaved edits survive. `active` is
// true only while 파일 is the current view.
export function FilesPane({ active = true }: { active?: boolean }) {
  const { connected, cfg, registerPane } = useWorkspace();
  const { call, callCached, readCache, status, setStatus, busy } = useCachedRpc(cfg, FILES_RESOURCE);
  const [rootSnapshot] = useState(() => readCache<FilesListResponse>(FILES_RPC.list, filesListParams("")));
  const [path, setPath] = useState(rootSnapshot?.data.path ?? "");
  const [entries, setEntries] = useState<FileEntry[]>(rootSnapshot?.data.entries ?? []);
  const [query, setQuery] = useState("");
  const [searching, setSearching] = useState(false);
  const [searchContent, setSearchContent] = useState(false);
  const [searchSemantic, setSearchSemantic] = useState(false);
  const [selected, setSelected] = useState<FileEntry | null>(null);
  const [shareUrl, setShareUrl] = useState("");
  const [makingFolder, setMakingFolder] = useState(false);
  const [moving, setMoving] = useState<FileEntry | null>(null);
  const [deleting, setDeleting] = useState<FileEntry | null>(null);
  const [tabs, setTabs] = useState<FileTab[]>([]);
  const [activeTab, setActiveTab] = useState<string | null>(null);
  const [closingTab, setClosingTab] = useState<string | null>(null); // dirty-close confirm
  const uploadRef = useRef<HTMLInputElement>(null);

  const aiText = projectList(
    `[파일 ${searching ? "검색" : path || "/"}]`,
    entries,
    (e) => `- ${isFolder(e) ? "[폴더] " : ""}${entryPath(e)}${e.size ? ` (${formatBytes(e.size)})` : ""}`,
  );
  // Publish the file listing to the AI panel only while active. Because the pane
  // stays mounted while hidden (to preserve tabs/edits), an unconditional
  // registration would clobber the visible pane's AI context. Guarding on
  // `active` is also race-safe on switch-away: it's a no-op, so the newly active
  // pane's registration wins regardless of effect order.
  useEffect(() => {
    if (active) registerPane(FILES_RESOURCE, aiText);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active, aiText]);

  useEffect(() => {
    if (!connected) return;
    void list("");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, cfg.url, cfg.token]);

  async function list(nextPath = path) {
    const r = await callCached<FilesListResponse>(FILES_RPC.list, filesListParams(nextPath), {
      pending: "불러오는 중...",
      scope: "files:entries",
      apply: (data) => applyList(data, nextPath),
    });
    if (r.ok && r.applied) setStatus("");
  }

  async function search() {
    const q = query.trim();
    if (!q) {
      await list(path);
      return;
    }
    const r = await callCached<FilesSearchResponse>(
      FILES_RPC.search,
      filesSearchParams(q, searchContent, searchSemantic),
      {
        pending: "검색 중...",
        scope: "files:entries",
        apply: (data) => applySearch(data.entries ?? []),
      },
    );
    if (r.ok && r.applied) setStatus((r.data?.entries ?? []).length ? "" : "검색 결과 없음");
  }

  function applyList(data: FilesListResponse, fallbackPath: string) {
    setPath(data?.path ?? fallbackPath);
    setEntries(data?.entries ?? []);
    setSearching(false);
    setSelected(null);
    setShareUrl("");
  }

  function applySearch(list: FileEntry[]) {
    setEntries(list);
    setSearching(true);
    setSelected(null);
    setShareUrl("");
  }

  async function share(entry: FileEntry) {
    const target = entryPath(entry);
    const r = await call<{ url?: string }>(FILES_RPC.share, { path: target }, "공유 링크 생성 중...");
    if (!r.ok) return;
    setSelected(entry);
    setShareUrl(r.data?.url ?? "");
    setStatus(r.data?.url ? "공유 링크 생성됨" : "공유 링크 없음");
  }

  async function mkdir(name: string) {
    const trimmed = name.trim();
    if (!trimmed) return;
    const r = await call(FILES_RPC.mkdir, { path: joinPath(path, trimmed) }, "폴더 생성 중...");
    if (!r.ok) return;
    setMakingFolder(false);
    clearCachedResource(FILES_RESOURCE);
    await list(path);
    setStatus("폴더 생성됨");
  }

  async function move(entry: FileEntry, dst: string) {
    const target = dst.trim();
    if (!target) return;
    const r = await call(FILES_RPC.move, { src: entryPath(entry), dst: target }, "이동 중...");
    if (!r.ok) return;
    setMoving(null);
    retargetTabs(entryPath(entry), target);
    clearCachedResource(FILES_RESOURCE);
    await list(path);
    setStatus("이동됨");
  }

  async function deleteEntry(entry: FileEntry) {
    const r = await call(FILES_RPC.delete, { path: entryPath(entry) }, "삭제 중...");
    if (!r.ok) return;
    setDeleting(null);
    setSelected(null);
    closeTabsUnder(entryPath(entry));
    clearCachedResource(FILES_RESOURCE);
    await list(path);
    setStatus("삭제됨");
  }

  async function upload(file: File) {
    const dataBase64 = await fileToBase64(file);
    const r = await call(
      FILES_RPC.upload,
      { path: joinPath(path, file.name), mimeType: inferAttachmentMimeType(file.name, file.type), dataBase64 },
      "업로드 중...",
    );
    if (!r.ok) return;
    clearCachedResource(FILES_RESOURCE);
    await list(path);
    setStatus("업로드됨");
  }

  // --- viewer tabs ---------------------------------------------------------

  function openFile(entry: FileEntry) {
    const p = entryPath(entry);
    if (!p) return;
    setSelected(entry);
    setTabs((prev) =>
      prev.some((t) => t.path === p)
        ? prev
        : [...prev, { path: p, name: entry.name ?? p, dirty: false, size: entry.size }],
    );
    setActiveTab(p);
  }

  // 이동/삭제와 열린 탭의 동기화 — 옛 경로로 남은 탭에서 저장하면 (삭제 후) 파일을 그
  // 자리에 되살리거나 (이름변경 후) 사본을 만든다. 성공한 이동은 탭을 새 경로로 재지정
  // 하고(폴더 이동은 하위 탭 전부), 삭제는 해당 탭을 닫는다.
  function retargetTabs(src: string, dst: string) {
    const mapPath = (p: string) => (p === src ? dst : p.startsWith(src + "/") ? dst + p.slice(src.length) : p);
    setTabs((prev) => {
      if (!prev.some((t) => mapPath(t.path) !== t.path)) return prev;
      const seen = new Set<string>();
      const next: FileTab[] = [];
      for (const t of prev) {
        const p = mapPath(t.path);
        if (seen.has(p)) continue; // destination already open — drop the duplicate tab
        seen.add(p);
        // Re-pathing remounts the viewer (tab key = path): content reloads from
        // the new path, so the dirty flag resets with it.
        next.push(p === t.path ? t : { ...t, path: p, name: p.split("/").pop() || t.name, dirty: false });
      }
      setActiveTab((cur) => (cur ? mapPath(cur) : cur));
      return next;
    });
  }

  function closeTabsUnder(src: string) {
    const gone = (p: string) => p === src || p.startsWith(src + "/");
    setTabs((prev) => {
      if (!prev.some((t) => gone(t.path))) return prev;
      const next = prev.filter((t) => !gone(t.path));
      setActiveTab((cur) => (cur && gone(cur) ? (next.at(-1)?.path ?? null) : cur));
      return next;
    });
  }

  function markDirty(p: string, dirty: boolean) {
    setTabs((prev) => prev.map((t) => (t.path === p ? { ...t, dirty } : t)));
  }

  function requestCloseTab(p: string) {
    const tab = tabs.find((t) => t.path === p);
    if (tab?.dirty) {
      setClosingTab(p);
      return;
    }
    closeTab(p);
  }

  function closeTab(p: string) {
    setClosingTab(null);
    setTabs((prev) => {
      const next = prev.filter((t) => t.path !== p);
      setActiveTab((cur) => (cur === p ? (next.at(-1)?.path ?? null) : cur));
      return next;
    });
  }

  // saveFile is the live-edit save: overwrite=true replaces the same path (the
  // default upload autorenames on clash, which would fork the file per save).
  async function saveFile(p: string, text: string): Promise<boolean> {
    const r = await call(
      FILES_RPC.upload,
      { path: p, mimeType: inferAttachmentMimeType(p, ""), dataBase64: textToBase64(text), overwrite: true },
      "저장 중...",
    );
    if (!r.ok) return false;
    clearCachedResource(FILES_RESOURCE);
    setStatus("저장됨");
    return true;
  }

  const columns: Column<FileEntry>[] = [
    {
      header: "이름",
      cell: (e) => (
        <>
          <div style={{ fontWeight: isFolder(e) ? 600 : 500, ...ellipsis(360) }}>{e.name ?? entryPath(e)}</div>
          <div style={{ fontSize: 12, color: color.muted, ...ellipsis(420) }}>{entryPath(e)}</div>
        </>
      ),
    },
    {
      header: "종류",
      width: 84,
      tdStyle: { fontSize: 13, opacity: 0.75 },
      cell: (e) => (isFolder(e) ? "폴더" : "파일"),
    },
    {
      header: "크기",
      width: 96,
      tdStyle: { fontSize: 13, opacity: 0.72, whiteSpace: "nowrap" },
      cell: (e) => (isFolder(e) ? "" : formatBytes(e.size)),
    },
    {
      header: "수정",
      width: 128,
      tdStyle: { fontSize: 13, opacity: 0.72, whiteSpace: "nowrap" },
      cell: (e) => fmtDate(e.serverModified),
    },
    {
      header: "",
      width: 170,
      tdStyle: { textAlign: "right", whiteSpace: "nowrap" },
      cell: (e) => (
        <span style={{ display: "inline-flex", gap: 2 }}>
          {!isFolder(e) && (
            <RowBtn onClick={() => void share(e)} disabled={busy} title="공유 링크">
              공유
            </RowBtn>
          )}
          <RowBtn onClick={() => setMoving(e)} disabled={busy} title="이동 또는 이름 변경">
            이동
          </RowBtn>
          <RowBtn onClick={() => setDeleting(e)} disabled={busy} danger title="삭제">
            삭제
          </RowBtn>
        </span>
      ),
    },
  ];

  return (
    <>
      <h2 style={{ marginTop: 2 }}>파일</h2>
      <div className="files-toolbar">
        <button className="btn" onClick={() => void list(parentPath(path))} disabled={!connected || !path || busy}>
          상위
        </button>
        <input
          className="field"
          value={searching ? "검색 결과" : path || "/"}
          onChange={(e) => setPath(e.target.value === "/" ? "" : e.target.value)}
          disabled={searching || busy}
          onKeyDown={(e) => {
            if (e.key === "Enter") void list(path);
          }}
          aria-label="파일 경로"
        />
        <button className="btn" onClick={() => void list(path)} disabled={!connected || busy}>
          새로고침
        </button>
        <button className="btn" onClick={() => setMakingFolder(true)} disabled={!connected || busy || searching}>
          새 폴더
        </button>
        <button className="btn btn-accent" onClick={() => uploadRef.current?.click()} disabled={!connected || busy}>
          업로드
        </button>
        <input
          ref={uploadRef}
          type="file"
          style={{ display: "none" }}
          onChange={(e) => {
            const file = e.target.files?.[0];
            e.currentTarget.value = "";
            if (file) void upload(file);
          }}
        />
      </div>
      <div className="files-search">
        <input
          className="field"
          placeholder="파일 검색..."
          value={query}
          disabled={!connected || busy}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") void search();
          }}
        />
        <label>
          <input type="checkbox" checked={searchContent} onChange={(e) => setSearchContent(e.target.checked)} />
          내용
        </label>
        <label>
          <input type="checkbox" checked={searchSemantic} onChange={(e) => setSearchSemantic(e.target.checked)} />
          의미
        </label>
        <button className="btn" onClick={() => void search()} disabled={!connected || busy}>
          검색
        </button>
        {searching && (
          <button className="row-btn" onClick={() => void list(path)} disabled={busy}>
            폴더로
          </button>
        )}
      </div>
      {status && <p className="pane-status">{status}</p>}
      {shareUrl && selected && (
        <div className="files-share">
          <span>{selected.name ?? entryPath(selected)}</span>
          <a href={shareUrl} target="_blank" rel="noreferrer">
            {shareUrl}
          </a>
        </div>
      )}
      <GridNotice query={{ isLoading: busy && entries.length === 0 }} count={entries.length} empty="파일이 없습니다.">
        <Grid
          columns={columns}
          rows={entries}
          getKey={(e) => entryPath(e)}
          maxWidth={980}
          onRowClick={(e) => {
            if (isFolder(e)) void list(entryPath(e));
            else openFile(e);
          }}
          isRowSelected={(e) => entryPath(e) === entryPath(selected ?? {})}
          rowTitle={(e) => entryPath(e)}
        />
      </GridNotice>
      {tabs.length > 0 && (
        <div className="file-tabs-shell">
          <div className="file-tabs" role="tablist" aria-label="열린 파일">
            {tabs.map((t) => (
              <span key={t.path} className={"file-tab" + (t.path === activeTab ? " active" : "")}>
                <button
                  role="tab"
                  id={`file-tab-${tabDomId(t.path)}`}
                  aria-selected={t.path === activeTab}
                  aria-controls={`file-tabpanel-${tabDomId(t.path)}`}
                  className="file-tab-label"
                  title={t.path}
                  onClick={() => setActiveTab(t.path)}
                >
                  {t.dirty ? "● " : ""}
                  {t.name}
                </button>
                <button
                  className="file-tab-close"
                  aria-label={`${t.name} 닫기`}
                  onClick={() => requestCloseTab(t.path)}
                >
                  ×
                </button>
              </span>
            ))}
          </div>
          {tabs.map((t) => (
            // Inactive tabs stay MOUNTED (display:none) so the viewer keeps
            // unsaved edits and loaded blobs across tab switches.
            <div
              key={t.path}
              role="tabpanel"
              id={`file-tabpanel-${tabDomId(t.path)}`}
              aria-labelledby={`file-tab-${tabDomId(t.path)}`}
              className="file-tab-body"
              style={t.path === activeTab ? undefined : { display: "none" }}
            >
              <FileViewer
                name={t.name}
                size={t.size}
                load={() => fetchGatewayBlob(filesDownloadUrl(cfg, t.path))}
                onSave={(text) => saveFile(t.path, text)}
                onDirtyChange={(d) => markDirty(t.path, d)}
                downloadUrl={filesDownloadUrl(cfg, t.path)}
              />
            </div>
          ))}
        </div>
      )}
      {makingFolder && (
        <OneFieldModal
          title="새 폴더"
          label="폴더 이름"
          action="생성"
          onClose={() => setMakingFolder(false)}
          onSubmit={(v) => void mkdir(v)}
        />
      )}
      {moving && (
        <OneFieldModal
          title="파일 이동"
          label="대상 경로"
          initialValue={entryPath(moving)}
          action="이동"
          onClose={() => setMoving(null)}
          onSubmit={(v) => void move(moving, v)}
        />
      )}
      {deleting && (
        <DeleteModal
          title="파일 삭제"
          path={entryPath(deleting)}
          onClose={() => setDeleting(null)}
          onDelete={() => void deleteEntry(deleting)}
        />
      )}
      {closingTab && (
        <Modal
          title="저장하지 않은 변경"
          onClose={() => setClosingTab(null)}
          width={420}
          footer={
            <>
              <button className="btn" onClick={() => setClosingTab(null)}>
                계속 편집
              </button>
              <button className="btn" style={{ color: color.danger }} onClick={() => closeTab(closingTab)}>
                버리고 닫기
              </button>
            </>
          }
        >
          <p style={{ ...muted, margin: 0 }}>{closingTab}에 저장하지 않은 변경이 있습니다.</p>
        </Modal>
      )}
    </>
  );
}

const FILES_RESOURCE = "files";

// Stable DOM id fragment for a tab path (role=tab ↔ role=tabpanel linkage).
// encodeURIComponent keeps the id free of spaces/quotes whatever the path holds.
function tabDomId(p: string): string {
  return encodeURIComponent(p);
}

interface FilesListResponse {
  entries?: FileEntry[];
  path?: string;
}

interface FilesSearchResponse {
  entries?: FileEntry[];
}

function filesListParams(path: string): Record<string, unknown> {
  return { path, limit: 300 };
}

function filesSearchParams(query: string, content: boolean, semantic: boolean): Record<string, unknown> {
  return { query, content, semantic, max: 80 };
}

function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error ?? new Error("file read failed"));
    reader.onload = () => {
      const raw = String(reader.result ?? "");
      resolve(raw.includes(",") ? raw.slice(raw.indexOf(",") + 1) : raw);
    };
    reader.readAsDataURL(file);
  });
}
