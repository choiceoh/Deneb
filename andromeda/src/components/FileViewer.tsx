import { useEffect, useRef, useState } from "react";
import { color, muted } from "@/theme";
import { MarkdownEditor } from "@/components/MarkdownEditor";
import {
  diffLineClass,
  isEditableKind,
  maxHwpBytes,
  maxTextPreviewBytes,
  parseCsv,
  viewKindFor,
} from "@/components/fileView";
import { type HwpBlock, parseHwp } from "@/components/hwp/hwp";

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
  size,
  load,
  onSave,
  onDirtyChange,
  downloadUrl,
}: {
  name: string;
  mime?: string;
  // Known byte size (the files grid has it) — lets the size caps refuse BEFORE
  // downloading the whole blob. Unknown sizes fall back to the post-load check.
  size?: number;
  load: () => Promise<Blob>;
  onSave?: (text: string) => Promise<boolean>;
  onDirtyChange?: (dirty: boolean) => void;
  downloadUrl?: string;
}) {
  const kind = viewKindFor(name, mime);
  const preTooBig =
    size !== undefined &&
    size > 0 &&
    (kind === "hwp" ? size > maxHwpBytes : isEditableKind(kind) && size > maxTextPreviewBytes);
  const [phase, setPhase] = useState<"loading" | "ready" | "error" | "toobig">(
    preTooBig ? "toobig" : kind === "none" ? "ready" : "loading",
  );
  const [errText, setErrText] = useState("");
  const [objectUrl, setObjectUrl] = useState("");
  const [text, setText] = useState("");
  const [savedText, setSavedText] = useState("");
  const [hwpBlocks, setHwpBlocks] = useState<HwpBlock[]>([]);
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
    if (kind === "none" || preTooBig) return;
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
        if (kind === "hwp") {
          // 한/글 5.x: extract content in-browser (dependency-free parser) —
          // paragraphs, tables, and embedded images. Guard the blob size first:
          // inflate + record-walk runs on the UI thread, so an unbounded file
          // would freeze the webview (degrade to the download link instead).
          if (blob.size > maxHwpBytes) {
            setPhase("toobig");
            return;
          }
          const doc = await parseHwp(await blob.arrayBuffer());
          if (!alive) return;
          setHwpBlocks(doc.blocks);
          setText(doc.text);
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
    try {
      // finally-guarded: an onSave rejection must not leave saving=true stuck
      // (the 저장/되돌리기 buttons would stay disabled forever).
      const ok = await onSave(text);
      if (ok) setSavedText(text);
    } finally {
      setSaving(false);
    }
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
        <p style={muted}>파일이 미리보기 한도를 넘습니다. 내려받아 확인하세요.</p>
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
  if (kind === "hwp") {
    return (
      <div className="file-viewer-text">
        <div className="file-viewer-bar">
          <span style={muted}>한글 문서 — 텍스트·표·이미지 추출 (서식·레이아웃은 원본에서)</span>
          {downloadLink}
        </div>
        <HwpView blocks={hwpBlocks} name={name} />
      </div>
    );
  }

  // Text-family: shared editor chrome (모드 탭 + 저장) over per-kind rendering.
  // Only genuinely text-backed kinds are editable — HWP is EXTRACTED text
  // (saving it would corrupt the binary original), so it stays read-only even
  // inside the file-tab (which passes an onSave).
  const editable = Boolean(onSave) && isEditableKind(kind);
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

// HwpView renders the extracted HWP content — paragraphs, tables (as a grid),
// and images — read-only. Images are data: URIs the parser built from the
// document's own BinData streams (image bytes only, never markup), so an <img>
// src is safe here. Empty extraction shows a hint + download escape hatch.
function HwpView({ blocks, name }: { blocks: HwpBlock[]; name: string }) {
  if (blocks.length === 0) {
    return (
      <p style={muted}>이 문서에서 추출할 수 있는 텍스트·표·이미지를 찾지 못했습니다. 원본을 내려받아 확인하세요.</p>
    );
  }
  return (
    <div className="file-viewer-scroll hwp-doc" aria-label={name}>
      {blocks.map((b, i) => {
        if (b.type === "para") return <p key={i}>{b.text}</p>;
        if (b.type === "image") {
          return (
            <figure key={i} className="hwp-figure">
              <img src={b.dataUri} alt={b.name} loading="lazy" />
            </figure>
          );
        }
        return (
          <table key={i} className="file-viewer-csv hwp-table">
            <tbody>
              {b.rows.map((row, ri) => (
                <tr key={ri}>
                  {row.map((cell, ci) => (
                    <td key={ci}>{cell}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        );
      })}
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

// DiffView colorizes unified-diff lines (added/removed/hunk/meta). A <div>
// wrapper, not <pre> — flow content (the line <div>s) inside <pre> is invalid
// HTML; the mono font/size come from .file-viewer-diff and the line whitespace
// from .diff-line (pre-wrap), so nothing relied on the <pre> element itself.
function DiffView({ text }: { text: string }) {
  return (
    <div className="file-viewer-scroll file-viewer-diff">
      {text.split("\n").map((line, i) => (
        <div key={i} className={"diff-line" + (diffLineClass(line) ? " diff-" + diffLineClass(line) : "")}>
          {line || " "}
        </div>
      ))}
    </div>
  );
}
