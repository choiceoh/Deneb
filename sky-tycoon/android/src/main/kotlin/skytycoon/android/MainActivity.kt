package skytycoon.android

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import skytycoon.ui.App
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
        SaveStorage.current = AndroidSaveStore(File(filesDir, "save.json"))
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setContent { App() }
    }
}

private class AndroidSaveStore(private val file: File) : SaveStore {
    override fun write(text: String) {
        runCatching { file.writeText(text) }
    }

    override fun read(): String? = if (file.isFile) runCatching { file.readText() }.getOrNull() else null

    override fun clear() {
        file.delete()
    }
}
