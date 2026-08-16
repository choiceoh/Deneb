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
        if (!tmp.renameTo(target)) replaceViaBackup(tmp, target)
    }.isSuccess

    /**
     * 대상 파일이 있으면 rename 이 실패하는 플랫폼(윈도 등)을 위한 경로.
     * 기존 세이브를 **지우지 않고** 옆으로 치워 두고, 교체가 실패하면 되돌린다 —
     * 지웠다가 두 번째 rename 마저 실패하면 유일한 복구본이 사라진다.
     */
    internal fun replaceViaBackup(tmp: File, target: File) {
        val backup = File(target.parentFile, "${target.name}.bak")
        backup.delete()
        val stashed = target.exists() && target.renameTo(backup)
        if (!tmp.renameTo(target)) {
            if (stashed) backup.renameTo(target)
            error("세이브 교체 실패")
        }
        backup.delete()
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
