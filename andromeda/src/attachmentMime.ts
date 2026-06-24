const MIME_BY_EXTENSION: Record<string, string> = {
  csv: "text/csv",
  doc: "application/msword",
  docx: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  gif: "image/gif",
  jpeg: "image/jpeg",
  jpg: "image/jpeg",
  m4a: "audio/mp4",
  mp3: "audio/mpeg",
  ogg: "audio/ogg",
  pdf: "application/pdf",
  png: "image/png",
  ppt: "application/vnd.ms-powerpoint",
  pptx: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
  txt: "text/plain",
  wav: "audio/wav",
  webm: "audio/webm",
  webp: "image/webp",
  xls: "application/vnd.ms-excel",
  xlsx: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
};

// Browser File.type is best-effort and is often empty for drag/drop or OS-backed
// files. Infer from the filename for supported attachment types so capture RPCs
// receive the intended image/audio/document MIME instead of application/octet-stream.
export function inferAttachmentMimeType(filename: string, browserType: string): string {
  const type = browserType.trim();
  if (type) return type;
  const ext = filename.split(".").pop()?.toLowerCase();
  return (ext && MIME_BY_EXTENSION[ext]) || "application/octet-stream";
}
