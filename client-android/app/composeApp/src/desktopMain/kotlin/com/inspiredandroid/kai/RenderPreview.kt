@file:OptIn(androidx.compose.ui.ExperimentalComposeUiApi::class)

package com.inspiredandroid.kai

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.ui.ImageComposeScene
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.deneb.DenebMarkdown
import com.inspiredandroid.kai.deneb.MailMessage
import com.inspiredandroid.kai.deneb.MailRow
import com.inspiredandroid.kai.ui.DarkColorScheme
import com.inspiredandroid.kai.ui.LightColorScheme
import org.jetbrains.skia.EncodedImageFormat
import java.io.File

// Off-screen render harness: renders Deneb composables to PNG via Skia so the
// look (and bugs like invisible text) can be inspected without building +
// installing the APK. Run with `./gradlew :composeApp:renderPreviews`.

private val sample = listOf(
    MailMessage("1", "김철수 <kim@topsolar.kr>", "내일 회의 자료 확인 부탁드립니다", "안녕하세요, 첨부한 자료 검토 후 회신 부탁드립니다.", "2026-05-31T09:12:00Z", true),
    MailMessage("2", "GitHub <noreply@github.com>", "[deneb] PR #1814 merged", "Your pull request was merged into main.", "2026-05-31T08:40:00Z", false),
    MailMessage("3", "이영희 <lee@example.com>", "(제목 없음)", "", "2026-05-30T22:05:00Z", false),
)

private val markdownSample = """
    # 프로젝트 X 개요
    **상태:** 진행 중 · 담당 김철수

    ## 핵심 결정
    - NVFP4 MTP graft 적용 (mean accept ~2.5)
    - `--speculative-config` 로 드래프터 강제

    ### 다음 단계
    1. 라이브 검증
    2. PR 병합
""".trimIndent()

fun main() {
    System.setProperty("java.awt.headless", "true")
    render("mail_dark.png", DarkColorScheme)
    render("mail_light.png", LightColorScheme)
    renderMarkdown("markdown_dark.png", DarkColorScheme)
    println("rendered -> /tmp/deneb-render/")
}

private fun renderMarkdown(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 840, height = 700, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                DenebMarkdown(markdownSample, Modifier.padding(20.dp))
            }
        }
    }
    val image = scene.render()
    val data = image.encodeToData(EncodedImageFormat.PNG) ?: error("PNG encode failed")
    File("/tmp/deneb-render").mkdirs()
    File("/tmp/deneb-render/$name").writeBytes(data.bytes)
    scene.close()
}

private fun render(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 840, height = 1100, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Column {
                    Text(
                        "받은 메일",
                        style = MaterialTheme.typography.headlineMedium,
                        modifier = Modifier.padding(16.dp),
                    )
                    sample.forEach { m ->
                        MailRow(m, selecting = false, isSelected = false, onTap = {}, onLongPress = {})
                    }
                    // Body-visibility check: this Text sets NO explicit color, so it
                    // relies on the Surface providing onBackground. If it renders, the
                    // dark-mode invisible-text fix works.
                    Text(
                        "— 상세 본문(색 미지정 테스트) —",
                        modifier = Modifier.padding(16.dp),
                    )
                    Text(
                        "이 문장은 색을 명시하지 않았습니다. Surface가 onBackground를 공급해 다크모드에서도 보여야 정상입니다.",
                        modifier = Modifier.padding(horizontal = 16.dp),
                    )
                }
            }
        }
    }
    val image = scene.render()
    val data = image.encodeToData(EncodedImageFormat.PNG) ?: error("PNG encode failed")
    File("/tmp/deneb-render").mkdirs()
    File("/tmp/deneb-render/$name").writeBytes(data.bytes)
    scene.close()
}
