/**
 * Native module loader for @deneb/core-rs.
 *
 * Attempts to load the napi-rs native addon at startup.
 * Falls back gracefully to null when the native module is unavailable
 * (e.g. unsupported platform, missing prebuilt binary, dev environment).
 */

// The native module interface — matches napi-rs generated bindings.
export interface CoreRsNative {
  // safe_regex
  hasNestedRepetition(source: string): boolean;

  // exif
  readJpegExifOrientation(buffer: Buffer): number | null;

  // png
  crc32(buf: Buffer): number;
  encodePngRgba(buffer: Buffer, width: number, height: number): Buffer;

  // external_content
  detectSuspiciousPatterns(content: string): string[];
  foldMarkerText(input: string): string;
  replaceMarkers(content: string): string;

  // mime_utils
  normalizeMimeType(mime: string): string | null;
  isGenericMime(mime: string): boolean;
}

let native: CoreRsNative | null = null;
let loadAttempted = false;

/**
 * Get the native core-rs module, or null if unavailable.
 * The load is attempted once and cached.
 */
export function getNative(): CoreRsNative | null {
  if (!loadAttempted) {
    loadAttempted = true;
    try {
      // Use dynamic require to avoid bundler issues.
      // The @deneb/core-rs package must be installed for this to work.
      // eslint-disable-next-line @typescript-eslint/no-require-imports
      native = require("@deneb/core-rs") as CoreRsNative;
    } catch {
      // Native module not available — fall back to TS implementations.
      native = null;
    }
  }
  return native;
}

/**
 * Check if the native module is available.
 */
export function isNativeAvailable(): boolean {
  return getNative() !== null;
}
