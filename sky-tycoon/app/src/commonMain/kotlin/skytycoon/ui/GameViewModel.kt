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
 *
 * 분기를 넘길 때마다 자동 저장한다 — 안드로이드는 백그라운드에서 프로세스가 통째로
 * 정리될 수 있어서, 저장이 없으면 20년짜리 캠페인이 소리 없이 사라진다.
 */
class GameViewModel(private val store: SaveStore = SaveStorage.current) {
    var state by mutableStateOf<GameState?>(null)
        private set

    /** 마지막 명령의 결과 메시지 (스낵바). */
    var message by mutableStateOf<String?>(null)

    /** 분기 결산 리포트를 띄울지. */
    var pendingReport by mutableStateOf<QuarterResult?>(null)

    var busy by mutableStateOf(false)
        private set

    val game: GameState get() = requireNotNull(state) { "게임이 아직 시작되지 않았습니다" }

    private fun load(slot: SaveSlot): GameState? = store.read(slot)?.let { Save.decodeOrNull(it) }

    /** 이어서 할 수 있는 저장이 있는가 (시작 화면에서 쓴다). */
    fun hasSavedGame(): Boolean = SaveSlot.entries.any { load(it) != null }

    /** 수동으로 저장해 둔 지점이 있는가. */
    fun hasManualSave(): Boolean = load(SaveSlot.MANUAL) != null

    fun start(scenarioId: String, companyId: String, difficultyId: String, seed: Int = Random.nextInt()) {
        state = NewGame.create(
            scenarioId = scenarioId,
            companyId = companyId,
            difficultyId = difficultyId,
            seed = seed,
        )
        pendingReport = null
        message = null
        // 여기서 저장하지 않는다. 시작 화면에서 잘못 눌렀을 때 기존 캠페인이
        // 즉시 지워지면 되돌릴 방법이 없다 — 첫 분기를 넘겨야 비로소 슬롯을 차지한다.
    }

    /** 저장된 게임 이어하기 — 자동/수동 중 더 멀리 진행된 쪽을 집는다. */
    fun resume(): Boolean {
        val loaded = SaveSlot.entries.mapNotNull { load(it) }.maxByOrNull { it.turn } ?: run {
            message = "저장된 게임이 없습니다."
            return false
        }
        state = loaded
        pendingReport = null
        message = "${loaded.year}년 ${loaded.quarter}분기부터 이어서 진행합니다."
        return true
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
        autoSave()
    }

    fun dismissReport() {
        pendingReport = null
    }

    // ---- 세이브 ----

    fun autoSave() {
        state?.let { store.write(SaveSlot.AUTO, Save.encode(it)) }
    }

    /** 수동 저장은 자동 저장과 다른 슬롯에 넣는다 — 안 그러면 다음 분기에 곧바로 지워진다. */
    fun saveNow(): Boolean {
        val s = state ?: return false
        store.write(SaveSlot.MANUAL, Save.encode(s))
        message = "${s.year}년 ${s.quarter}분기를 저장했습니다."
        return true
    }

    /** 수동 저장 지점으로 되돌린다. */
    fun loadSaved(): Boolean {
        val loaded = load(SaveSlot.MANUAL) ?: run {
            message = "저장해 둔 지점이 없습니다."
            return false
        }
        state = loaded
        pendingReport = null
        autoSave()
        message = "${loaded.year}년 ${loaded.quarter}분기로 되돌렸습니다."
        return true
    }

    /** 파일로 내보낼 때 쓰는 JSON. */
    fun exportSave(): String? = state?.let { Save.encode(it, indent = true) }

    fun importSave(text: String): Boolean {
        @Suppress("UNUSED_EXPRESSION")
        val loaded = Save.decodeOrNull(text) ?: run {
            message = "세이브 파일을 읽을 수 없습니다."
            return false
        }
        state = loaded
        autoSave()
        message = "${loaded.year}년 ${loaded.quarter}분기 세이브를 불러왔습니다."
        return true
    }
}
