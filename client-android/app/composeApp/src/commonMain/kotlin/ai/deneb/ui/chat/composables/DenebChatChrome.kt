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
 * [onImageFile]/[onAudioFile]/[onDocumentFile] receive an already-picked file (the
 * input owns the single picker); the entry point reads its bytes and runs the
 * matching gateway capture turn.
 */
data class CaptureActions(
    val onImageFile: (PlatformFile) -> Unit,
    val onAudioFile: (PlatformFile) -> Unit,
    val onDocumentFile: (PlatformFile) -> Unit,
    val onVoiceInput: () -> Unit,
)

/** Ambient capture actions; null hides the capture options. */
val LocalCaptureActions = compositionLocalOf<CaptureActions?> { null }
