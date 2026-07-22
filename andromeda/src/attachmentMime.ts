const MIME_BY_EXTENSION: Record<string, string> = {
  avi: "video/x-msvideo",
  csv: "text/csv",
  doc: "application/msword",
  docx: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  gif: "image/gif",
  // HWP/HWPX (한글): the gateway converts them (hwp5txt / LibreOffice) — the exact
  // MIME only needs to land in DOCUMENT_MIMES; server dispatch is by extension.
  hwp: "application/x-hwp",
  hwpx: "application/x-hwpx",
  jpeg: "image/jpeg",
  jpg: "image/jpeg",
  m4a: "audio/mp4",
  m4v: "video/mp4",
  md: "text/markdown",
  mkv: "video/x-matroska",
  mov: "video/quicktime",
  mp3: "audio/mpeg",
  mp4: "video/mp4",
  odp: "application/vnd.oasis.opendocument.presentation",
  ods: "application/vnd.oasis.opendocument.spreadsheet",
  odt: "application/vnd.oasis.opendocument.text",
  ogg: "audio/ogg",
  pdf: "application/pdf",
  png: "image/png",
  ppt: "application/vnd.ms-powerpoint",
  pptx: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
  rtf: "application/rtf",
  txt: "text/plain",
  wav: "audio/wav",
  webm: "audio/webm",
  webp: "image/webp",
  xls: "application/vnd.ms-excel",
  xlsx: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
};

// Document MIMEs the capture document-extractor accepts — derived from the
// extension map above so the drop/paste intake guard (attachments.ts) can never
// drift from what the picker supports: add an extension here and every intake
// path picks it up. Video is excluded (it rides the audio/ASR path via ffmpeg,
// gated by isAttachableMime's own video check), so it is not a "document" MIME.
export const DOCUMENT_MIMES: ReadonlySet<string> = new Set(
  Object.values(MIME_BY_EXTENSION).filter(
    (m) => !m.startsWith("image/") && !m.startsWith("audio/") && !m.startsWith("video/"),
  ),
);

// Browser File.type is best-effort: often EMPTY for drag/drop files, and often a
// GENERIC application/octet-stream for OS-backed drops/pastes. Both cases infer
// from the filename so capture RPCs receive the intended image/audio/document
// MIME — otherwise a perfectly good contract.pdf gets rejected as unsupported.
export function inferAttachmentMimeType(filename: string, browserType: string): string {
  const type = browserType.trim();
  if (type && type !== "application/octet-stream" && type !== "binary/octet-stream") return type;
  const ext = filename.split(".").pop()?.toLowerCase();
  return (ext && MIME_BY_EXTENSION[ext]) || "application/octet-stream";
}
