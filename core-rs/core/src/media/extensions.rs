//! MIME type to file extension mapping.
//!
//! Provides bidirectional mapping between MIME types and their common
//! file extensions, plus type classification helpers.

/// A MIME detection result with extension info.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MimeInfo {
    pub mime: &'static str,
    pub extension: &'static str,
    pub category: MediaCategory,
}

/// High-level media category.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MediaCategory {
    Image,
    Audio,
    Video,
    Document,
    Archive,
    Text,
    Unknown,
}

/// MIME-to-extension mapping table.
const MIME_MAP: &[(&str, &str, MediaCategory)] = &[
    // Images
    ("image/png", "png", MediaCategory::Image),
    ("image/jpeg", "jpg", MediaCategory::Image),
    ("image/gif", "gif", MediaCategory::Image),
    ("image/webp", "webp", MediaCategory::Image),
    ("image/x-icon", "ico", MediaCategory::Image),
    ("image/bmp", "bmp", MediaCategory::Image),
    ("image/svg+xml", "svg", MediaCategory::Image),
    ("image/tiff", "tiff", MediaCategory::Image),
    ("image/heic", "heic", MediaCategory::Image),
    ("image/heif", "heif", MediaCategory::Image),
    ("image/avif", "avif", MediaCategory::Image),
    // Audio
    ("audio/mpeg", "mp3", MediaCategory::Audio),
    ("audio/ogg", "ogg", MediaCategory::Audio),
    ("audio/wav", "wav", MediaCategory::Audio),
    ("audio/flac", "flac", MediaCategory::Audio),
    ("audio/mp4", "m4a", MediaCategory::Audio),
    ("audio/aac", "aac", MediaCategory::Audio),
    ("audio/webm", "weba", MediaCategory::Audio),
    ("audio/opus", "opus", MediaCategory::Audio),
    // Video
    ("video/mp4", "mp4", MediaCategory::Video),
    ("video/webm", "webm", MediaCategory::Video),
    ("video/ogg", "ogv", MediaCategory::Video),
    ("video/quicktime", "mov", MediaCategory::Video),
    ("video/x-msvideo", "avi", MediaCategory::Video),
    // Documents
    ("application/pdf", "pdf", MediaCategory::Document),
    ("application/msword", "doc", MediaCategory::Document),
    (
        "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
        "docx",
        MediaCategory::Document,
    ),
    (
        "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
        "xlsx",
        MediaCategory::Document,
    ),
    (
        "application/vnd.openxmlformats-officedocument.presentationml.presentation",
        "pptx",
        MediaCategory::Document,
    ),
    ("application/vnd.ms-excel", "xls", MediaCategory::Document),
    (
        "application/vnd.ms-powerpoint",
        "ppt",
        MediaCategory::Document,
    ),
    // Archives
    ("application/zip", "zip", MediaCategory::Archive),
    ("application/gzip", "gz", MediaCategory::Archive),
    ("application/x-tar", "tar", MediaCategory::Archive),
    ("application/x-7z-compressed", "7z", MediaCategory::Archive),
    ("application/vnd.rar", "rar", MediaCategory::Archive),
    // Text
    ("application/json", "json", MediaCategory::Text),
    ("application/xml", "xml", MediaCategory::Text),
    ("text/html", "html", MediaCategory::Text),
    ("text/plain", "txt", MediaCategory::Text),
    ("text/css", "css", MediaCategory::Text),
    ("text/javascript", "js", MediaCategory::Text),
    ("text/markdown", "md", MediaCategory::Text),
    ("text/csv", "csv", MediaCategory::Text),
    // Fallback
    ("application/octet-stream", "bin", MediaCategory::Unknown),
];

/// Get the file extension for a MIME type.
pub fn extension_for_mime(mime: &str) -> &'static str {
    lookup_mime(mime).map_or("bin", |(ext, _)| ext)
}

/// Get the media category for a MIME type.
pub fn category_for_mime(mime: &str) -> MediaCategory {
    lookup_mime(mime).map_or(MediaCategory::Unknown, |(_, cat)| cat)
}

/// Look up MIME info from the mapping table in a single pass.
fn lookup_mime(mime: &str) -> Option<(&'static str, MediaCategory)> {
    MIME_MAP
        .iter()
        .find(|(m, _, _)| *m == mime)
        .map(|(_, ext, cat)| (*ext, *cat))
}

/// Detect MIME type from magic bytes and return full info.
/// Single lookup: detects MIME then resolves extension+category in one pass.
pub fn detect_mime_with_info(data: &[u8]) -> MimeInfo {
    let mime = super::detect_mime(data);
    let (extension, category) = lookup_mime(mime).unwrap_or(("bin", MediaCategory::Unknown));
    MimeInfo {
        mime,
        extension,
        category,
    }
}

/// Check if a MIME type is a supported image format.
pub fn is_image(mime: &str) -> bool {
    category_for_mime(mime) == MediaCategory::Image
}

/// Check if a MIME type is an audio format.
pub fn is_audio(mime: &str) -> bool {
    category_for_mime(mime) == MediaCategory::Audio
}

/// Check if a MIME type is a video format.
pub fn is_video(mime: &str) -> bool {
    category_for_mime(mime) == MediaCategory::Video
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_extension_for_known_mimes() {
        assert_eq!(extension_for_mime("image/png"), "png");
        assert_eq!(extension_for_mime("image/jpeg"), "jpg");
        assert_eq!(extension_for_mime("application/pdf"), "pdf");
        assert_eq!(extension_for_mime("video/mp4"), "mp4");
        assert_eq!(extension_for_mime("audio/mpeg"), "mp3");
    }

    #[test]
    fn test_extension_for_unknown() {
        assert_eq!(extension_for_mime("application/x-custom"), "bin");
    }

    #[test]
    fn test_category() {
        assert_eq!(category_for_mime("image/png"), MediaCategory::Image);
        assert_eq!(category_for_mime("audio/flac"), MediaCategory::Audio);
        assert_eq!(category_for_mime("video/webm"), MediaCategory::Video);
        assert_eq!(
            category_for_mime("application/pdf"),
            MediaCategory::Document
        );
        assert_eq!(category_for_mime("application/zip"), MediaCategory::Archive);
        assert_eq!(category_for_mime("application/json"), MediaCategory::Text);
    }

    #[test]
    fn test_classify_helpers() {
        assert!(is_image("image/png"));
        assert!(!is_image("audio/mp3"));
        assert!(is_audio("audio/mpeg"));
        assert!(!is_audio("video/mp4"));
        assert!(is_video("video/mp4"));
        assert!(!is_video("image/png"));
    }

    #[test]
    fn test_detect_with_info() {
        let png = [0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00];
        let info = detect_mime_with_info(&png);
        assert_eq!(info.mime, "image/png");
        assert_eq!(info.extension, "png");
        assert_eq!(info.category, MediaCategory::Image);
    }

    #[test]
    fn test_all_entries_have_unique_mimes() {
        let mut seen = std::collections::HashSet::new();
        for (mime, _, _) in MIME_MAP {
            assert!(seen.insert(*mime), "duplicate MIME: {mime}");
        }
    }
}
