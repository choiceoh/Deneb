@file:OptIn(androidx.compose.ui.test.ExperimentalTestApi::class)
@file:Suppress("DEPRECATION")

package ai.deneb.deneb

import ai.deneb.sampleEngineStatus
import ai.deneb.ui.OledColorScheme
import androidx.compose.foundation.ScrollState
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.mutableStateOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.runDesktopComposeUiTest
import kotlinx.datetime.TimeZone
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class EngineDisclosureUiTest {
    @Test
    fun refreshPreservesOpenDetailsAndScrollOnNarrowScreen() = runDesktopComposeUiTest(width = 320, height = 915) {
        val status = mutableStateOf(sampleEngineStatus)
        val scroll = ScrollState(0)
        setContent {
            MaterialTheme(colorScheme = OledColorScheme) {
                Column(Modifier.verticalScroll(scroll)) {
                    EngineStatusContent(status.value, zone = TimeZone.of("Asia/Seoul"))
                }
            }
        }
        onNodeWithText("요청 디코드").assertExists()
        onNodeWithText("실제 프리필").assertExists()
        onNodeWithText("타깃 검증").assertDoesNotExist()
        onNodeWithText("실행 상세").performScrollTo().performClick()
        onNodeWithText("타깃 검증").performScrollTo().assertExists()
        val offset = scroll.value
        assertTrue(offset > 0)
        runOnIdle { status.value = status.value.copy(nowMs = status.value.nowMs + 15_000) }
        waitForIdle()
        onNodeWithText("타깃 검증").assertExists()
        assertEquals(offset, scroll.value)
        onNodeWithText("실행 상세").performScrollTo().performClick()
        onNodeWithText("타깃 검증").assertDoesNotExist()
        onNodeWithText("응답·길이 분포").performScrollTo().performClick()
        onNodeWithText("토큰 제한 종료").assertExists()
        onNodeWithText("0개 수용").assertExists()
    }
}
