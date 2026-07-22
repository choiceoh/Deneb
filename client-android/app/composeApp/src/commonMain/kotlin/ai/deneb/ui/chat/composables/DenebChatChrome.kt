package ai.deneb.ui.chat.composables

import androidx.compose.runtime.compositionLocalOf
import io.github.vinceglb.filekit.PlatformFile

// Capture actions for the chat input. The left navigation drawer that used to host
// them (a typographic menu) was retired when the bottom tab bar took over section
// navigation and the left drawer became the session history.
//
// The attach (+) button no longer asks "what to insert" — it opens one file picker,
// stages the picks as chips, and on send hands them to [onFilesBatch] (one gateway
// batch capture the agent reads and cross-references). The live mic ([onVoiceInput],
// system speech recognizer) is not a file, so it is the chat input's trailing mic
// button (shown when the field is empty) rather than crowding the attach picker.

/**
 * Platform capture actions. Provided by the Android entry point via
 * [LocalCaptureActions]; null (the default) on platforms (desktop/iOS) without
 * these system launchers — there the attach picker simply attaches the file.
 *
 * [onFilesBatch] receives the files the user staged (one or many) when they hit send,
 * plus the composer text as the capture's caption, and runs one gateway batch capture
 * the agent reads and cross-references. The caption is blank when the composer was empty.
 */
data class CaptureActions(
    val onFilesBatch: (List<PlatformFile>, String) -> Unit,
    val onVoiceInput: () -> Unit,
)

/** Ambient capture actions; null hides the capture options. */
val LocalCaptureActions = compositionLocalOf<CaptureActions?> { null }
