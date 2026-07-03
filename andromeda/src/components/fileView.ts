// fileView — pure helpers behind the in-app file viewer: which viewer a file
// gets, whether it is text-editable, and tiny no-dependency parsers for CSV
// tables and diff coloring. No React, fully unit-testable.

export type ViewKind = "image" | "pdf" | "markdown" | "csv" | "diff" | "text" | "hwp" | "none";

const IMAGE_EXTS = new Set(["png", "jpg", "jpeg", "gif", "svg", "webp", "bmp", "ico", "tiff", "avif"]);

// Text-ish extensions that open in the editor. Broad on purpose — a wrong
// "text" guess degrades to a harmless mono view, a wrong "none" hides content.
const TEXT_EXTS = new Set([
  "txt",
  "log",
  "json",
  "jsonl",
  "yaml",
  "yml",
  "toml",
  "ini",
  "conf",
  "env",
  "xml",
  "html",
  "htm",
  "css",
  "js",
  "jsx",
  "ts",
  "tsx",
  "go",
  "py",
  "java",
  "kt",
  "kts",
  "swift",
  "rs",
  "c",
  "h",
  "cpp",
  "hpp",
  "cs",
  "rb",
  "php",
  "sh",
  "bash",
  "zsh",
  "ps1",
  "bat",
  "sql",
  "gradle",
  "properties",
  "proto",
  "tf",
  "lua",
]);

export function extOf(name: string): string {
  const base = name.split("/").pop() ?? name;
  const i = base.lastIndexOf(".");
  return i < 0 ? "" : base.slice(i + 1).toLowerCase();
}

// viewKindFor picks the viewer for a filename (+ optional MIME hint). The
// extension wins — servers routinely send octet-stream for everything.
export function viewKindFor(name: string, mime?: string): ViewKind {
  const ext = extOf(name);
  if (IMAGE_EXTS.has(ext)) return "image";
  if (ext === "pdf") return "pdf";
  if (ext === "md" || ext === "markdown") return "markdown";
  if (ext === "csv" || ext === "tsv") return "csv";
  if (ext === "diff" || ext === "patch") return "diff";
  if (ext === "hwp") return "hwp"; // 한/글 5.x — in-app text extraction (see hwp/)
  if (TEXT_EXTS.has(ext)) return "text";
  const m = (mime ?? "").toLowerCase();
  if (m.startsWith("image/")) return "image";
  if (m === "application/pdf") return "pdf";
  if (m.startsWith("text/") || m === "application/json") return "text";
  return "none";
}

// Editable = the viewer can hand the content back as text for a save.
export function isEditableKind(kind: ViewKind): boolean {
  return kind === "markdown" || kind === "csv" || kind === "diff" || kind === "text";
}

// maxTextPreviewBytes caps what the editor loads — a 100MB log would freeze
// the webview; past the cap the viewer degrades to the download link.
export const maxTextPreviewBytes = 2 << 20; // 2 MiB

// parseCsv is a small RFC-4180-ish parser (quoted fields, embedded commas and
// newlines, "" escapes). TSV rides the same path with a tab delimiter.
export function parseCsv(text: string, delimiter = ","): string[][] {
  const rows: string[][] = [];
  let field = "";
  let row: string[] = [];
  let quoted = false;
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if (quoted) {
      if (ch === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i++;
        } else {
          quoted = false;
        }
      } else {
        field += ch;
      }
      continue;
    }
    if (ch === '"' && field === "") {
      quoted = true;
    } else if (ch === delimiter) {
      row.push(field);
      field = "";
    } else if (ch === "\n" || ch === "\r") {
      if (ch === "\r" && text[i + 1] === "\n") i++;
      row.push(field);
      rows.push(row);
      field = "";
      row = [];
    } else {
      field += ch;
    }
  }
  if (field !== "" || row.length > 0) {
    row.push(field);
    rows.push(row);
  }
  return rows;
}

// diffLineClass maps a unified-diff line to its display class ("" = context).
export function diffLineClass(line: string): "add" | "del" | "hunk" | "meta" | "" {
  if (line.startsWith("+++") || line.startsWith("---")) return "meta";
  if (line.startsWith("@@")) return "hunk";
  if (line.startsWith("+")) return "add";
  if (line.startsWith("-")) return "del";
  if (line.startsWith("diff ") || line.startsWith("index ")) return "meta";
  return "";
}

// textToBase64 encodes UTF-8 text for the upload RPC's dataBase64 field.
export function textToBase64(text: string): string {
  const bytes = new TextEncoder().encode(text);
  let bin = "";
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) {
    bin += String.fromCharCode(...bytes.subarray(i, i + chunk));
  }
  return btoa(bin);
}
