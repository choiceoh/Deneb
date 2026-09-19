package ai.deneb.deneb

import ai.deneb.deneb.generated.EngineRoleHold
import ai.deneb.deneb.generated.EngineRoleMove
import ai.deneb.deneb.generated.EngineServing
import ai.deneb.deneb.generated.EngineServingProfile
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class EngineServingTest {
    private val now = 1_787_270_400_000L
    private val atRest = EngineServing(
        nowMs = now,
        available = true,
        head = "choiceoh@100.125.220.117",
        selected = "glm53",
        selectedModel = "glm-5.3-flash",
        chosenBy = "deneb",
        chosenAtMs = now - 3 * 3_600_000L,
        supervised = true,
        serving = "glm53",
        servingModel = "glm-5.3-flash",
        phase = "serving",
        profiles = listOf(EngineServingProfile("glm53", "glm-5.3-flash"), EngineServingProfile("qwen38", "qwen3.8-flash-next")),
    )
    private val chosenQwen = atRest.copy(selected = "qwen38", selectedModel = "qwen3.8-flash-next", chosenAtMs = now - 60_000L)

    @Test
    fun atRestTheLineIsJustTheModel() {
        assertNull(servingProgress(atRest))
        assertEquals(emptyList(), servingNotices(ServingState(atRest)))
        // A switch that went as asked says nothing more: the roles that moved are not listed.
        val followed = atRest.copy(moved = listOf(EngineRoleMove("coding", "wormhole/qwen3.8-flash-next", "wormhole/glm-5.3-flash")))
        assertEquals(emptyList(), servingNotices(ServingState(followed)))
    }

    // Written is not served: until the supervisor takes the choice, the line
    // names both models and says it has not started.
    @Test
    fun aChoiceInMotionNamesBothModelsAndTheStep() {
        assertEquals("glm-5.3-flash → qwen3.8-flash-next · 곧 시작", servingProgress(chosenQwen))
        assertEquals("glm-5.3-flash → qwen3.8-flash-next · 이전 모델을 내리는 중", servingProgress(chosenQwen.copy(phase = "switching")))
        // The supervisor empties SERVING once the old model is down.
        assertEquals("qwen3.8-flash-next · 띄우는 중", servingProgress(chosenQwen.copy(phase = "launching", serving = "", servingModel = "")))
        assertEquals("qwen3.8-flash-next · 플릿이 비길 기다리는 중", servingProgress(chosenQwen.copy(phase = "waiting", serving = "", servingModel = "")))
        // A boot given up is not motion; the notices say it.
        assertNull(servingProgress(atRest.copy(phase = "held")))
    }

    @Test
    fun theExceptionsAreOneLineEach() {
        val reverted = servingNotices(ServingState(atRest.copy(chosenBy = "supervisor")))
        assertEquals(listOf(ServingNotice("고른 모델이 부팅에 실패해 기본 모델로 돌아왔습니다")), reverted)
        val held = servingNotices(ServingState(atRest.copy(phase = "held")))
        assertEquals(listOf(ServingNotice("부팅 재시도를 멈췄습니다 — 확인이 필요합니다", failure = true)), held)
        val vision = atRest.copy(held = listOf(EngineRoleHold("vision", "wormhole/glm-5.3-flash", "qwen3.8-flash-next 엔트리는 이미지를 받지 않습니다")))
        assertEquals(
            "비전은 그대로 — qwen3.8-flash-next 엔트리는 이미지를 받지 않습니다",
            servingNotices(ServingState(vision)).single().text,
        )
        assertTrue(servingNotices(ServingState(atRest, requestFailed = true)).single().failure)
    }

    // A running supervisor that predates the selection would never take a
    // choice, and a fleet without the script cannot hold one: no switch, and a
    // line that says why. A gateway with the control switched off has no head
    // and nothing to report — the feature is absent, not broken.
    @Test
    fun noSwitchWithoutAFleetThatTakesIt() {
        val old = atRest.copy(supervised = false, phase = "", serving = "", servingModel = "")
        assertFalse(servingSwitchable(old))
        assertEquals("모델 전환 불가 — 슈퍼바이저가 모델 선택을 아직 읽지 않습니다", servingNotices(ServingState(old)).single().text)
        val noScript = EngineServing(nowMs = now, available = false, head = "choiceoh@100.125.220.117", reason = "플릿이 모델 선택을 아직 지원하지 않습니다 (st_production.py 없음)")
        assertFalse(servingSwitchable(noScript))
        assertEquals("모델 전환 불가 — 플릿이 모델 선택을 아직 지원하지 않습니다 (st_production.py 없음)", servingNotices(ServingState(noScript)).single().text)
        assertEquals(emptyList(), servingNotices(ServingState(EngineServing(available = false, reason = "엔진 제어가 설정되어 있지 않습니다"))))
        assertTrue(servingSwitchable(atRest))
        assertFalse(servingSwitchable(atRest.copy(profiles = atRest.profiles.take(1))))
    }
}
