use flate2::Compression;
use napi::bindgen_prelude::*;
use napi_derive::napi;
use std::io::Write;

/// Compute CRC32 checksum for a buffer (PNG-compatible).
#[napi]
pub fn crc32(buffer: Buffer) -> u32 {
    crc32_pure(&buffer)
}

/// Encode an RGBA pixel buffer as a PNG image.
#[napi]
pub fn encode_png_rgba(buffer: Buffer, width: u32, height: u32) -> Result<Buffer> {
    encode_png_rgba_pure(&buffer, width, height)
        .map(Buffer::from)
        .map_err(|e| Error::from_reason(e))
}

// --- Pure Rust functions (testable without napi linking) ---

fn crc32_pure(data: &[u8]) -> u32 {
    crc32fast::hash(data)
}

fn encode_png_rgba_pure(data: &[u8], width: u32, height: u32) -> std::result::Result<Vec<u8>, String> {
    let expected_len = (width as usize) * (height as usize) * 4;

    if data.len() != expected_len {
        return Err(format!(
            "Buffer length {} does not match {}x{}x4 = {}",
            data.len(), width, height, expected_len
        ));
    }

    let stride = (width as usize) * 4;

    // Prepare filtered scanlines: filter byte (0 = None) + row data.
    let mut raw = Vec::with_capacity((stride + 1) * (height as usize));
    for row in 0..height as usize {
        raw.push(0u8); // filter: None
        raw.extend_from_slice(&data[row * stride..(row + 1) * stride]);
    }

    // PNG IDAT uses zlib-wrapped deflate.
    let mut encoder = flate2::write::ZlibEncoder::new(Vec::new(), Compression::default());
    encoder.write_all(&raw).map_err(|e| format!("Zlib error: {e}"))?;
    let compressed = encoder.finish().map_err(|e| format!("Zlib finish error: {e}"))?;

    // Build PNG file.
    let mut out = Vec::with_capacity(8 + 25 + compressed.len() + 12 + 12);

    // PNG signature.
    out.extend_from_slice(&[0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A]);

    // IHDR chunk.
    let mut ihdr_data = Vec::with_capacity(13);
    ihdr_data.extend_from_slice(&width.to_be_bytes());
    ihdr_data.extend_from_slice(&height.to_be_bytes());
    ihdr_data.push(8); // bit depth
    ihdr_data.push(6); // color type: RGBA
    ihdr_data.push(0); // compression
    ihdr_data.push(0); // filter
    ihdr_data.push(0); // interlace
    write_png_chunk(&mut out, b"IHDR", &ihdr_data);

    // IDAT chunk.
    write_png_chunk(&mut out, b"IDAT", &compressed);

    // IEND chunk.
    write_png_chunk(&mut out, b"IEND", &[]);

    Ok(out)
}

fn write_png_chunk(out: &mut Vec<u8>, chunk_type: &[u8; 4], data: &[u8]) {
    out.extend_from_slice(&(data.len() as u32).to_be_bytes());
    out.extend_from_slice(chunk_type);
    out.extend_from_slice(data);

    let mut crc_input = Vec::with_capacity(4 + data.len());
    crc_input.extend_from_slice(chunk_type);
    crc_input.extend_from_slice(data);
    let crc = crc32fast::hash(&crc_input);
    out.extend_from_slice(&crc.to_be_bytes());
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_crc32_known_value() {
        assert_eq!(crc32_pure(b"IEND"), 0xAE42_6082);
    }

    #[test]
    fn test_crc32_empty() {
        assert_eq!(crc32_pure(&[]), 0);
    }

    #[test]
    fn test_encode_png_rgba_1x1() {
        let pixel = vec![255u8, 0, 0, 255];
        let png = encode_png_rgba_pure(&pixel, 1, 1).unwrap();

        // Check PNG signature.
        assert_eq!(&png[0..8], &[0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A]);
        // Check IHDR chunk type.
        assert_eq!(&png[12..16], b"IHDR");
    }

    #[test]
    fn test_encode_png_rgba_invalid_size() {
        assert!(encode_png_rgba_pure(&[255, 0, 0], 1, 1).is_err());
    }

    #[test]
    fn test_encode_png_rgba_2x2() {
        let pixels = vec![0u8; 16];
        let png = encode_png_rgba_pure(&pixels, 2, 2).unwrap();
        assert_eq!(&png[0..8], &[0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A]);
    }
}
