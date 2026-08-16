package skytycoon.ui.desktop

import androidx.compose.ui.unit.DpSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Window
import androidx.compose.ui.window.application
import androidx.compose.ui.window.rememberWindowState
import skytycoon.ui.App
import skytycoon.ui.SaveStorage

fun main() = application {
    SaveStorage.current = FileSaveStore.default()
    Window(
        onCloseRequest = ::exitApplication,
        title = "Sky Tycoon — 항공사 경영 시뮬레이션",
        state = rememberWindowState(size = DpSize(1360.dp, 900.dp)),
    ) {
        App()
    }
}
