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
        val backup = backup(target)
        backup.delete()
        val stashed = target.exists() && target.renameTo(backup)
        if (!tmp.renameTo(target)) {
            if (stashed) backup.renameTo(target)
            error("세이브 교체 실패")
        }
        backup.delete()
    }

    /**
     * 교체 도중 프로세스가 죽으면 직전 세이브가 `.bak` 에만 남는다. 대상만 보고 없다고
     * 하면 멀쩡히 남아 있는 캠페인을 두고 "저장된 게임이 없습니다"가 뜬다.
     */
    override fun read(slot: SaveSlot): String? {
        val target = file(slot)
        val source = target.takeIf { it.isFile } ?: backup(target).takeIf { it.isFile } ?: return null
        return runCatching { source.readText() }.getOrNull()
    }

    override fun clear(slot: SaveSlot) {
        val target = file(slot)
        target.delete()
        // 백업까지 지워야 한다 — 안 지우면 지운 슬롯이 다음 read 에서 되살아난다.
        backup(target).delete()
    }

    private fun backup(target: File) = File(dir, "${target.name}.bak")

    companion object {
        fun default(): FileSaveStore = FileSaveStore(File(System.getProperty("user.home"), ".skytycoon"))
    }
}
