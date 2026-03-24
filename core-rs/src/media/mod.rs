//! Media MIME detection and processing helpers.
//!
//! Detects MIME types from file magic bytes — ported from `src/media/`.

/// Detect MIME type from the first bytes of a file (magic byte sniffing).
pub fn detect_mime(data: &[u8]) -> &'static str {
    if data.len() < 4 {
        return "application/octet-stream";
    }

    // Images
    if data.starts_with(&[0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A]) {
        return "image/png";
    }
    if data.starts_with(&[0xFF, 0xD8, 0xFF]) {
        return "image/jpeg";
    }
    if data.starts_with(b"GIF87a") || data.starts_with(b"GIF89a") {
        return "image/gif";
    }
    if data.starts_with(b"RIFF") && data.len() >= 12 && &data[8..12] == b"WEBP" {
        return "image/webp";
    }
    if data.starts_with(&[0x00, 0x00, 0x01, 0x00]) {
        return "image/x-icon";
    }
    if data.starts_with(b"BM") {
        return "image/bmp";
    }

    // Audio/Video
    if data.starts_with(b"ID3") || (data.len() >= 2 && data[0] == 0xFF && (data[1] & 0xE0) == 0xE0)
    {
        return "audio/mpeg";
    }
    if data.starts_with(b"OggS") {
        return "audio/ogg";
    }
    if data.starts_with(b"RIFF") && data.len() >= 12 && &data[8..12] == b"WAVE" {
        return "audio/wav";
    }
    if data.starts_with(b"fLaC") {
        return "audio/flac";
    }
    if data.len() >= 8 && &data[4..8] == b"ftyp" {
        // MP4/M4A container
        if data.len() >= 12 {
            let brand = &data[8..12];
            if brand == b"M4A " || brand == b"M4B " {
                return "audio/mp4";
            }
        }
        return "video/mp4";
    }
    if data.starts_with(&[0x1A, 0x45, 0xDF, 0xA3]) {
        return "video/webm";
    }

    // Documents
    if data.starts_with(b"%PDF") {
        return "application/pdf";
    }
    if data.starts_with(&[0x50, 0x4B, 0x03, 0x04]) {
        return "application/zip";
    }
    if data.starts_with(&[0x1F, 0x8B]) {
        return "application/gzip";
    }

    // Text-based (check for common text patterns)
    if data.starts_with(b"{") || data.starts_with(b"[") {
        return "application/json";
    }
    if data.starts_with(b"<?xml") || data.starts_with(b"<svg") {
        return "application/xml";
    }
    if data.starts_with(b"<!DOCTYPE") || data.starts_with(b"<html") || data.starts_with(b"<HTML")
    {
        return "text/html";
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
