package ai.deneb.ui.chat.composables

import androidx.compose.runtime.compositionLocalOf
import io.github.vinceglb.filekit.PlatformFile

// Capture actions for the chat input. The left navigation drawer that used to host
// them (a typographic menu) was retired when the bottom tab bar took over section
// navigation and the left drawer became the session history.
//
// The attach (+) button no longer asks "what to insert" — it opens one file picker
// and the chat input routes by file type: an image goes to [onImageFile] (OCR), an
// audio file to [onAudioFile] (transcription), and any other file (pdf/doc/sheet/
// text) to [onDocumentFile] (extract + analyze). The live mic ([onVoiceInput],
// system speech recognizer) is not a file, so it is the chat input's trailing mic
// button (shown when the field is empty) rather than crowding the attach picker.

/**
 * Platform capture actions. Provided by the Android entry point via
 * [LocalCaptureActions]; null (the default) on platforms (desktop/iOS) without
 * these system launchers — there the attach picker simply attaches the file.
 *
 * [onImageFile]/[onAudioFile]/[onDocumentFile] receive a single already-picked file
 * (routed by type) and [onFilesBatch] receives several picked at once (one batch turn
 * the agent reads and cross-references). All carry the text the user had typed in the
 * composer, which rides along as the capture's caption so the turn analyzes the files
 * in the light of that question; the entry point reads the bytes and runs the gateway
 * capture turn. The caption is blank when the composer was empty.
 */
data class CaptureActions(
    val onImageFile: (PlatformFile, String) -> Unit,
    val onAudioFile: (PlatformFile, String) -> Unit,
    val onDocumentFile: (PlatformFile, String) -> Unit,
    val onFilesBatch: (List<PlatformFile>, String) -> Unit,
    val onVoiceInput: () -> Unit,
)

/** Ambient capture actions; null hides the capture options. */
val LocalCaptureActions = compositionLocalOf<CaptureActions?> { null }
