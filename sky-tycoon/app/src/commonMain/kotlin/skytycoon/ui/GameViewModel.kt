package skytycoon.ui

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import skytycoon.core.model.GameState
import skytycoon.core.model.QuarterResult
import skytycoon.core.save.Save
import skytycoon.core.sim.Actions
import skytycoon.core.sim.Command
import skytycoon.core.sim.NewGame
import skytycoon.core.sim.TurnEngine
import kotlin.random.Random

/**
 * 게임 상태 보관소. 시뮬레이션이 순수 함수라 여기서는 "새 상태로 갈아끼우기"만 하면
 * Compose 가 알아서 다시 그린다.
 */
class GameViewModel {
    var state by mutableStateOf<GameState?>(null)
        private set

    /** 마지막 명령의 결과 메시지 (스낵바). */
    var message by mutableStateOf<String?>(null)

    /** 분기 결산 리포트를 띄울지. */
    var pendingReport by mutableStateOf<QuarterResult?>(null)

    var busy by mutableStateOf(false)
        private set

    val game: GameState get() = requireNotNull(state) { "게임이 아직 시작되지 않았습니다" }

    fun start(scenarioId: String, companyId: String, difficultyId: String) {
        state = NewGame.create(
            scenarioId = scenarioId,
            companyId = companyId,
            difficultyId = difficultyId,
            seed = Random.nextInt(),
        )
        pendingReport = null
        message = null
    }

    fun quit() {
        state = null
        pendingReport = null
    }

    /** 플레이어 명령 실행. 실패하면 이유를 스낵바로 알린다. */
    fun run(cmd: Command): Boolean {
        val current = state ?: return false
        val result = Actions.execute(current, cmd)
        if (result.ok) {
            state = result.state
            if (result.message.isNotBlank()) message = result.message
        } else {
            message = result.message
        }
        return result.ok
    }

    fun endTurn() {
        val current = state ?: return
        if (current.outcome != null) return
        busy = true
        val next = TurnEngine.advance(current)
        state = next
        pendingReport = next.airlineOrNull(next.playerId)?.lastResult
        busy = false
    }

    fun dismissReport() {
        pendingReport = null
    }

    // ---- 세이브 ----

    fun exportSave(): String? = state?.let { Save.encode(it, indent = true) }

    fun importSave(text: String): Boolean {
        val loaded = Save.decodeOrNull(text) ?: run {
            message = "세이브 파일을 읽을 수 없습니다."
            return false
        }
        state = loaded
        message = "${loaded.year}년 ${loaded.quarter}분기 세이브를 불러왔습니다."
        return true
    }
}
