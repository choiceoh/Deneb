package skytycoon.ui

/**
 * 세이브 슬롯. 자동 저장 하나만 두면 매 분기 덮어써져서 "저장해 두고 나중에 되돌리기"가
 * 성립하지 않는다 (수동 저장이 다음 턴에 바로 지워진다). 그래서 둘로 나눈다.
 */
enum class SaveSlot {
    /** 분기를 넘길 때마다 자동으로 갱신된다. 앱이 죽어도 캠페인이 남게 하는 용도. */
    AUTO,

    /** 플레이어가 명시적으로 저장한 시점. 자동 저장이 건드리지 않는다. */
    MANUAL,
}

/**
 * 세이브를 어디에 둘지는 플랫폼마다 다르다 (데스크톱은 홈 디렉터리, 안드로이드는 앱 전용 저장소).
 * 코어와 UI 는 이 인터페이스만 알고, 실제 경로는 각 진입점(Main / MainActivity)이 꽂아 준다.
 */
interface SaveStore {
    fun write(slot: SaveSlot, text: String)
    fun read(slot: SaveSlot): String?
    fun clear(slot: SaveSlot)
}

/** 기본 구현 — 앱을 껐다 켜면 사라진다. 진입점이 파일 기반 구현을 꽂으면 대체된다. */
class MemorySaveStore : SaveStore {
    private val data = mutableMapOf<SaveSlot, String>()
    override fun write(slot: SaveSlot, text: String) {
        data[slot] = text
    }

    override fun read(slot: SaveSlot): String? = data[slot]

    override fun clear(slot: SaveSlot) {
        data.remove(slot)
    }
}

object SaveStorage {
    var current: SaveStore = MemorySaveStore()
}
