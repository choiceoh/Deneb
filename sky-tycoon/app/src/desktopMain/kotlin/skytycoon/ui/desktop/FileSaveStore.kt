package skytycoon.ui.desktop

import skytycoon.ui.SaveStore
import java.io.File

/** 데스크톱 세이브 — 홈 디렉터리 아래 한 슬롯. */
class FileSaveStore(private val file: File) : SaveStore {
    override fun write(text: String) {
        file.parentFile?.mkdirs()
        file.writeText(text)
    }

    override fun read(): String? = if (file.isFile) runCatching { file.readText() }.getOrNull() else null

    override fun clear() {
        file.delete()
    }

    companion object {
        fun default(): FileSaveStore =
            FileSaveStore(File(System.getProperty("user.home"), ".skytycoon/save.json"))
    }
}
