package skytycoon.ui

import skytycoon.core.save.Save
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * 세이브 슬롯이 하나뿐이라, "언제 슬롯을 차지하는가"가 곧 캠페인의 안전장치다.
 */
class SaveFlowTest {

    @Test
    fun `새 게임을 시작하는 것만으로는 기존 세이브를 덮어쓰지 않는다`() {
        val store = MemorySaveStore()
        val first = GameViewModel(store)
        first.start("goldenage", "hanseong", "normal", seed = 1)
        first.endTurn()
        first.endTurn()
        val saved = store.read(SaveSlot.AUTO)!!
        val savedTurn = Save.decode(saved).turn

        // 시작 화면에서 다른 회사로 새 게임을 눌러본다.
        val second = GameViewModel(store)
        second.start("goldenage", "fuji", "normal", seed = 2)
        assertEquals(saved, store.read(SaveSlot.AUTO), "새 게임을 고른 것만으로 기존 캠페인이 지워졌다")

        // 되돌아가 이어하기를 하면 원래 진행 상황이 살아 있어야 한다.
        val third = GameViewModel(store)
        assertTrue(third.hasSavedGame())
        assertTrue(third.resume())
        assertEquals(savedTurn, third.game.turn)
        assertEquals("hanseong", third.game.playerId)
    }

    @Test
    fun `새 판을 수동 저장하면 이어하기가 그 판을 집는다`() {
        val store = MemorySaveStore()
        val old = GameViewModel(store)
        old.start("goldenage", "hanseong", "normal", seed = 1)
        old.endTurn()
        old.endTurn()

        // 새 판을 시작해 분기를 넘기기 전에 수동 저장만 한다. 자동 저장 슬롯은
        // 아직 예전 판이라, 여기서 슬롯을 넘겨받지 않으면 이어하기가 예전 판을 집는다.
        val fresh = GameViewModel(store)
        fresh.start("goldenage", "fuji", "normal", seed = 2)
        assertTrue(fresh.saveNow())

        val resumed = GameViewModel(store)
        assertTrue(resumed.resume())
        assertEquals("fuji", resumed.game.playerId, "방금 저장한 새 판 대신 예전 판이 되살아났다")
        assertEquals(2, resumed.game.seed)
    }

    @Test
    fun `같은 판을 수동 저장해도 자동 저장을 과거로 되돌리지 않는다`() {
        val store = MemorySaveStore()
        val vm = GameViewModel(store)
        vm.start("goldenage", "hanseong", "normal", seed = 1)
        vm.endTurn()
        vm.saveNow()
        vm.endTurn()
        vm.endTurn()
        val advanced = vm.game.turn

        // 이 시점의 수동 저장은 자동 저장과 같은 판이므로 자동 저장은 그대로여야 한다.
        vm.saveNow()
        assertEquals(advanced, Save.decode(store.read(SaveSlot.AUTO)!!).turn, "자동 저장이 과거로 밀렸다")
    }

    @Test
    fun `이어하기는 진행 중인 판을 집는다`() {
        val store = MemorySaveStore()

        // 1) 예전 판을 한참 굴리고 중간 지점을 저장해 둔다.
        val old = GameViewModel(store)
        old.start("goldenage", "hanseong", "normal", seed = 100)
        repeat(6) { old.endTurn() }
        old.saveNow()
        val oldTurn = old.game.turn

        // 2) 새 판을 시작해 한 분기만 넘긴다.
        val fresh = GameViewModel(store)
        fresh.start("goldenage", "britannia", "normal", seed = 200)
        fresh.endTurn()

        // 3) 이어하기 — 진행도만 보면 예전 판(6분기)이 이기지만, 지금 판이 나와야 한다.
        val resumed = GameViewModel(store)
        assertTrue(resumed.resume())
        assertEquals("britannia", resumed.game.playerId, "예전 판의 수동 저장이 새 판을 밀어냈다")
        assertTrue(resumed.game.turn < oldTurn)

        // 지난 판의 저장 지점은 되돌리기 대상으로도 노출되지 않는다.
        assertFalse(resumed.hasManualSave())
        assertFalse(resumed.loadSaved())
        assertEquals("britannia", resumed.game.playerId)
    }

    @Test
    fun `분기를 넘기면 자동 저장된다`() {
        val store = MemorySaveStore()
        val vm = GameViewModel(store)
        assertFalse(vm.hasSavedGame())
        vm.start("goldenage", "hanseong", "normal", seed = 7)
        vm.endTurn()
        assertTrue(vm.hasSavedGame(), "분기를 넘겼는데 저장되지 않았다")
        assertEquals(vm.game.turn, Save.decode(store.read(SaveSlot.AUTO)!!).turn)
    }

    @Test
    fun `저장 지점으로 되돌리면 그 분기 상태가 그대로 온다`() {
        val store = MemorySaveStore()
        val vm = GameViewModel(store)
        vm.start("goldenage", "hanseong", "normal", seed = 11)
        vm.endTurn()

        // 아직 수동 저장이 없으면 되돌릴 곳도 없다.
        assertFalse(vm.hasManualSave())
        assertFalse(vm.loadSaved())

        val marker = vm.game.turn
        val cashAtMarker = vm.game.player.cash
        vm.saveNow()

        // 여러 분기를 더 굴려도 수동 저장 지점은 자동 저장에 덮이지 않아야 한다.
        repeat(3) { vm.endTurn() }
        assertTrue(vm.game.turn > marker)

        assertTrue(vm.loadSaved())
        assertEquals(marker, vm.game.turn, "저장해 둔 분기로 돌아오지 않았다")
        assertEquals(cashAtMarker, vm.game.player.cash)
    }

    @Test
    fun `저장에 실패하면 성공했다고 알리지 않는다`() {
        // 디스크가 꽉 찼는데 "저장했습니다"가 뜨면, 플레이어는 안심하고 앱을 닫는다.
        val store = FailingSaveStore()
        val vm = GameViewModel(store)
        vm.start("goldenage", "hanseong", "normal", seed = 5)

        assertFalse(vm.saveNow(), "저장이 실패했는데 성공으로 보고했다")
        assertTrue(vm.message?.contains("실패") == true, "실패를 알리지 않았다: ${vm.message}")

        vm.endTurn()
        assertTrue(vm.saveFailed, "자동 저장 실패가 표시되지 않았다")
        assertTrue(vm.message?.contains("실패") == true, "분기 진행 시 실패를 알리지 않았다")
        assertFalse(vm.hasSavedGame(), "쓰이지도 않은 세이브가 있다고 나온다")
    }

    @Test
    fun `되돌리기 성공 메시지가 저장 실패 경고를 덮지 않는다`() {
        // 저장 지점은 읽히지만 디스크가 꽉 차 자동 저장은 실패하는 상황.
        val seed = GameViewModel(MemorySaveStore()).let { warm ->
            warm.start("goldenage", "hanseong", "normal", seed = 3)
            warm.endTurn()
            skytycoon.core.save.Save.encode(warm.game)
        }
        val store = FailingSaveStore(mapOf(SaveSlot.MANUAL to seed))
        val vm = GameViewModel(store)
        vm.start("goldenage", "hanseong", "normal", seed = 3)

        assertTrue(vm.loadSaved(), "읽기는 되는데 되돌리기가 실패했다")
        assertTrue(
            vm.message?.contains("실패") == true,
            "되돌리기 성공 문구가 저장 실패 경고를 덮었다: ${vm.message}",
        )
        assertTrue(vm.saveFailed)
    }
}

/**
 * 항상 쓰기에 실패하는 저장소 (디스크 가득 참·읽기 전용 상황).
 * 읽기는 미리 넣어둔 내용을 돌려줘 "불러오기는 되는데 저장은 안 되는" 상황을 재현한다.
 */
private class FailingSaveStore(private val preset: Map<SaveSlot, String> = emptyMap()) : SaveStore {
    override fun write(slot: SaveSlot, text: String) = false
    override fun read(slot: SaveSlot): String? = preset[slot]
    override fun clear(slot: SaveSlot) = Unit
}
