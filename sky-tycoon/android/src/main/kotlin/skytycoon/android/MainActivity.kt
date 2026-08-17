package skytycoon.android

import android.graphics.Color
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
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
        // 앱은 항상 어두운 팔레트(Ink)로 그린다. 인자 없는 enableEdgeToEdge 는 기기 테마를
        // 따라가므로, 라이트 모드 폰에서 시계·상태 아이콘이 어두운 색으로 깔려 검은 바 위에서
        // 보이지 않는다. 배경이 어둡다고 못박아 아이콘을 밝은 쪽으로 고정한다.
        enableEdgeToEdge(
            statusBarStyle = SystemBarStyle.dark(Color.TRANSPARENT),
            navigationBarStyle = SystemBarStyle.dark(Color.TRANSPARENT),
        )
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
            val backup = backup(target)
            backup.delete()
            val stashed = target.exists() && target.renameTo(backup)
            if (!tmp.renameTo(target)) {
                if (stashed) backup.renameTo(target)
                error("세이브 교체 실패")
            }
            backup.delete()
        }
    }.isSuccess

    // 교체 도중 죽으면 직전 세이브가 .bak 에만 남는다 — 그것도 봐야 캠페인을 안 잃는다.
    override fun read(slot: SaveSlot): String? {
        val target = file(slot)
        val source = target.takeIf { it.isFile } ?: backup(target).takeIf { it.isFile } ?: return null
        return runCatching { source.readText() }.getOrNull()
    }

    override fun clear(slot: SaveSlot) {
        val target = file(slot)
        target.delete()
        // 백업까지 지워야 지운 슬롯이 다음 read 에서 되살아나지 않는다.
        backup(target).delete()
    }

    private fun backup(target: File) = File(dir, "${target.name}.bak")
}
