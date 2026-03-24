/**
 * Native-accelerated PNG encoding utilities.
 *
 * Delegates to Rust (core-rs) when available, falls back to TypeScript.
 */

import { crc32 as crc32TS, encodePngRgba as encodePngRgbaTS } from "../media/png-encode.js";
import { getNative } from "./loader.js";

/**
 * Compute CRC32 checksum for a buffer.
 * Uses native Rust implementation when available.
 */
export function crc32(buf: Buffer): number {
  const native = getNative();
  if (native) {
    return native.crc32(buf);
  }
  return crc32TS(buf);
}

/**
 * Encode an RGBA buffer as a PNG image.
 * Uses native Rust implementation when available.
 */
export function encodePngRgba(buffer: Buffer, width: number, height: number): Buffer {
  const native = getNative();
  if (native) {
    return native.encodePngRgba(buffer, width, height);
  }
  return encodePngRgbaTS(buffer, width, height);
}
