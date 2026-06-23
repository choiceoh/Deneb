import { useEffect, useRef, useState } from "react";
import { FILES_RPC } from "@/resources";
import type { FileEntry } from "@/types";
import { useRpc } from "@/useRpc";
import { color, ellipsis, muted } from "@/theme";
import { fmtDate } from "@/format";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { Column, Grid, GridNotice, RowBtn } from "@/components/Grid";
import { Field, Modal } from "@/components/Modal";

export function FilesPane() {
  const { connected, cfg } = useWorkspace();
  const { call, status, setStatus, busy } = useRpc(cfg);
  const [path, setPath] = useState("");
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [query, setQuery] = useState("");
  const [searching, setSearching] = useState(false);
  const [searchContent, setSearchContent] = useState(false);
  const [searchSemantic, setSearchSemantic] = useState(false);
  const [selected, setSelected] = useState<FileEntry | null>(null);
  const [shareUrl, setShareUrl] = useState("");
  const [makingFolder, setMakingFolder] = useState(false);
  const [moving, setMoving] = useState<FileEntry | null>(null);
  const [deleting, setDeleting] = useState<FileEntry | null>(null);
  const uploadRef = useRef<HTMLInputElement>(null);

  useRegisterPane(
    undefined,
    entries.length
      ? `[파일 ${searching ? "검색" : path || "/"}]\n${entries
          .map((e) => `- ${isFolder(e) ? "[폴더] " : ""}${entryPath(e)}${e.size ? ` (${formatBytes(e.size)})` : ""}`)
          .join("\n")}`
      : "",
  );

  useEffect(() => {
    if (!connected) return;
    void list("");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, cfg.url, cfg.token]);

  async function list(nextPath = path) {
    const r = await call<{ entries?: FileEntry[]; path?: string }>(
      FILES_RPC.list,
      { path: nextPath, limit: 300 },
      "불러오는 중...",
    );
    if (!r.ok) return;
    setPath(r.data?.path ?? nextPath);
    setEntries(r.data?.entries ?? []);
    setSearching(false);
    setSelected(null);
    setShareUrl("");
    setStatus("");
  }

  async function search() {
    const q = query.trim();
    if (!q) {
      await list(path);
      return;
    }
    const r = await call<{ entries?: FileEntry[] }>(
      FILES_RPC.search,
      { query: q, content: searchContent, semantic: searchSemantic, max: 80 },
      "검색 중...",
    );
    if (!r.ok) return;
    setEntries(r.data?.entries ?? []);
    setSearching(true);
    setSelected(null);
    setShareUrl("");
    setStatus((r.data?.entries ?? []).length ? "" : "검색 결과 없음");
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
    await list(path);
    setStatus("폴더 생성됨");
  }

  async function move(entry: FileEntry, dst: string) {
    const target = dst.trim();
    if (!target) return;
    const r = await call(FILES_RPC.move, { src: entryPath(entry), dst: target }, "이동 중...");
    if (!r.ok) return;
    setMoving(null);
    await list(path);
    setStatus("이동됨");
  }

  async function deleteEntry(entry: FileEntry) {
    const r = await call(FILES_RPC.delete, { path: entryPath(entry) }, "삭제 중...");
    if (!r.ok) return;
    setDeleting(null);
    setSelected(null);
    await list(path);
    setStatus("삭제됨");
  }

  async function upload(file: File) {
    const dataBase64 = await fileToBase64(file);
    const r = await call(
      FILES_RPC.upload,
      { path: joinPath(path, file.name), mimeType: file.type, dataBase64 },
      "업로드 중...",
    );
    if (!r.ok) return;
    await list(path);
    setStatus("업로드됨");
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
            else setSelected(e);
          }}
          isRowSelected={(e) => entryPath(e) === entryPath(selected ?? {})}
          rowTitle={(e) => entryPath(e)}
        />
      </GridNotice>
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
          path={entryPath(deleting)}
          onClose={() => setDeleting(null)}
          onDelete={() => void deleteEntry(deleting)}
        />
      )}
    </>
  );
}

function entryPath(entry: FileEntry): string {
  return entry.pathDisplay ?? entry.pathLower ?? entry.name ?? "";
}

function isFolder(entry: FileEntry): boolean {
  return entry.tag === "folder" || entry.tag === "dir";
}

function joinPath(base: string, name: string): string {
  const cleanName = name.replace(/^\/+/, "");
  const cleanBase = base.replace(/\/+$/, "");
  return cleanBase ? `${cleanBase}/${cleanName}` : cleanName;
}

function parentPath(value: string): string {
  const clean = value.replace(/^\/+|\/+$/g, "");
  if (!clean) return "";
  const parts = clean.split("/");
  parts.pop();
  return parts.join("/");
}

function formatBytes(size?: number): string {
  if (typeof size !== "number" || !Number.isFinite(size) || size <= 0) return "";
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
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

function OneFieldModal({
  title,
  label,
  initialValue = "",
  action,
  onClose,
  onSubmit,
}: {
  title: string;
  label: string;
  initialValue?: string;
  action: string;
  onClose: () => void;
  onSubmit: (value: string) => void;
}) {
  const [value, setValue] = useState(initialValue);
  return (
    <Modal
      title={title}
      onClose={onClose}
      width={440}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            취소
          </button>
          <button className="btn btn-accent" onClick={() => onSubmit(value)} disabled={!value.trim()}>
            {action}
          </button>
        </>
      }
    >
      <Field label={label}>
        <input className="field" value={value} onChange={(e) => setValue(e.target.value)} autoFocus />
      </Field>
    </Modal>
  );
}

function DeleteModal({ path, onClose, onDelete }: { path: string; onClose: () => void; onDelete: () => void }) {
  return (
    <Modal
      title="파일 삭제"
      onClose={onClose}
      width={420}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            취소
          </button>
          <button className="btn btn-accent" onClick={onDelete}>
            삭제
          </button>
        </>
      }
    >
      <p style={{ ...muted, margin: 0 }}>{path}</p>
    </Modal>
  );
}
