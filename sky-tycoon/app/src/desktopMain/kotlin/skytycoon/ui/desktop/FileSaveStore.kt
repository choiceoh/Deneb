package skytycoon.ui.desktop

import skytycoon.ui.SaveSlot
import skytycoon.ui.SaveStore
import java.io.File

/** 데스크톱 세이브 — 홈 디렉터리 아래 슬롯별 파일. */
class FileSaveStore(private val dir: File) : SaveStore {
    private fun file(slot: SaveSlot) = File(dir, if (slot == SaveSlot.AUTO) "auto.json" else "manual.json")

    override fun write(slot: SaveSlot, text: String) {
        dir.mkdirs()
        runCatching { file(slot).writeText(text) }
    }

    override fun read(slot: SaveSlot): String? =
        file(slot).takeIf { it.isFile }?.let { runCatching { it.readText() }.getOrNull() }

    override fun clear(slot: SaveSlot) {
        file(slot).delete()
    }

    companion object {
        fun default(): FileSaveStore = FileSaveStore(File(System.getProperty("user.home"), ".skytycoon"))
    }
}
