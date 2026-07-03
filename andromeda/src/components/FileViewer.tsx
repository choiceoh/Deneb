import { useEffect, useRef, useState } from "react";
import { color, muted } from "@/theme";
import { MarkdownEditor } from "@/components/MarkdownEditor";
import { diffLineClass, isEditableKind, maxTextPreviewBytes, parseCsv, viewKindFor } from "@/components/fileView";

// FileViewer renders one file inline — the AionUi-style preview sized to
// Deneb: images and PDFs straight off a blob, markdown through the existing
// MarkdownEditor (preview/edit), CSV as a table, diffs colorized, code/text in
// a mono editor. When onSave is provided the text kinds become live-editable
// (저장/되돌리기); without it the viewer is read-only (mail attachments).
//
// The component owns its content state, so a parent that keeps it MOUNTED
// (display:none for inactive tabs) preserves edits across tab switches.
export function FileViewer({
  name,
  mime,
  load,
  onSave,
  onDirtyChange,
  downloadUrl,
}: {
  name: string;
  mime?: string;
  load: () => Promise<Blob>;
  onSave?: (text: string) => Promise<boolean>;
  onDirtyChange?: (dirty: boolean) => void;
  downloadUrl?: string;
}) {
  const kind = viewKindFor(name, mime);
  const [phase, setPhase] = useState<"loading" | "ready" | "error" | "toobig">(kind === "none" ? "ready" : "loading");
  const [errText, setErrText] = useState("");
  const [objectUrl, setObjectUrl] = useState("");
  const [text, setText] = useState("");
  const [savedText, setSavedText] = useState("");
  const [preview, setPreview] = useState(true); // markdown/csv/diff: rendered view first
  const [saving, setSaving] = useState(false);
  const dirty = isEditableKind(kind) && text !== savedText;
  const dirtyRef = useRef(false);

  useEffect(() => {
    if (dirtyRef.current !== dirty) {
      dirtyRef.current = dirty;
      onDirtyChange?.(dirty);
    }
  }, [dirty, onDirtyChange]);

  useEffect(() => {
    if (kind === "none") return;
    let alive = true;
    let url = "";
    void (async () => {
      try {
        const blob = await load();
        if (!alive) return;
        if (kind === "image" || kind === "pdf") {
          url = URL.createObjectURL(blob);
          setObjectUrl(url);
          setPhase("ready");
          return;
        }
        if (blob.size > maxTextPreviewBytes) {
          setPhase("toobig");
          return;
        }
        const body = await blob.text();
        if (!alive) return;
        setText(body);
        setSavedText(body);
        setPhase("ready");
      } catch (e) {
        if (!alive) return;
        setErrText(e instanceof Error ? e.message : String(e));
        setPhase("error");
      }
    })();
    return () => {
      alive = false;
      // Guard revoke: some environments (and teardown ordering in tests) can
      // strip URL.revokeObjectURL out from under an unmounting component.
      if (url && typeof URL.revokeObjectURL === "function") URL.revokeObjectURL(url);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function save() {
    if (!onSave || saving) return;
    setSaving(true);
    const ok = await onSave(text);
    setSaving(false);
    if (ok) setSavedText(text);
  }

  const downloadLink = downloadUrl && (
    <a className="row-btn" href={downloadUrl} target="_blank" rel="noreferrer">
      다운로드
    </a>
  );

  if (kind === "none") {
    return (
      <div className="file-viewer-empty">
        <p style={muted}>이 형식({name.split(".").pop()})은 미리보기를 지원하지 않습니다.</p>
        {downloadLink}
      </div>
    );
  }
  if (phase === "loading") return <p style={muted}>불러오는 중...</p>;
  if (phase === "error") {
    return (
      <div className="file-viewer-empty">
        <p style={{ ...muted, color: color.danger }}>불러오기 실패: {errText}</p>
        {downloadLink}
      </div>
    );
  }
  if (phase === "toobig") {
    return (
      <div className="file-viewer-empty">
        <p style={muted}>파일이 미리보기 한도(2MB)를 넘습니다.</p>
        {downloadLink}
      </div>
    );
  }

  if (kind === "image") {
    return (
      <div className="file-viewer-media">
        <img src={objectUrl} alt={name} />
      </div>
    );
  }
  if (kind === "pdf") {
    return (
      <div className="file-viewer-media file-viewer-pdf">
        <embed src={objectUrl} type="application/pdf" title={name} />
        {downloadLink && <div className="file-viewer-medialinks">{downloadLink}</div>}
      </div>
    );
  }

  // Text-family: shared editor chrome (모드 탭 + 저장) over per-kind rendering.
  const editable = Boolean(onSave);
  const hasPreviewMode = kind === "markdown" || kind === "csv" || kind === "diff";
  const showPreview = hasPreviewMode && preview;

  return (
    <div className="file-viewer-text">
      <div className="file-viewer-bar">
        {hasPreviewMode && (
          <div className="wiki-mode-tabs" role="group" aria-label="보기 방식">
            <button
              className={"wiki-mode-tab" + (!showPreview ? " active" : "")}
              onClick={() => setPreview(false)}
              aria-pressed={!showPreview}
            >
              {editable ? "편집" : "원본"}
            </button>
            <button
              className={"wiki-mode-tab" + (showPreview ? " active" : "")}
              onClick={() => setPreview(true)}
              aria-pressed={showPreview}
            >
              미리보기
            </button>
          </div>
        )}
        {editable && (
          <>
            <button className="btn btn-accent" onClick={() => void save()} disabled={!dirty || saving}>
              저장
            </button>
            <button className="row-btn" onClick={() => setText(savedText)} disabled={!dirty || saving}>
              되돌리기
            </button>
            <span className={"wiki-save-state" + (dirty ? " dirty" : "")}>{dirty ? "수정됨" : "저장됨"}</span>
          </>
        )}
        {downloadLink}
      </div>
      {kind === "markdown" ? (
        <MarkdownEditor
          value={text}
          onChange={setText}
          preview={showPreview}
          disabled={!editable}
          fill
          ariaLabel={name}
        />
      ) : showPreview && kind === "csv" ? (
        <CsvTable text={text} tsv={name.toLowerCase().endsWith(".tsv")} />
      ) : showPreview && kind === "diff" ? (
        <DiffView text={text} />
      ) : (
        <textarea
          className="field file-viewer-code"
          value={text}
          onChange={(e) => setText(e.target.value)}
          readOnly={!editable}
          spellCheck={false}
          aria-label={name}
        />
      )}
    </div>
  );
}

// CsvTable renders parsed rows, capped so a huge export can't freeze the pane.
function CsvTable({ text, tsv }: { text: string; tsv: boolean }) {
  const rows = parseCsv(text, tsv ? "\t" : ",");
  const capped = rows.length > 500;
  const shown = capped ? rows.slice(0, 500) : rows;
  if (shown.length === 0) return <p style={muted}>빈 파일</p>;
  const [head, ...body] = shown;
  return (
    <div className="file-viewer-scroll">
      <table className="file-viewer-csv">
        <thead>
          <tr>
            {head.map((c, i) => (
              <th key={i}>{c}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {body.map((r, i) => (
            <tr key={i}>
              {r.map((c, j) => (
                <td key={j}>{c}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      {capped && <p style={muted}>전체 {rows.length}행 중 500행 표시 (편집 모드에서 전문 확인)</p>}
    </div>
  );
}

// DiffView colorizes unified-diff lines (added/removed/hunk/meta).
function DiffView({ text }: { text: string }) {
  return (
    <pre className="file-viewer-scroll file-viewer-diff">
      {text.split("\n").map((line, i) => (
        <div key={i} className={"diff-line" + (diffLineClass(line) ? " diff-" + diffLineClass(line) : "")}>
          {line || " "}
        </div>
      ))}
    </pre>
  );
}
