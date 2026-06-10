@file:Suppress("ktlint:standard:filename")
@file:OptIn(ExperimentalDesktopTarget::class)

package ai.deneb

import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEvent
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.isCtrlPressed
import androidx.compose.ui.input.key.isMetaPressed
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.type
import androidx.compose.ui.unit.DpSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Window
import androidx.compose.ui.window.application
import androidx.compose.ui.window.rememberWindowState
import androidx.navigation.NavHostController
import androidx.navigation.compose.rememberNavController
import ai.deneb.ui.chat.composables.denebSectionDestinations
import ai.deneb.ui.chat.composables.navigateToDenebSection
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.logo
import nl.marc_apps.tts.TextToSpeechEngine
import nl.marc_apps.tts.TextToSpeechInstance
import nl.marc_apps.tts.experimental.ExperimentalDesktopTarget
import nl.marc_apps.tts.rememberTextToSpeechOrNull
import org.jetbrains.compose.resources.painterResource

fun main() {
    System.setProperty("apple.awt.application.appearance", "system")
    // Help AWT/Skiko pick up HiDPI on Linux/Wayland (Sway, GNOME fractional scaling).
    // Without this, the JVM ignores GDK_SCALE and renders at 1× on a hi-res monitor.
    if (System.getProperty("sun.java2d.uiScale.enabled") == null) {
        System.setProperty("sun.java2d.uiScale.enabled", "true")
    }
    if (System.getProperty("sun.java2d.uiScale") == null) {
        System.setProperty("sun.java2d.uiScale", "auto")
    }
    application {
        val windowState = rememberWindowState(size = DpSize(1280.dp, 800.dp))
        // Hoisted above the Window so the window-level shortcut handler can navigate.
        val navController = rememberNavController()
        Window(
            onCloseRequest = ::exitApplication,
            state = windowState,
            title = "Deneb",
            icon = painterResource(Res.drawable.logo),
            // Window-level preview so Ctrl/Cmd+digit works even while a text field
            // holds focus (the chat input usually does).
            onPreviewKeyEvent = { event -> handleSectionShortcut(event, navController) },
        ) {
            // Defer TTS initialization until after the first frame
            var ttsReady by remember { mutableStateOf(false) }
            LaunchedEffect(Unit) { ttsReady = true }
            val textToSpeech: TextToSpeechInstance? = if (ttsReady) {
                rememberTextToSpeechOrNull(TextToSpeechEngine.Google)
            } else {
                null
            }

            App(
                navController = navController,
                textToSpeech = textToSpeech,
            )
        }
    }
}

/**
 * Ctrl+1..7 (Cmd on macOS) switches between the sidebar sections, in sidebar order
 * ([denebSectionDestinations]). Returns true (consumed) only for that exact chord,
 * so plain typing and other shortcuts pass through untouched.
 */
private fun handleSectionShortcut(event: KeyEvent, navController: NavHostController): Boolean {
    if (event.type != KeyEventType.KeyDown) return false
    if (!event.isCtrlPressed && !event.isMetaPressed) return false
    val index = when (event.key) {
        Key.One -> 0
        Key.Two -> 1
        Key.Three -> 2
        Key.Four -> 3
        Key.Five -> 4
        Key.Six -> 5
        Key.Seven -> 6
        else -> return false
    }
    val dest = denebSectionDestinations.getOrNull(index) ?: return false
    navigateToDenebSection(navController, dest)
    return true
}
