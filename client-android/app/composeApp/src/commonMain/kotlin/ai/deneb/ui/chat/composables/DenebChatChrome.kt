package ai.deneb.ui.chat.composables

import androidx.compose.runtime.compositionLocalOf

// Capture actions for the chat input. The left navigation drawer that used to host
// them (a typographic menu) was retired when the bottom tab bar took over section
// navigation and the left drawer became the session history; the captures now live
// in the input's attach (+) menu (QuestionInput).

/**
 * Platform capture actions surfaced in the chat input's attach (+) menu. Provided
 * by the Android entry point via [LocalCaptureActions]; null (the default) hides
 * them on platforms (desktop/iOS) without these system launchers.
 */
data class CaptureActions(
    val onCaptureImage: () -> Unit,
    val onCaptureAudio: () -> Unit,
    val onVoiceInput: () -> Unit,
)

/** Ambient capture actions; null hides the capture options. */
val LocalCaptureActions = compositionLocalOf<CaptureActions?> { null }
