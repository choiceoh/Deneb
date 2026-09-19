package ai.deneb.deneb

import ai.deneb.deneb.generated.EngineModelRow
import ai.deneb.deneb.generated.EngineStatusResult
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class EngineModelsTest {
    private val glm = EngineModelRow(model = "glm-5.3-flash", current = true, days = 3, requests = 3_091, lastDay = "2026-09-19")
    private val qwen = EngineModelRow(model = "qwen3.8-flash-next", current = false, days = 2, requests = 212, lastDay = "2026-09-18")

    private fun status(selected: String, reachable: Boolean = true, live: String = glm.model) = EngineStatusResult(reachable = reachable, model = if (reachable) live else "", models = listOf(glm, qwen), selectedModel = selected)

    @Test
    fun theCurrentTabSaysWhetherTheEngineAnswersAsItNow() {
        assertEquals("서빙 중", engineModelMarker(glm, status(glm.model)))
        // A down engine serves nothing, whatever it served last.
        assertEquals("마지막", engineModelMarker(glm, status(glm.model, reachable = false)))
        assertNull(engineModelMarker(qwen, status(glm.model)))
    }

    @Test
    fun onlyAPastModelGetsTheScopeNote() {
        assertNull(engineModelNote(status(glm.model)))
        val note = engineModelNote(status(qwen.model))
        assertEquals("지난 모델의 통계 · 최근 7일 중 2일 · 마지막 9/18. 가용성·점유율은 엔진 전체 기준입니다.", note)
    }

    @Test
    fun theLiveInternalsBelongToTheCurrentModelOnly() {
        assertTrue(status(glm.model).viewingCurrentModel())
        assertFalse(status(qwen.model).viewingCurrentModel())
        // An engine that never named a model has nothing to filter by: everything is current.
        assertTrue(EngineStatusResult().viewingCurrentModel())
    }
}
