package skytycoon.android

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import skytycoon.ui.App
import skytycoon.ui.SaveSlot
import skytycoon.ui.SaveStore
import skytycoon.ui.SaveStorage
import java.io.File

/**
 * 안드로이드 진입점. 화면은 전부 `:app` 의 Compose Multiplatform 공용 코드라
 * 여기는 껍데기만 있으면 된다 (같은 화면이 데스크톱에서도 그대로 뜬다).
 */
class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        // 백그라운드에서 프로세스가 정리돼도 캠페인이 남도록 앱 전용 저장소에 붙인다.
        SaveStorage.current = AndroidSaveStore(filesDir)
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setContent { App() }
    }
}

private class AndroidSaveStore(private val dir: File) : SaveStore {
    private fun file(slot: SaveSlot) = File(dir, if (slot == SaveSlot.AUTO) "auto.json" else "manual.json")

    // 임시 파일에 쓰고 바꿔 끼운다 — 쓰다 실패해도 직전 세이브는 남는다.
    override fun write(slot: SaveSlot, text: String): Boolean = runCatching {
        val target = file(slot)
        val tmp = File(dir, "${target.name}.tmp")
        tmp.writeText(text)
        if (!tmp.renameTo(target)) {
            // 기존 세이브를 지우지 않고 옆으로 치운다. 지웠다가 교체마저 실패하면
            // 유일한 복구본이 사라진다.
            val backup = File(dir, "${target.name}.bak")
            backup.delete()
            val stashed = target.exists() && target.renameTo(backup)
            if (!tmp.renameTo(target)) {
                if (stashed) backup.renameTo(target)
                error("세이브 교체 실패")
            }
            backup.delete()
        }
    }.isSuccess

    override fun read(slot: SaveSlot): String? =
        file(slot).takeIf { it.isFile }?.let { runCatching { it.readText() }.getOrNull() }

    override fun clear(slot: SaveSlot) {
        file(slot).delete()
    }
}
