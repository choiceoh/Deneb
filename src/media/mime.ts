import path from "node:path";
import { loadCoreRs, type CoreRsModule } from "../bindings/core-rs.js";
import { type MediaKind, mediaKindFromMime } from "./constants.js";

// Cache the bound detectMime function to avoid repeated loadCoreRs() + property lookup.
let _nativeDetectMime: CoreRsModule["detectMime"] | undefined;
function nativeDetectMime(): CoreRsModule["detectMime"] {
  if (_nativeDetectMime === undefined) {
    _nativeDetectMime = loadCoreRs().detectMime.bind(loadCoreRs());
  }
  return _nativeDetectMime;
}

// Map common mimes to preferred file extensions.
const EXT_BY_MIME: Record<string, string> = {
  "image/heic": ".heic",
  "image/heif": ".heif",
  "image/jpeg": ".jpg",
  "image/png": ".png",
  "image/webp": ".webp",
  "image/gif": ".gif",
  "audio/ogg": ".ogg",
  "audio/mpeg": ".mp3",
  "audio/wav": ".wav",
  "audio/flac": ".flac",
  "audio/aac": ".aac",
  "audio/opus": ".opus",
  "audio/x-m4a": ".m4a",
  "audio/mp4": ".m4a",
  "video/mp4": ".mp4",
  "video/quicktime": ".mov",
  "application/pdf": ".pdf",
  "application/json": ".json",
  "application/zip": ".zip",
  "application/gzip": ".gz",
  "application/x-tar": ".tar",
  "application/x-7z-compressed": ".7z",
  "application/vnd.rar": ".rar",
  "application/msword": ".doc",
  "application/vnd.ms-excel": ".xls",
  "application/vnd.ms-powerpoint": ".ppt",
  "application/vnd.openxmlformats-officedocument.wordprocessingml.document": ".docx",
  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": ".xlsx",
  "application/vnd.openxmlformats-officedocument.presentationml.presentation": ".pptx",
  "text/csv": ".csv",
  "text/plain": ".txt",
  "text/markdown": ".md",
};

const MIME_BY_EXT: Record<string, string> = {
  ...Object.fromEntries(Object.entries(EXT_BY_MIME).map(([mime, ext]) => [ext, mime])),
  // Additional extension aliases
  ".jpeg": "image/jpeg",
  ".js": "text/javascript",
};

const AUDIO_FILE_EXTENSIONS = new Set([
  ".aac",
  ".caf",
  ".flac",
  ".m4a",
  ".mp3",
  ".oga",
  ".ogg",
  ".opus",
  ".wav",
]);

export function normalizeMimeType(mime?: string | null): string | undefined {
  if (!mime) {
    return undefined;
  }
  const cleaned = mime.split(";")[0]?.trim().toLowerCase();
  return cleaned || undefined;
}

/**
 * Synchronous MIME sniff using native Rust detection.
 * Returns undefined if type is unknown.
 */
export function sniffMimeSync(buffer?: Buffer): string | undefined {
  if (!buffer) {
    return undefined;
  }
  const mime = nativeDetectMime()(buffer);
  return mime && mime !== "application/octet-stream" ? mime : undefined;
}

function sniffMime(buffer?: Buffer): string | undefined {
  if (!buffer) {
    return undefined;
  }
  const mime = nativeDetectMime()(buffer);
  return mime && mime !== "application/octet-stream" ? mime : undefined;
}

export function getFileExtension(filePath?: string | null): string | undefined {
  if (!filePath) {
    return undefined;
  }
  try {
    if (/^https?:\/\//i.test(filePath)) {
      const url = new URL(filePath);
      return path.extname(url.pathname).toLowerCase() || undefined;
    }
  } catch {
    // fall back to plain path parsing
  }
  const ext = path.extname(filePath).toLowerCase();
  return ext || undefined;
}

export function isAudioFileName(fileName?: string | null): boolean {
  const ext = getFileExtension(fileName);
  if (!ext) {
    return false;
  }
  return AUDIO_FILE_EXTENSIONS.has(ext);
}

export function detectMime(opts: {
  buffer?: Buffer;
  headerMime?: string | null;
  filePath?: string;
}): string | undefined {
  return detectMimeImpl(opts);
}

/**
 * Scan a zip buffer for OOXML markers to refine `application/zip` to a specific
 * Office Open XML type (xlsx, docx, pptx). Searches the raw buffer for known
 * zip local file header filenames without full zip parsing.
 */
function sniffOoxmlFromZipBuffer(buffer: Buffer): string | undefined {
  // Scan for internal path markers unique to each OOXML format.
  // These appear as local file header filenames in the zip directory.
  if (buffer.includes("xl/workbook.xml") || buffer.includes("xl/")) {
    return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet";
  }
  if (buffer.includes("word/document.xml") || buffer.includes("word/")) {
    return "application/vnd.openxmlformats-officedocument.wordprocessingml.document";
  }
  if (buffer.includes("ppt/presentation.xml") || buffer.includes("ppt/")) {
    return "application/vnd.openxmlformats-officedocument.presentationml.presentation";
  }
  return undefined;
}

function isGenericMime(mime?: string): boolean {
  if (!mime) {
    return true;
  }
  const m = mime.toLowerCase();
  return m === "application/octet-stream" || m === "application/zip";
}

function detectMimeImpl(opts: {
  buffer?: Buffer;
  headerMime?: string | null;
  filePath?: string;
}): string | undefined {
  const ext = getFileExtension(opts.filePath);
  const extMime = ext ? MIME_BY_EXT[ext] : undefined;

  const headerMime = normalizeMimeType(opts.headerMime);
  const sniffed = sniffMime(opts.buffer);

  // Prefer sniffed types, but don't let generic container types override a more
  // specific extension mapping (e.g. XLSX vs ZIP).
  if (sniffed && (!isGenericMime(sniffed) || !extMime)) {
    // For zip-based files, try to refine to OOXML (xlsx/docx/pptx) by scanning
    // the buffer for known internal filenames. This avoids full zip parsing.
    if (sniffed === "application/zip" && opts.buffer) {
      const refined = sniffOoxmlFromZipBuffer(opts.buffer);
      if (refined) {
        return refined;
      }
    }
    return sniffed;
  }
  if (extMime) {
    return extMime;
  }
  if (headerMime && !isGenericMime(headerMime)) {
    return headerMime;
  }
  if (sniffed) {
    return sniffed;
  }
  if (headerMime) {
    return headerMime;
  }

  return undefined;
}

export function extensionForMime(mime?: string | null): string | undefined {
  const normalized = normalizeMimeType(mime);
  if (!normalized) {
    return undefined;
  }
  return EXT_BY_MIME[normalized];
}

export function isGifMedia(opts: {
  contentType?: string | null;
  fileName?: string | null;
}): boolean {
  if (opts.contentType?.toLowerCase() === "image/gif") {
    return true;
  }
  const ext = getFileExtension(opts.fileName);
  return ext === ".gif";
}

export function imageMimeFromFormat(format?: string | null): string | undefined {
  if (!format) {
    return undefined;
  }
  switch (format.toLowerCase()) {
    case "jpg":
    case "jpeg":
      return "image/jpeg";
    case "heic":
      return "image/heic";
    case "heif":
      return "image/heif";
    case "png":
      return "image/png";
    case "webp":
      return "image/webp";
    case "gif":
      return "image/gif";
    default:
      return undefined;
  }
}

export function kindFromMime(mime?: string | null): MediaKind | undefined {
  return mediaKindFromMime(normalizeMimeType(mime));
}
