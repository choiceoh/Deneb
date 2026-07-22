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

// Max files the multi-select attach picker offers in one batch — mirrors the
// gateway's maxBatchFiles so a huge selection can't fan out past what the batch
// capture turn accepts (it caps again server-side).
const val MAX_BATCH_FILES = 20

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

// Rich document formats the gateway extracts natively (OOXML/PDF) or converts on
// the host (hwp5txt for HWP, LibreOffice for legacy Office / ODF). Listed so the
// in-app attach picker offers them — text/image/pdf were the only pickable types
// before; Office/HWP arrived only via the share sheet.
internal val documentExtensions = setOf(
    "docx", "xlsx", "pptx",
    "doc", "xls", "ppt",
    "rtf", "odt", "ods", "odp",
    "hwp", "hwpx",
)

val supportedFileExtensions = (imageExtensions + textExtensions + documentExtensions).toList()

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

/**
 * MIME to send with a picked document on the capture (`miniapp.capture.document`)
 * path so the gateway's extractDocument dispatch routes it correctly. The gateway
 * is filename-first, but a `text/plain` hint short-circuits its plain-text branch
 * BEFORE the converter fallback — so a binary format it converts on the host (HWP
 * via hwp5txt, legacy Office / ODF via LibreOffice) would come back as its raw
 * bytes read as garbage text. So:
 *   - PDF announces application/pdf (its own gateway branch);
 *   - genuine text / code files keep text/plain — the gateway's isTextFile list is
 *     narrower than our [textExtensions], so those NEED the hint to be read as text;
 *   - every other document (OOXML + the converter formats) sends no MIME and lets
 *     the gateway route by filename — OOXML by suffix, HWP/legacy/ODF via the
 *     converter default case.
 * Pure + testable; MainActivity's picker path just calls it.
 */
fun documentCaptureMime(fileName: String): String {
    val ext = fileName.substringAfterLast('.', "").lowercase()
    return when {
        ext == "pdf" -> "application/pdf"
        ext in textExtensions -> "text/plain"
        else -> ""
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
