package ai.deneb.deneb

import ai.deneb.ui.chat.History
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertIs

class TurnRecoveryEffectiveProbeTest {

    @Test
    fun answeredProbeBecomesStillRunningWhileGatewayRunActive() {
        val probe = probeTranscriptForTurn(
            listOf(
                History(role = History.Role.USER, content = "question"),
                History(role = History.Role.ASSISTANT, content = "확인하고 답할게요"),
            ),
            "question",
        )
        assertIs<TurnProbe.Answered>(probe)
        assertEquals(TurnProbe.StillRunning, effectiveTurnProbe(probe, turnRunning = true))
    }

    @Test
    fun answeredProbeStaysAnsweredWhenGatewayRunFinished() {
        val probe = TurnProbe.Answered("final answer")
        assertEquals(probe, effectiveTurnProbe(probe, turnRunning = false))
    }
}
