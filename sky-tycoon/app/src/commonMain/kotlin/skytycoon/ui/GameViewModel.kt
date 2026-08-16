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

    /**
     * 캠페인 식별자. 두 슬롯이 같은 판인지 구분하지 않으면, 새 게임을 시작한 뒤
     * 이전 판의 수동 저장이 "더 많이 진행됐다"는 이유로 되살아난다.
     */
    private fun campaignKey(s: GameState) = "${s.seed}:${s.scenarioId}:${s.playerId}:${s.startYear}"

    /** 이어서 할 수 있는 저장이 있는가 (시작 화면에서 쓴다). */
    fun hasSavedGame(): Boolean = SaveSlot.entries.any { load(it) != null }

    /** 지금 진행 중인 판에 되돌아갈 수동 저장 지점이 있는가. */
    fun hasManualSave(): Boolean {
        val current = state ?: return false
        val manual = load(SaveSlot.MANUAL) ?: return false
        return campaignKey(manual) == campaignKey(current)
    }

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

    /**
     * 저장된 게임 이어하기 — 진행 중인 판(자동 저장)이 기준이다.
     * 진행도로 고르면 예전 판의 수동 저장 지점이 새 판을 밀어내고 살아난다.
     */
    fun resume(): Boolean {
        val loaded = load(SaveSlot.AUTO) ?: load(SaveSlot.MANUAL) ?: run {
            message = "저장된 게임이 없습니다."
            return false
        }
        state = loaded
        pendingReport = null
        message = "${loaded.displayYear}년 ${loaded.displayQuarter}분기부터 이어서 진행합니다."
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
        val settledTurn = current.turn
        val next = TurnEngine.advance(current)
        state = next
        // 인수당하면 이번 분기 결산이 없다. turn 을 확인하지 않으면 지난 분기 리포트가
        // 패배 화면 앞에 한 번 더 뜬다.
        pendingReport = next.airlineOrNull(next.playerId)?.lastResult?.takeIf { it.turn == settledTurn }
        busy = false
        if (!autoSave()) message = autoSaveWarning
    }

    fun dismissReport() {
        pendingReport = null
    }

    // ---- 세이브 ----

    /** 자동 저장이 실패한 채로 진행 중인가 (경고를 한 번만 띄우기 위한 플래그). */
    var saveFailed by mutableStateOf(false)
        private set

    /**
     * 자동 저장. 성공 여부를 돌려주고 메시지는 **호출부가** 정한다 —
     * 여기서 message 를 직접 쓰면 뒤이어 성공 문구를 덮어쓰는 호출부에서 경고가 사라진다.
     */
    fun autoSave(): Boolean {
        val s = state ?: return false
        val ok = store.write(SaveSlot.AUTO, Save.encode(s))
        saveFailed = !ok
        return ok
    }

    private val autoSaveWarning =
        "자동 저장에 실패했습니다. 저장 공간을 확인하세요 — 지금 종료하면 진행 상황이 사라집니다."

    /** 수동 저장은 자동 저장과 다른 슬롯에 넣는다 — 안 그러면 다음 분기에 곧바로 지워진다. */
    fun saveNow(): Boolean {
        val s = state ?: return false
        if (!store.write(SaveSlot.MANUAL, Save.encode(s))) {
            message = "저장에 실패했습니다. 저장 공간을 확인하세요."
            return false
        }
        // 이어하기는 자동 저장 슬롯을 "지금 하는 판"으로 믿는다. 새 판을 시작하고
        // 분기를 넘기기 전에 수동 저장만 했다면 자동 저장은 아직 예전 판이라,
        // 여기서 옮겨두지 않으면 이어하기가 방금 저장한 판을 못 찾는다.
        val auto = load(SaveSlot.AUTO)
        val claimed = if (auto == null || campaignKey(auto) != campaignKey(s)) autoSave() else true
        val label = "${s.displayYear}년 ${s.displayQuarter}분기"
        message = if (claimed) "${label}를 저장했습니다." else "${label}를 저장했지만 $autoSaveWarning"
        return true
    }

    /** 수동 저장 지점으로 되돌린다 (같은 판일 때만). */
    fun loadSaved(): Boolean {
        val loaded = load(SaveSlot.MANUAL) ?: run {
            message = "저장해 둔 지점이 없습니다."
            return false
        }
        val current = state
        if (current != null && campaignKey(loaded) != campaignKey(current)) {
            message = "지난 판의 저장 지점이라 되돌릴 수 없습니다."
            return false
        }
        state = loaded
        pendingReport = null
        val saved = autoSave()
        message = if (saved) {
            "${loaded.displayYear}년 ${loaded.displayQuarter}분기로 되돌렸습니다."
        } else {
            "${loaded.displayYear}년 ${loaded.displayQuarter}분기로 되돌렸지만 $autoSaveWarning"
        }
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
        val saved = autoSave()
        message = if (saved) {
            "${loaded.displayYear}년 ${loaded.displayQuarter}분기 세이브를 불러왔습니다."
        } else {
            "${loaded.displayYear}년 ${loaded.displayQuarter}분기 세이브를 불러왔지만 $autoSaveWarning"
        }
        return true
    }
}
