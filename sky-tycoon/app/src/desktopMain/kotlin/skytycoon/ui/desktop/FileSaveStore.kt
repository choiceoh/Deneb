package skytycoon.ui.desktop

import skytycoon.ui.SaveSlot
import skytycoon.ui.SaveStore
import java.io.File

/** 데스크톱 세이브 — 홈 디렉터리 아래 슬롯별 파일. */
class FileSaveStore(private val dir: File) : SaveStore {
    private fun file(slot: SaveSlot) = File(dir, if (slot == SaveSlot.AUTO) "auto.json" else "manual.json")

    /**
     * 임시 파일에 다 쓴 뒤 이름을 바꿔 끼운다. 곧바로 덮어쓰면 쓰다가 실패했을 때
     * 직전 세이브까지 깨져 되돌아갈 곳이 사라진다.
     */
    override fun write(slot: SaveSlot, text: String): Boolean = runCatching {
        dir.mkdirs()
        val target = file(slot)
        val tmp = File(dir, "${target.name}.tmp")
        tmp.writeText(text)
        if (!tmp.renameTo(target)) {
            target.delete()
            check(tmp.renameTo(target)) { "세이브 교체 실패" }
        }
    }.isSuccess

    override fun read(slot: SaveSlot): String? =
        file(slot).takeIf { it.isFile }?.let { runCatching { it.readText() }.getOrNull() }

    override fun clear(slot: SaveSlot) {
        file(slot).delete()
    }

    companion object {
        fun default(): FileSaveStore = FileSaveStore(File(System.getProperty("user.home"), ".skytycoon"))
    }
}
