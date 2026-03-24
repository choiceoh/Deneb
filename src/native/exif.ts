/**
 * Native-accelerated JPEG EXIF orientation reader.
 *
 * Delegates to Rust (core-rs) when available, falls back to TypeScript.
 */

import { getNative } from "./loader.js";

// Re-export the TS implementation for direct use when needed.
// The TS function is not exported from image-ops.ts, so we provide it here
// as a standalone fallback.

/**
 * TypeScript fallback: read JPEG EXIF orientation from buffer.
 * Inlined from `src/media/image-ops.ts` (the function is not exported there).
 */
function readJpegExifOrientationTS(buffer: Buffer): number | null {
  if (buffer.length < 2 || buffer[0] !== 0xff || buffer[1] !== 0xd8) {
    return null;
  }

  let offset = 2;
  while (offset < buffer.length - 4) {
    if (buffer[offset] !== 0xff) {
      offset++;
      continue;
    }

    const marker = buffer[offset + 1];
    if (marker === 0xff) {
      offset++;
      continue;
    }

    if (marker === 0xe1) {
      const exifStart = offset + 4;
      if (
        buffer.length > exifStart + 6 &&
        buffer.toString("ascii", exifStart, exifStart + 4) === "Exif" &&
        buffer[exifStart + 4] === 0 &&
        buffer[exifStart + 5] === 0
      ) {
        const tiffStart = exifStart + 6;
        if (buffer.length < tiffStart + 8) {
          return null;
        }

        const byteOrder = buffer.toString("ascii", tiffStart, tiffStart + 2);
        const isLittleEndian = byteOrder === "II";

        const readU16 = (pos: number) =>
          isLittleEndian ? buffer.readUInt16LE(pos) : buffer.readUInt16BE(pos);
        const readU32 = (pos: number) =>
          isLittleEndian ? buffer.readUInt32LE(pos) : buffer.readUInt32BE(pos);

        const ifd0Offset = readU32(tiffStart + 4);
        const ifd0Start = tiffStart + ifd0Offset;
        if (buffer.length < ifd0Start + 2) {
          return null;
        }

        const numEntries = readU16(ifd0Start);
        for (let i = 0; i < numEntries; i++) {
          const entryOffset = ifd0Start + 2 + i * 12;
          if (buffer.length < entryOffset + 12) {
            break;
          }

          const tag = readU16(entryOffset);
          if (tag === 0x0112) {
            const value = readU16(entryOffset + 8);
            return value >= 1 && value <= 8 ? value : null;
          }
        }
      }
      return null;
    }

    if (marker >= 0xe0 && marker <= 0xef) {
      const segmentLength = buffer.readUInt16BE(offset + 2);
      offset += 2 + segmentLength;
      continue;
    }

    if (marker === 0xc0 || marker === 0xda) {
      break;
    }

    offset++;
  }

  return null;
}

/**
 * Read JPEG EXIF orientation from a buffer.
 * Returns orientation (1-8) or null if not found.
 * Uses native Rust implementation when available.
 */
export function readJpegExifOrientation(buffer: Buffer): number | null {
  const native = getNative();
  if (native) {
    return native.readJpegExifOrientation(buffer);
  }
  return readJpegExifOrientationTS(buffer);
}
