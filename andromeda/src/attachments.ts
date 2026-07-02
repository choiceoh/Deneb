import { inferAttachmentMimeType } from "@/attachmentMime";

// Attachment intake shared by the chat surfaces (right Deneb panel · chat tab).
// The clip button, drag-drop, and clipboard paste all funnel through here so every
// intake path enforces the same rules: which files capture can handle, the size
// ceiling, and base64 reading. The picker's `accept` list only filters the file
// dialog — drops and pastes bypass it, hence this explicit guard.

// Document MIMEs capture's document extractor accepts — mirrors MIME_BY_EXTENSION
// in attachmentMime.ts (images/audio are matched by prefix instead).
const DOCUMENT_MIMES = new Set([
  "application/msword",
  "application/pdf",
  "application/vnd.ms-excel",
  "application/vnd.ms-powerpoint",
  "application/vnd.openxmlformats-officedocument.presentationml.presentation",
  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  "text/csv",
  "text/plain",
]);

// The base64 payload rides in a single JSON RPC body — cap the file size well below
// any gateway body limit so a stray huge file fails fast with a friendly notice
// instead of freezing the UI mid-encode and erroring server-side.
export const MAX_ATTACH_MB = 25;
const MAX_ATTACH_BYTES = MAX_ATTACH_MB * 1024 * 1024;

export function isAttachableMime(mime: string): boolean {
  return mime.startsWith("image/") || mime.startsWith("audio/") || DOCUMENT_MIMES.has(mime);
}

export interface AttachIntake {
  ok: File[];
  // Per-file skip reasons, ready to show verbatim (Korean) above the composer.
  skipped: string[];
}

// Split an intake batch into attachable files and skip notices, preserving order.
export function splitAttachable(files: File[]): AttachIntake {
  const ok: File[] = [];
  const skipped: string[] = [];
  for (const file of files) {
    const mime = inferAttachmentMimeType(file.name, file.type);
    if (!isAttachableMime(mime)) skipped.push(`${file.name} — 지원하지 않는 형식이라 건너뜀`);
    else if (file.size > MAX_ATTACH_BYTES) skipped.push(`${file.name} — ${MAX_ATTACH_MB}MB 초과라 건너뜀`);
    else ok.push(file);
  }
  return { ok, skipped };
}

// Read a file into the bare base64 payload (no data-URL prefix) for capture RPCs.
export function readFileBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result).split(",")[1] ?? "");
    reader.onerror = () => reject(reader.error ?? new Error(`failed to read ${file.name}`));
    reader.readAsDataURL(file);
  });
}
