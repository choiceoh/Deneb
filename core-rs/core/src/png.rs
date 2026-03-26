//! Minimal PNG encoder for generating simple RGBA images.
//!
//! Ports `crc32`, `pngChunk`, `fillPixel`, and `encodePngRgba` from
//! `src/media/png-encode.ts`.

/// Minimal PNG encoder for generating simple RGBA images.
///
/// Ports `crc32`, `pngChunk`, `fillPixel`, and `encodePngRgba` from
/// `src/media/png-encode.ts`.
use flate2::Compression;
#[cfg(feature = "napi_binding")]
use napi::bindgen_prelude::Buffer;
use std::io::Write;

// ---------------------------------------------------------------------------
// CRC32 table (precomputed at compile time)
// ---------------------------------------------------------------------------

const fn build_crc_table() -> [u32; 256] {
    let mut table = [0u32; 256];
    let mut i = 0u32;
    while i < 256 {
        let mut c = i;
        let mut k = 0;
        while k < 8 {
            if c & 1 != 0 {
                c = 0xEDB8_8320 ^ (c >> 1);
            } else {
                c >>= 1;
            }
            k += 1;
        }
        table[i as usize] = c;
        i += 1;
    }
    table
}

static CRC_TABLE: [u32; 256] = build_crc_table();

// ---------------------------------------------------------------------------
// Implementation
// ---------------------------------------------------------------------------

/// Compute CRC32 checksum for a byte slice.
pub fn crc32_impl(buf: &[u8]) -> u32 {
    let mut crc: u32 = 0xFFFF_FFFF;
    for &byte in buf {
        crc = CRC_TABLE[((crc ^ byte as u32) & 0xFF) as usize] ^ (crc >> 8);
    }
    crc ^ 0xFFFF_FFFF
}

/// Create a PNG chunk: [length][type][data][crc].
fn png_chunk(chunk_type: &[u8; 4], data: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(4 + 4 + data.len() + 4);
    out.extend_from_slice(&(data.len() as u32).to_be_bytes());
    out.extend_from_slice(chunk_type);
    out.extend_from_slice(data);
    // CRC covers type + data
    let mut crc_buf = Vec::with_capacity(4 + data.len());
    crc_buf.extend_from_slice(chunk_type);
    crc_buf.extend_from_slice(data);
    let crc = crc32_impl(&crc_buf);
    out.extend_from_slice(&crc.to_be_bytes());
    out
}

/// Write a pixel to an RGBA buffer. Ignores out-of-bounds writes.
/// `rgba` is `[r, g, b, a]`.
pub fn fill_pixel_impl(buf: &mut [u8], x: i32, y: i32, width: i32, rgba: [u8; 4]) {
    if x < 0 || y < 0 || x >= width || width <= 0 {
        return;
    }
    // Use checked arithmetic to prevent integer overflow on large dimensions.
    let idx = match (y as i64)
        .checked_mul(width as i64)
        .and_then(|v| v.checked_add(x as i64))
        .and_then(|v| v.checked_mul(4))
    {
        Some(v) if v >= 0 => v as usize,
        _ => return,
    };
    if idx + 3 >= buf.len() {
        return;
    }
    buf[idx] = rgba[0];
    buf[idx + 1] = rgba[1];
    buf[idx + 2] = rgba[2];
    buf[idx + 3] = rgba[3];
}

/// Encode an RGBA buffer as a PNG image.
/// Returns an empty Vec if dimensions would cause integer overflow.
pub fn encode_png_rgba_impl(buffer: &[u8], width: u32, height: u32) -> Vec<u8> {
    // Use checked arithmetic to prevent overflow on large dimensions.
    let stride = match (width as usize).checked_mul(4) {
        Some(s) => s,
        None => return Vec::new(),
    };
    let raw_len = match stride
        .checked_add(1)
        .and_then(|s| s.checked_mul(height as usize))
    {
        Some(l) => l,
        None => return Vec::new(),
    };
    let mut raw = vec![0u8; raw_len];

    for row in 0..height as usize {
        let raw_offset = row * (stride + 1);
        raw[raw_offset] = 0; // filter: none
        let src_start = row * stride;
        let src_end = src_start + stride;
        if src_end <= buffer.len() {
            raw[raw_offset + 1..raw_offset + 1 + stride]
                .copy_from_slice(&buffer[src_start..src_end]);
        }
    }

    // PNG IDAT uses zlib (deflate with zlib header)
    let mut encoder = flate2::write::ZlibEncoder::new(Vec::new(), Compression::default());
    if encoder.write_all(&raw).is_err() {
        return Vec::new();
    }
    let compressed = match encoder.finish() {
        Ok(v) => v,
        Err(_) => return Vec::new(),
    };

    // Build PNG
    let signature: [u8; 8] = [0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A];

    let mut ihdr = [0u8; 13];
    ihdr[0..4].copy_from_slice(&width.to_be_bytes());
    ihdr[4..8].copy_from_slice(&height.to_be_bytes());
    ihdr[8] = 8; // bit depth
    ihdr[9] = 6; // color type RGBA
    ihdr[10] = 0; // compression
    ihdr[11] = 0; // filter
    ihdr[12] = 0; // interlace

    let mut out = Vec::new();
    out.extend_from_slice(&signature);
    out.extend_from_slice(&png_chunk(b"IHDR", &ihdr));
    out.extend_from_slice(&png_chunk(b"IDAT", &compressed));
    out.extend_from_slice(&png_chunk(b"IEND", &[]));
    out
}

// ---------------------------------------------------------------------------
// napi exports
// ---------------------------------------------------------------------------

/// Compute CRC32 checksum for a buffer (napi entrypoint).
#[cfg(feature = "napi_binding")]
#[cfg_attr(feature = "napi_binding", napi)]
pub fn crc32(buf: Buffer) -> u32 {
    crc32_impl(&buf)
}

/// Encode an RGBA buffer as a PNG image (napi entrypoint).
#[cfg(feature = "napi_binding")]
#[cfg_attr(feature = "napi_binding", napi)]
pub fn encode_png_rgba(buffer: Buffer, width: u32, height: u32) -> Buffer {
    encode_png_rgba_impl(&buffer, width, height).into()
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn crc32_empty() {
        assert_eq!(crc32_impl(&[]), 0);
    }

    #[test]
    fn crc32_known_values() {
        // CRC32 of "123456789" = 0xCBF43926
        assert_eq!(crc32_impl(b"123456789"), 0xCBF4_3926);
    }

    #[test]
    fn crc32_single_byte() {
        // Known CRC32 for single byte 0x00
        let result = crc32_impl(&[0x00]);
        assert_eq!(result, 0xD202_EF8D);
    }

    #[test]
    fn fill_pixel_basic() {
        let mut buf = vec![0u8; 4 * 4]; // 2x2 image
        fill_pixel_impl(&mut buf, 1, 0, 2, [255, 128, 64, 255]);
        assert_eq!(buf[4], 255); // R
        assert_eq!(buf[5], 128); // G
        assert_eq!(buf[6], 64); // B
        assert_eq!(buf[7], 255); // A
    }

    #[test]
    fn fill_pixel_out_of_bounds() {
        let mut buf = vec![0u8; 16];
        fill_pixel_impl(&mut buf, -1, 0, 2, [255, 0, 0, 255]);
        fill_pixel_impl(&mut buf, 0, -1, 2, [255, 0, 0, 255]);
        assert!(buf.iter().all(|&b| b == 0)); // No writes
    }

    #[test]
    fn encode_png_produces_valid_signature() {
        // 1x1 red pixel
        let buffer = vec![255, 0, 0, 255];
        let png = encode_png_rgba_impl(&buffer, 1, 1);
        assert_eq!(
            &png[0..8],
            &[0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A]
        );
    }

    #[test]
    fn encode_png_has_ihdr_idat_iend() {
        let buffer = vec![0u8; 4 * 4]; // 2x2
        let png = encode_png_rgba_impl(&buffer, 2, 2);
        let png_str = String::from_utf8_lossy(&png);
        // Check chunk types exist (they are ASCII in the binary)
        assert!(png.windows(4).any(|w| w == b"IHDR"));
        assert!(png.windows(4).any(|w| w == b"IDAT"));
        assert!(png.windows(4).any(|w| w == b"IEND"));
        let _ = png_str;
    }

    #[test]
    fn fill_pixel_overflow_safe() {
        // Large y * width would overflow i32; should not panic or corrupt.
        let mut buf = vec![0u8; 16];
        fill_pixel_impl(&mut buf, 0, 70_000, 70_000, [255, 0, 0, 255]);
        assert!(buf.iter().all(|&b| b == 0)); // No writes
    }

    #[test]
    fn fill_pixel_zero_width() {
        let mut buf = vec![0u8; 16];
        fill_pixel_impl(&mut buf, 0, 0, 0, [255, 0, 0, 255]);
        assert!(buf.iter().all(|&b| b == 0));
    }

    #[test]
    fn encode_png_overflow_returns_empty() {
        // (stride + 1) * height overflows usize; should return empty, not panic.
        // width=1073741824 → stride=4294967296 (overflows u32 but not usize on 64-bit),
        // so use width that causes stride.checked_mul(4) to exceed usize on any platform.
        // On 64-bit: width * 4 fits but (stride+1) * height can overflow.
        let result = encode_png_rgba_impl(&[], u32::MAX, u32::MAX);
        // Either returns empty (overflow caught) or doesn't panic.
        // On 64-bit, u32::MAX * 4 = 17179869180 which fits usize but the
        // vec allocation will fail. Our checked_mul catches stride overflow
        // before we try to allocate.
        assert!(result.is_empty());
    }

    #[test]
    fn encode_png_dimensions_in_ihdr() {
        let buffer = vec![0u8; 4 * 6]; // 3x2
        let png = encode_png_rgba_impl(&buffer, 3, 2);
        // IHDR starts after signature(8) + length(4) + "IHDR"(4) = offset 16
        // Width at offset 16, height at offset 20
        let ihdr_start = 16;
        let w = u32::from_be_bytes([
            png[ihdr_start],
            png[ihdr_start + 1],
            png[ihdr_start + 2],
            png[ihdr_start + 3],
        ]);
        let h = u32::from_be_bytes([
            png[ihdr_start + 4],
            png[ihdr_start + 5],
            png[ihdr_start + 6],
            png[ihdr_start + 7],
        ]);
        assert_eq!(w, 3);
        assert_eq!(h, 2);
    }
}
