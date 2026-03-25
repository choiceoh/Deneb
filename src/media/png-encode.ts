/**
 * PNG encoding via native Rust addon.
 * Used for QR codes, live probes, and other programmatic image generation.
 */
import { loadNative } from "../bindings/native.js";

/** Compute CRC32 checksum for a buffer (used in PNG chunk encoding). */
export function crc32(buf: Buffer): number {
  return loadNative().crc32(buf);
}

/** Create a PNG chunk with type, data, and CRC. */
export function pngChunk(type: string, data: Buffer): Buffer {
  const typeBuf = Buffer.from(type, "ascii");
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length, 0);
  const crc = crc32(Buffer.concat([typeBuf, data]));
  const crcBuf = Buffer.alloc(4);
  crcBuf.writeUInt32BE(crc, 0);
  return Buffer.concat([len, typeBuf, data, crcBuf]);
}

/** Write a pixel to an RGBA buffer. Ignores out-of-bounds writes. */
export function fillPixel(
  buf: Buffer,
  x: number,
  y: number,
  width: number,
  r: number,
  g: number,
  b: number,
  a = 255,
): void {
  if (x < 0 || y < 0 || x >= width) {
    return;
  }
  const idx = (y * width + x) * 4;
  if (idx < 0 || idx + 3 >= buf.length) {
    return;
  }
  buf[idx] = r;
  buf[idx + 1] = g;
  buf[idx + 2] = b;
  buf[idx + 3] = a;
}

/** Encode an RGBA buffer as a PNG image. */
export function encodePngRgba(buffer: Buffer, width: number, height: number): Buffer {
  return loadNative().encodePngRgba(buffer, width, height);
}
