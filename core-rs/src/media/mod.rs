//! Media MIME detection and processing helpers.
//!
//! Detects MIME types from file magic bytes — ported from `src/media/`.

pub mod extensions;

/// Detect MIME type from the first bytes of a file (magic byte sniffing).
/// Uses first-byte dispatch to minimize comparisons.
pub fn detect_mime(data: &[u8]) -> &'static str {
    if data.len() < 4 {
        return "application/octet-stream";
    }

    // Dispatch on first byte to skip irrelevant branches.
    match data[0] {
        0x89 => {
            if data.starts_with(&[0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A]) {
                return "image/png";
            }
        }
        0xFF => {
            if data[1] == 0xD8 && data[2] == 0xFF {
                return "image/jpeg";
            }
            // MP3 sync word: 0xFF 0xE0..0xFF
            if (data[1] & 0xE0) == 0xE0 {
                return "audio/mpeg";
            }
        }
        b'G' => {
            if data.starts_with(b"GIF87a") || data.starts_with(b"GIF89a") {
                return "image/gif";
            }
        }
        b'R' => {
            if data.starts_with(b"RIFF") && data.len() >= 12 {
                if &data[8..12] == b"WEBP" {
                    return "image/webp";
                }
                if &data[8..12] == b"WAVE" {
                    return "audio/wav";
                }
            }
        }
        0x00 => {
            if data.starts_with(&[0x00, 0x00, 0x01, 0x00]) {
                return "image/x-icon";
            }
            // MP4/M4A: ftyp at offset 4
            if data.len() >= 8 && &data[4..8] == b"ftyp" {
                if data.len() >= 12 {
                    let brand = &data[8..12];
                    if brand == b"M4A " || brand == b"M4B " {
                        return "audio/mp4";
                    }
                }
                return "video/mp4";
            }
        }
        b'B' => {
            if data.starts_with(b"BM") {
                return "image/bmp";
            }
        }
        b'I' => {
            if data.starts_with(b"ID3") {
                return "audio/mpeg";
            }
        }
        b'O' => {
            if data.starts_with(b"OggS") {
                return "audio/ogg";
            }
        }
        b'f' => {
            if data.starts_with(b"fLaC") {
                return "audio/flac";
            }
            // ftyp with non-zero box size
            if data.len() >= 8 && &data[4..8] == b"ftyp" {
                return "video/mp4";
            }
        }
        0x1A => {
            if data.starts_with(&[0x1A, 0x45, 0xDF, 0xA3]) {
                return "video/webm";
            }
        }
        b'%' => {
            if data.starts_with(b"%PDF") {
                return "application/pdf";
            }
        }
        0x50 => {
            if data.starts_with(&[0x50, 0x4B, 0x03, 0x04]) {
                return "application/zip";
            }
        }
        0x1F => {
            if data[1] == 0x8B {
                return "application/gzip";
            }
        }
        b'{' | b'[' => return "application/json",
        b'<' => {
            if data.starts_with(b"<?xml") || data.starts_with(b"<svg") {
                return "application/xml";
            }
            if data.starts_with(b"<!DOCTYPE") || data.starts_with(b"<html") || data.starts_with(b"<HTML") {
                return "text/html";
            }
        }
        _ => {}
    }

    // Fallback: check ftyp at offset 4 for non-zero first byte MP4 variants.
    if data.len() >= 8 && &data[4..8] == b"ftyp" {
        if data.len() >= 12 {
            let brand = &data[8..12];
            if brand == b"M4A " || brand == b"M4B " {
                return "audio/mp4";
            }
        }
        return "video/mp4";
    }

    "application/octet-stream"
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_png() {
        let data = [0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00];
        assert_eq!(detect_mime(&data), "image/png");
    }

    #[test]
    fn test_jpeg() {
        let data = [0xFF, 0xD8, 0xFF, 0xE0, 0x00];
        assert_eq!(detect_mime(&data), "image/jpeg");
    }

    #[test]
    fn test_gif() {
        assert_eq!(detect_mime(b"GIF89a..."), "image/gif");
        assert_eq!(detect_mime(b"GIF87a..."), "image/gif");
    }

    #[test]
    fn test_webp() {
        let mut data = Vec::from(b"RIFF" as &[u8]);
        data.extend_from_slice(&[0x00; 4]); // size
        data.extend_from_slice(b"WEBP");
        assert_eq!(detect_mime(&data), "image/webp");
    }

    #[test]
    fn test_pdf() {
        assert_eq!(detect_mime(b"%PDF-1.4"), "application/pdf");
    }

    #[test]
    fn test_mp4() {
        let data = [0x00, 0x00, 0x00, 0x1C, b'f', b't', b'y', b'p', b'i', b's', b'o', b'm'];
        assert_eq!(detect_mime(&data), "video/mp4");
    }

    #[test]
    fn test_json() {
        assert_eq!(detect_mime(b"{\"key\":\"value\"}"), "application/json");
    }

    #[test]
    fn test_unknown() {
        assert_eq!(detect_mime(&[0x00, 0x01, 0x02, 0x03]), "application/octet-stream");
    }

    #[test]
    fn test_too_short() {
        assert_eq!(detect_mime(&[0x00]), "application/octet-stream");
    }
}
