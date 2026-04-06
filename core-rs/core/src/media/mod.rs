//! Media MIME detection and processing helpers.
//!
//! Detects MIME types from file magic bytes — ported from `src/media/`.

/// Detect OOXML document type by scanning ZIP contents for known path markers.
/// Scans the first 8KB of the ZIP file for internal path signatures.
fn detect_ooxml(data: &[u8]) -> Option<&'static str> {
    // Scan a limited window to avoid reading the entire file
    let scan_len = data.len().min(8192);
    let window = &data[..scan_len];

    // Look for OOXML internal path markers in the ZIP local file headers.
    // Use specific filenames to avoid false positives from short prefixes.
    if contains_bytes(window, b"xl/workbook.xml")
        || contains_bytes(window, b"xl/sharedStrings.xml")
        || contains_bytes(window, b"xl/styles.xml")
    {
        return Some("application/vnd.openxmlformats-officedocument.spreadsheetml.sheet");
    }
    if contains_bytes(window, b"word/document.xml")
        || contains_bytes(window, b"word/styles.xml")
        || contains_bytes(window, b"word/settings.xml")
    {
        return Some("application/vnd.openxmlformats-officedocument.wordprocessingml.document");
    }
    if contains_bytes(window, b"ppt/presentation.xml")
        || contains_bytes(window, b"ppt/slides/")
        || contains_bytes(window, b"ppt/slideMasters/")
    {
        return Some("application/vnd.openxmlformats-officedocument.presentationml.presentation");
    }
    None
}

/// Simple byte substring search (no allocation).
#[inline]
fn contains_bytes(haystack: &[u8], needle: &[u8]) -> bool {
    haystack.windows(needle.len()).any(|w| w == needle)
}

/// Detect ISO Base Media File Format (ISOBMFF) variants from the `ftyp` box at offset 4.
/// The 4-byte brand at offset 8 distinguishes MP4, M4A, AVIF, HEIC/HEIF.
#[inline]
fn detect_ftyp(data: &[u8]) -> Option<&'static str> {
    if data.len() >= 8 && &data[4..8] == b"ftyp" {
        if data.len() >= 12 {
            let brand = &data[8..12];
            // Audio containers
            if brand == b"M4A " || brand == b"M4B " {
                return Some("audio/mp4");
            }
            // AVIF image
            if brand == b"avif" || brand == b"avis" {
                return Some("image/avif");
            }
            // HEIC/HEIF image
            if brand == b"heic" || brand == b"heix" || brand == b"hevc" || brand == b"mif1" {
                return Some("image/heic");
            }
        }
        return Some("video/mp4");
    }
    None
}

/// Detect MIME type from the first bytes of a file (magic byte sniffing).
/// Uses first-byte dispatch to minimize comparisons across 21+ formats.
/// Falls back to "application/octet-stream" for unrecognized data.
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
            // MP3 frame sync: 12-bit sync word 0xFFF or 0xFFE. The top 3 bits
            // of byte[1] must be set (0xE0 mask), covering MPEG1/2/2.5 layers.
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
            if let Some(mime) = detect_ftyp(data) {
                return mime;
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
            // TIFF little-endian: II\x2A\x00
            if data.len() >= 4 && data[1] == b'I' && data[2] == 0x2A && data[3] == 0x00 {
                return "image/tiff";
            }
        }
        b'M' => {
            // TIFF big-endian: MM\x00\x2A
            if data.len() >= 4 && data[1] == b'M' && data[2] == 0x00 && data[3] == 0x2A {
                return "image/tiff";
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
            if let Some(mime) = detect_ftyp(data) {
                return mime;
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
                // ZIP file — check for OOXML markers inside the archive
                if let Some(ooxml) = detect_ooxml(data) {
                    return ooxml;
                }
                return "application/zip";
            }
        }
        0x1F => {
            if data[1] == 0x8B {
                return "application/gzip";
            }
        }
        // Best-effort JSON heuristic: any data starting with { or [ is assumed JSON.
        // Not true validation — plain text starting with a bracket would match.
        b'{' | b'[' => return "application/json",
        b'<' => {
            if data.starts_with(b"<?xml") || data.starts_with(b"<svg") {
                return "application/xml";
            }
            if data.starts_with(b"<!DOCTYPE")
                || data.starts_with(b"<html")
                || data.starts_with(b"<HTML")
            {
                return "text/html";
            }
        }
        _ => {}
    }

    // Fallback: check ftyp at offset 4 for non-zero first byte MP4 variants.
    if let Some(mime) = detect_ftyp(data) {
        return mime;
    }

    "application/octet-stream"
}
