/// JPEG EXIF orientation reader.
///
/// Ports `readJpegExifOrientation` from `src/media/image-ops.ts` (lines 47-134).
/// Pure binary buffer parsing — ideal Rust target (no GC, SIMD-friendly).

#[cfg(feature = "napi_binding")]
use napi::bindgen_prelude::Buffer;

// ---------------------------------------------------------------------------
// Implementation
// ---------------------------------------------------------------------------

/// Reads EXIF orientation from a JPEG buffer.
/// Returns orientation value 1-8, or None if not found / not JPEG.
///
/// EXIF orientation values:
/// 1 = Normal, 2 = Flip H, 3 = Rotate 180, 4 = Flip V,
/// 5 = Rotate 270 CW + Flip H, 6 = Rotate 90 CW, 7 = Rotate 90 CW + Flip H, 8 = Rotate 270 CW
pub fn read_jpeg_exif_orientation_impl(buf: &[u8]) -> Option<u8> {
    // Check JPEG magic bytes (SOI marker)
    if buf.len() < 2 || buf[0] != 0xFF || buf[1] != 0xD8 {
        return None;
    }

    let mut offset: usize = 2;
    while offset < buf.len().saturating_sub(4) {
        // Look for marker
        if buf[offset] != 0xFF {
            offset += 1;
            continue;
        }

        let marker = buf[offset + 1];
        // Skip padding FF bytes
        if marker == 0xFF {
            offset += 1;
            continue;
        }

        // APP1 marker (EXIF)
        if marker == 0xE1 {
            let exif_start = offset + 4;

            // Check for "Exif\0\0" header
            if buf.len() > exif_start + 6
                && &buf[exif_start..exif_start + 4] == b"Exif"
                && buf[exif_start + 4] == 0
                && buf[exif_start + 5] == 0
            {
                let tiff_start = exif_start + 6;
                if buf.len() < tiff_start + 8 {
                    return None;
                }

                // Check byte order (II = little-endian, MM = big-endian)
                let is_little_endian = &buf[tiff_start..tiff_start + 2] == b"II";

                let read_u16 = |pos: usize| -> Option<u16> {
                    if pos + 2 > buf.len() {
                        return None;
                    }
                    Some(if is_little_endian {
                        u16::from_le_bytes([buf[pos], buf[pos + 1]])
                    } else {
                        u16::from_be_bytes([buf[pos], buf[pos + 1]])
                    })
                };

                let read_u32 = |pos: usize| -> Option<u32> {
                    if pos + 4 > buf.len() {
                        return None;
                    }
                    Some(if is_little_endian {
                        u32::from_le_bytes([buf[pos], buf[pos + 1], buf[pos + 2], buf[pos + 3]])
                    } else {
                        u32::from_be_bytes([buf[pos], buf[pos + 1], buf[pos + 2], buf[pos + 3]])
                    })
                };

                // Read IFD0 offset
                let ifd0_offset = read_u32(tiff_start + 4)? as usize;
                let ifd0_start = tiff_start + ifd0_offset;
                if buf.len() < ifd0_start + 2 {
                    return None;
                }

                let num_entries = read_u16(ifd0_start)? as usize;
                for i in 0..num_entries {
                    let entry_offset = ifd0_start + 2 + i * 12;
                    if buf.len() < entry_offset + 12 {
                        break;
                    }

                    let tag = read_u16(entry_offset)?;
                    // Orientation tag = 0x0112
                    if tag == 0x0112 {
                        let value = read_u16(entry_offset + 8)?;
                        return if (1..=8).contains(&value) {
                            Some(value as u8)
                        } else {
                            None
                        };
                    }
                }
            }
            return None;
        }

        // Skip other APP segments (0xE0-0xEF)
        if marker >= 0xE0 && marker <= 0xEF {
            if offset + 3 >= buf.len() {
                break;
            }
            let segment_length = u16::from_be_bytes([buf[offset + 2], buf[offset + 3]]) as usize;
            offset += 2 + segment_length;
            continue;
        }

        // SOF or SOS — stop searching
        if marker == 0xC0 || marker == 0xDA {
            break;
        }

        offset += 1;
    }

    None
}

// ---------------------------------------------------------------------------
// napi export
// ---------------------------------------------------------------------------

/// Read JPEG EXIF orientation from a buffer (napi entrypoint).
/// Returns orientation (1-8) or null if not found.
#[cfg(feature = "napi_binding")]
#[cfg_attr(feature = "napi_binding", napi)]
pub fn read_jpeg_exif_orientation(buffer: Buffer) -> Option<u32> {
    read_jpeg_exif_orientation_impl(&buffer).map(|v| v as u32)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn returns_none_for_empty() {
        assert_eq!(read_jpeg_exif_orientation_impl(&[]), None);
    }

    #[test]
    fn returns_none_for_non_jpeg() {
        // PNG magic
        assert_eq!(
            read_jpeg_exif_orientation_impl(&[0x89, 0x50, 0x4E, 0x47]),
            None
        );
    }

    #[test]
    fn returns_none_for_jpeg_without_exif() {
        // Minimal JPEG: SOI + SOS (no EXIF)
        let buf = vec![0xFF, 0xD8, 0xFF, 0xDA, 0x00, 0x02];
        assert_eq!(read_jpeg_exif_orientation_impl(&buf), None);
    }

    #[test]
    fn reads_orientation_big_endian() {
        // Construct a minimal JPEG with EXIF containing orientation=6 (big-endian)
        let mut buf = Vec::new();
        // SOI
        buf.extend_from_slice(&[0xFF, 0xD8]);
        // APP1 marker
        buf.extend_from_slice(&[0xFF, 0xE1]);
        // APP1 length (will be filled)
        let length_pos = buf.len();
        buf.extend_from_slice(&[0x00, 0x00]); // placeholder
                                              // "Exif\0\0"
        buf.extend_from_slice(b"Exif\0\0");
        let tiff_start = buf.len();
        // TIFF header (big-endian)
        buf.extend_from_slice(b"MM");
        buf.extend_from_slice(&[0x00, 0x2A]); // magic
        buf.extend_from_slice(&[0x00, 0x00, 0x00, 0x08]); // IFD0 offset = 8
                                                          // IFD0
        buf.extend_from_slice(&[0x00, 0x01]); // 1 entry
                                              // Entry: orientation tag
        buf.extend_from_slice(&[0x01, 0x12]); // tag 0x0112
        buf.extend_from_slice(&[0x00, 0x03]); // type SHORT
        buf.extend_from_slice(&[0x00, 0x00, 0x00, 0x01]); // count 1
        buf.extend_from_slice(&[0x00, 0x06, 0x00, 0x00]); // value 6

        // Fill APP1 length
        let app1_len = (buf.len() - length_pos) as u16;
        buf[length_pos] = (app1_len >> 8) as u8;
        buf[length_pos + 1] = (app1_len & 0xFF) as u8;

        let _ = tiff_start; // used in construction

        assert_eq!(read_jpeg_exif_orientation_impl(&buf), Some(6));
    }

    #[test]
    fn reads_orientation_little_endian() {
        let mut buf = Vec::new();
        buf.extend_from_slice(&[0xFF, 0xD8]);
        buf.extend_from_slice(&[0xFF, 0xE1]);
        let length_pos = buf.len();
        buf.extend_from_slice(&[0x00, 0x00]);
        buf.extend_from_slice(b"Exif\0\0");
        // TIFF header (little-endian)
        buf.extend_from_slice(b"II");
        buf.extend_from_slice(&[0x2A, 0x00]); // magic LE
        buf.extend_from_slice(&[0x08, 0x00, 0x00, 0x00]); // IFD0 offset = 8
                                                          // IFD0
        buf.extend_from_slice(&[0x01, 0x00]); // 1 entry (LE)
                                              // Entry: orientation tag
        buf.extend_from_slice(&[0x12, 0x01]); // tag 0x0112 (LE)
        buf.extend_from_slice(&[0x03, 0x00]); // type SHORT (LE)
        buf.extend_from_slice(&[0x01, 0x00, 0x00, 0x00]); // count 1 (LE)
        buf.extend_from_slice(&[0x03, 0x00, 0x00, 0x00]); // value 3 (LE)

        let app1_len = (buf.len() - length_pos) as u16;
        buf[length_pos] = (app1_len >> 8) as u8;
        buf[length_pos + 1] = (app1_len & 0xFF) as u8;

        assert_eq!(read_jpeg_exif_orientation_impl(&buf), Some(3));
    }

    #[test]
    fn rejects_out_of_range_orientation() {
        let mut buf = Vec::new();
        buf.extend_from_slice(&[0xFF, 0xD8]);
        buf.extend_from_slice(&[0xFF, 0xE1]);
        let length_pos = buf.len();
        buf.extend_from_slice(&[0x00, 0x00]);
        buf.extend_from_slice(b"Exif\0\0");
        buf.extend_from_slice(b"MM");
        buf.extend_from_slice(&[0x00, 0x2A]);
        buf.extend_from_slice(&[0x00, 0x00, 0x00, 0x08]);
        buf.extend_from_slice(&[0x00, 0x01]);
        buf.extend_from_slice(&[0x01, 0x12]);
        buf.extend_from_slice(&[0x00, 0x03]);
        buf.extend_from_slice(&[0x00, 0x00, 0x00, 0x01]);
        buf.extend_from_slice(&[0x00, 0x09, 0x00, 0x00]); // value 9 — out of range

        let app1_len = (buf.len() - length_pos) as u16;
        buf[length_pos] = (app1_len >> 8) as u8;
        buf[length_pos + 1] = (app1_len & 0xFF) as u8;

        assert_eq!(read_jpeg_exif_orientation_impl(&buf), None);
    }
}
