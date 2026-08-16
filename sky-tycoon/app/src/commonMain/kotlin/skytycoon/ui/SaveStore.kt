package skytycoon.ui

/**
 * 세이브를 어디에 둘지는 플랫폼마다 다르다 (데스크톱은 홈 디렉터리, 안드로이드는 앱 전용 저장소).
 * 코어와 UI 는 이 인터페이스만 알고, 실제 경로는 각 진입점(Main / MainActivity)이 꽂아 준다.
 *
 * expect/actual 대신 이 방식을 쓴 이유: 소스셋 계층을 건드리지 않고도 테스트에서
 * 메모리 구현으로 바꿔 끼울 수 있다.
 */
interface SaveStore {
    fun write(text: String)
    fun read(): String?
    fun clear()
}

/** 기본 구현 — 앱을 껐다 켜면 사라진다. 진입점이 파일 기반 구현을 꽂으면 대체된다. */
class MemorySaveStore : SaveStore {
    private var data: String? = null
    override fun write(text: String) {
        data = text
    }

    override fun read(): String? = data
    override fun clear() {
        data = null
    }
}

object SaveStorage {
    var current: SaveStore = MemorySaveStore()
}
