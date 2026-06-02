@file:OptIn(androidx.compose.ui.ExperimentalComposeUiApi::class)

package com.inspiredandroid.kai

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.ui.ImageComposeScene
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.deneb.CalendarEventContent
import com.inspiredandroid.kai.deneb.CalendarEventDetail
import com.inspiredandroid.kai.deneb.DenebMarkdown
import com.inspiredandroid.kai.deneb.MailMessage
import com.inspiredandroid.kai.deneb.MailRow
import com.inspiredandroid.kai.ui.DarkColorScheme
import com.inspiredandroid.kai.ui.DenebRow
import com.inspiredandroid.kai.ui.DenebScreenScaffold
import com.inspiredandroid.kai.ui.DenebType
import com.inspiredandroid.kai.ui.LightColorScheme
import com.inspiredandroid.kai.ui.denebHint
import com.inspiredandroid.kai.ui.chat.composables.DenebDrawerSheet
import com.inspiredandroid.kai.ui.dynamicui.ChartNode
import com.inspiredandroid.kai.ui.dynamicui.KaiUiRenderer
import kotlinx.collections.immutable.persistentListOf
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
    renderChrome("chrome_dark.png", DarkColorScheme)
    renderChrome("chrome_light.png", LightColorScheme)
    renderDesignSample("design_dark.png", DarkColorScheme)
    renderDesignSample("design_light.png", LightColorScheme)
    renderCalendarEvent("calendar_event_dark.png", DarkColorScheme)
    renderCalendarEvent("calendar_event_light.png", LightColorScheme)
    renderChart("chart_dark.png", DarkColorScheme)
    renderChart("chart_light.png", LightColorScheme)
    println("rendered -> /tmp/deneb-render/")
}

private val sampleMail = listOf(
    Triple("김민준 부장", "내일 회의 자료 검토 부탁드립니다", true),
    Triple("GitHub", "[deneb] PR #1853 merged into main", false),
    Triple("에코프로 구매팀", "모듈 견적 회신 요청 — 6월말 납기", false),
    Triple("이서연", "(제목 없음)", false),
)

// Validates the Deneb design system (DenebScreenScaffold + DenebRow + DenebType)
// on a mock mail list: English chrome, hairline rows, no Material cards.
private fun renderDesignSample(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 760, height = 1300, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "mail", onBack = {}) {
                Column(Modifier.padding(horizontal = 24.dp)) {
                    sampleMail.forEach { (from, subject, unread) ->
                        DenebRow(onClick = {}) {
                            Text(
                                text = from,
                                style = if (unread) DenebType.rowTitleStrong else DenebType.rowTitle,
                                color = MaterialTheme.colorScheme.onBackground,
                            )
                            Spacer(Modifier.height(3.dp))
                            Text(
                                text = subject,
                                style = DenebType.snippet,
                                color = denebHint(),
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                        }
                    }
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

private val sampleEvent = CalendarEventDetail(
    id = "e1",
    title = "기획조정실 주간 회의 — 3분기 루프탑·RE100 진행 점검",
    description = "- 남도에코 모듈 입고 일정 공유\n- RE100 고객사 계약 진행률\n- 주차장 태양광 견적 리뷰",
    location = "본사 3층 대회의실",
    start = "2026-06-03T14:00:00Z",
    end = "2026-06-03T15:00:00Z",
    allDay = false,
    organizer = "오선택 전무",
    attendees = listOf("김민준 부장", "이서연 차장", "에코프로 구매팀"),
    status = "confirmed",
)

// Validates the calendar-event detail in the hybrid idiom: Deneb type skin
// (subject + section labels + body) with the Meet join as a Material button.
private fun renderCalendarEvent(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 760, height = 1100, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "일정", onBack = {}) {
                Column(Modifier.padding(horizontal = 24.dp)) {
                    CalendarEventContent(ev = sampleEvent)
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

// Renders the left navigation drawer (분석 + 기록·설정 groups) so the look can be
// checked without an APK.
private fun renderChrome(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 760, height = 1200, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Box(Modifier.width(320.dp)) {
                    DenebDrawerSheet(
                        onOpenSearch = {},
                        onOpenMail = {},
                        onOpenCalendar = {},
                        onOpenPeople = {},
                        onOpenCategories = {},
                        onNavigateToSettings = {},
                        onClose = {},
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

private fun renderChart(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 840, height = 1000, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Column(Modifier.padding(20.dp)) {
                    KaiUiRenderer(
                        node = ChartNode(
                            chartType = "bar",
                            labels = persistentListOf("1월", "2월", "3월", "4월"),
                            values = persistentListOf(12f, 28f, 19f, 34f),
                            label = "월별 매출",
                        ),
                        isInteractive = false,
                        onCallback = { _, _ -> },
                        wrapInCard = false,
                    )
                    Spacer(Modifier.height(24.dp))
                    KaiUiRenderer(
                        node = ChartNode(
                            chartType = "line",
                            labels = persistentListOf("월", "화", "수", "목", "금"),
                            values = persistentListOf(5f, 15f, 9f, 22f, 14f),
                            label = "주간 추세",
                        ),
                        isInteractive = false,
                        onCallback = { _, _ -> },
                        wrapInCard = false,
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
