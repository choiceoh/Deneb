package ai.deneb.data

enum class FileCategory {
    IMAGE,
    TEXT,
    PDF,
    UNSUPPORTED,
}

const val MAX_TEXT_FILE_BYTES = 200_000
const val MAX_PDF_BYTES = 20_000_000
const val MAX_IMAGE_BYTES = 15_000_000

// Raw image input cap before compression — images typically shrink after compression,
// so we allow larger raw files than MAX_IMAGE_BYTES while still preventing an OOM
// from reading a multi-gigabyte file into memory.
const val MAX_RAW_IMAGE_BYTES = 50_000_000

private val textMimeTypes = setOf(
    "application/json",
    "application/xml",
    "application/javascript",
    "application/x-yaml",
    "application/yaml",
    "application/x-sh",
    "application/sql",
    "application/graphql",
    "application/toml",
)

private val textExtensions = setOf(
    "txt", "md", "json", "csv", "xml", "yaml", "yml",
    "html", "css", "js", "ts", "kt", "kts", "java",
    "py", "rb", "rs", "go", "c", "h", "cpp", "hpp",
    "swift", "sh", "bash", "zsh", "sql", "graphql",
    "toml", "ini", "cfg", "conf", "log", "properties",
    "gradle", "tsx", "jsx", "gsc",
)

internal val imageExtensions = setOf(
    "jpg",
    "jpeg",
    "png",
    "gif",
    "webp",
    "bmp",
    "svg",
    "heic",
    "heif",
)

// Audio file extensions the attach picker auto-routes to transcription
// (VibeVoice-ASR). Kept beside imageExtensions so the chat input can classify a
// picked file by type without a per-type menu.
internal val audioExtensions = setOf(
    "m4a",
    "mp3",
    "wav",
    "ogg",
    "oga",
    "opus",
    "aac",
    "flac",
    "amr",
    "3gp",
)

val supportedFileExtensions = (imageExtensions + textExtensions).toList()

/** How the chat input routes a file picked from the single attach (+) picker. */
enum class AttachmentRoute {
    IMAGE_CAPTURE, // image -> gateway OCR
    AUDIO_CAPTURE, // audio -> gateway transcription
    FILE_ATTACH, // anything else -> attach for the next message
}

/**
 * Classifies a picked file by extension so the attach (+) button needs no
 * "what to insert" menu. Image/audio routes only apply when platform captures are
 * available (Android); without them every file is attached. Pure + testable; the
 * Compose wiring in QuestionInput just dispatches on the result.
 */
fun routeAttachment(extension: String, capturesAvailable: Boolean): AttachmentRoute {
    if (!capturesAvailable) return AttachmentRoute.FILE_ATTACH
    return when (extension.lowercase()) {
        in imageExtensions -> AttachmentRoute.IMAGE_CAPTURE
        in audioExtensions -> AttachmentRoute.AUDIO_CAPTURE
        else -> AttachmentRoute.FILE_ATTACH
    }
}

fun classifyFile(mimeType: String?, fileName: String?): FileCategory {
    if (mimeType != null) {
        if (mimeType.startsWith("image/")) return FileCategory.IMAGE
        if (mimeType == "application/pdf") return FileCategory.PDF
        if (mimeType.startsWith("text/") || mimeType in textMimeTypes) return FileCategory.TEXT
    }
    // Fall back to extension
    val ext = fileName?.substringAfterLast('.', "")?.lowercase()
    if (ext != null && ext in imageExtensions) return FileCategory.IMAGE
    if (ext != null && ext in textExtensions) return FileCategory.TEXT
    if (ext == "pdf") return FileCategory.PDF

    // If mimeType is null and no recognized extension, unsupported
    if (mimeType == null) return FileCategory.UNSUPPORTED

    return FileCategory.UNSUPPORTED
}
