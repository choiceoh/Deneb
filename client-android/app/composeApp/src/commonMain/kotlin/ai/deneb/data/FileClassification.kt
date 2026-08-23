package ai.deneb.data

// Max files the multi-select attach picker offers in one batch — mirrors the
// gateway's maxBatchFiles so a huge selection can't fan out past what the batch
// capture turn accepts (it caps again server-side).
const val MAX_BATCH_FILES = 20

// Ceiling on the ENCODED size of a picked image. Images bypass the payload cap
// because they are downsampled before upload, but the file still has to be read
// into memory to be decoded, so this bounds that read. Well above real camera
// output (a 200MP JPEG is ~30MB) — it exists to refuse pathological files.
const val MAX_RAW_IMAGE_BYTES = 50_000_000

// Largest total attachment payload one capture batch may carry.
//
// The per-file cap is not enough on its own: captureBatch holds every file's raw
// bytes AND its base64 text AND the assembled JSON body at the same time, so a
// batch peaks at roughly 4-5x its raw size. Twenty 25MB documents therefore ask
// for well over a gigabyte on a ~256MB heap — that is how build 838 died, with
// a 105MB single allocation inside the request serializer. Capping the raw total
// bounds that peak; the gateway independently rejects bodies over 128MiB, so a
// batch the client cannot build was never going to be accepted anyway.
const val MAX_BATCH_BYTES = 40_000_000L

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

/**
 * Whether the composer should stage a picked file with this extension. It is the
 * caller's [supported] document/text/image set PLUS audio: the picker offers audio
 * (for transcription) whenever platform captures are present, so staging must accept
 * it too — else a picked recording is wrongly rejected as an unsupported type.
 */
fun isStageableExtension(extension: String, supported: List<String>): Boolean {
    val ext = extension.lowercase()
    return ext.isNotEmpty() && (ext in supported || ext in audioExtensions)
}

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

// Largest non-image attachment the composer stages: images are downsampled before
// upload, but a document / video / audio file rides as-is (base64), so cap it to keep
// a huge pick from OOMing the device or bloating the turn payload.
const val MAX_ATTACHMENT_BYTES = 25_000_000L

/** Extension for attachment size checks when only filename and/or MIME are known. */
fun attachmentExtension(filename: String, mime: String = ""): String {
    val fromName = filename.substringAfterLast('.', "").lowercase()
    if (fromName.isNotEmpty()) return fromName
    return when {
        mime.startsWith("image/") -> "jpg"
        mime.startsWith("audio/") -> "m4a"
        mime == "application/pdf" -> "pdf"
        else -> "bin"
    }
}

/**
 * Whether a file of this [extension] at [sizeBytes] is within the upload cap.
 *
 * Images get the far looser [MAX_RAW_IMAGE_BYTES] ceiling rather than the payload
 * cap, because they are downsampled before send — what leaves the device is a
 * ~2048px JPEG regardless of what was picked. The ceiling still exists because
 * the encoded file is read into memory whole before that downsample, and it sits
 * far above any real photo (a 200MP JPEG is ~30MB), so it rejects pathological
 * files without rejecting camera output.
 *
 * Non-images require a known positive size — a failed stat/read must not fall
 * through as 0, which would bypass the cap and OOM the device on a multi‑GB pick.
 */
fun isWithinAttachmentSize(extension: String, sizeBytes: Long): Boolean {
    if (extension.lowercase() in imageExtensions) return sizeBytes <= MAX_RAW_IMAGE_BYTES
    if (sizeBytes <= 0) return false
    return sizeBytes <= MAX_ATTACHMENT_BYTES
}

/** Human-readable file size for the attachment chips, e.g. "2.3 MB", "812 KB", "40 B". */
fun formatFileSize(bytes: Long): String = when {
    bytes >= 1_000_000 -> "${(bytes / 100_000) / 10.0} MB"
    bytes >= 1_000 -> "${bytes / 1_000} KB"
    else -> "$bytes B"
}
