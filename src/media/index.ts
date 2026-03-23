// Public API barrel for src/media/

// MIME utilities
export {
  detectMime,
  extensionForMime,
  getFileExtension,
  imageMimeFromFormat,
  isAudioFileName,
  isGifMedia,
  kindFromMime,
  normalizeMimeType,
} from "./mime.js";

// Media store
export {
  MEDIA_MAX_BYTES,
  SaveMediaSourceError,
  cleanOldMedia,
  ensureMediaDir,
  extractOriginalFilename,
  getMediaDir,
  saveMediaBuffer,
  saveMediaSource,
  setMediaStoreNetworkDepsForTest,
} from "./store.js";
export type { SaveMediaSourceErrorCode, SavedMedia } from "./store.js";

// Base64 utilities
export { canonicalizeBase64, estimateBase64DecodedBytes } from "./base64.js";

// Web media
export {
  LocalMediaAccessError,
  getDefaultLocalRoots,
  loadWebMedia,
  loadWebMediaRaw,
  optimizeImageToJpeg,
} from "./web-media.js";
export type { LocalMediaAccessErrorCode, WebMediaResult } from "./web-media.js";

// Media parsing
export { MEDIA_TOKEN_RE, normalizeMediaSource, splitMediaFromOutput } from "./parse.js";

// MIME sniffing
export { sniffMimeFromBase64 } from "./sniff-mime-from-base64.js";

// Input files
export {
  DEFAULT_INPUT_FILE_MAX_BYTES,
  DEFAULT_INPUT_FILE_MAX_CHARS,
  DEFAULT_INPUT_FILE_MIMES,
  DEFAULT_INPUT_IMAGE_MAX_BYTES,
  DEFAULT_INPUT_IMAGE_MIMES,
  DEFAULT_INPUT_MAX_REDIRECTS,
  DEFAULT_INPUT_PDF_MAX_PAGES,
  DEFAULT_INPUT_PDF_MAX_PIXELS,
  DEFAULT_INPUT_PDF_MIN_TEXT_CHARS,
  DEFAULT_INPUT_TIMEOUT_MS,
  extractFileContentFromSource,
  extractImageContentFromSource,
  fetchWithGuard,
  normalizeMimeList,
  parseContentType,
  resolveInputFileLimits,
} from "./input-files.js";
export type {
  InputFetchResult,
  InputFileExtractResult,
  InputFileLimits,
  InputFileLimitsConfig,
  InputFileSource,
  InputImageContent,
  InputImageLimits,
  InputImageSource,
  InputPdfLimits,
} from "./input-files.js";

// Constants
export {
  MAX_AUDIO_BYTES,
  MAX_DOCUMENT_BYTES,
  MAX_IMAGE_BYTES,
  MAX_VIDEO_BYTES,
  maxBytesForKind,
  mediaKindFromMime,
} from "./constants.js";
export type { MediaKind } from "./constants.js";

// PDF extraction
export { extractPdfContent } from "./pdf-extract.js";
export type { PdfExtractedContent, PdfExtractedImage } from "./pdf-extract.js";
