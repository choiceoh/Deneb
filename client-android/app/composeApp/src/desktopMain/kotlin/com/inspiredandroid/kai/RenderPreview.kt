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
import com.inspiredandroid.kai.deneb.CalMonth
import com.inspiredandroid.kai.deneb.CalendarAddContent
import com.inspiredandroid.kai.deneb.CalendarDayList
import com.inspiredandroid.kai.deneb.CalendarEvent
import com.inspiredandroid.kai.deneb.CalendarEventContent
import com.inspiredandroid.kai.deneb.CalendarEventDetail
import com.inspiredandroid.kai.deneb.CalendarMonthGrid
import com.inspiredandroid.kai.deneb.DenebMarkdown
import com.inspiredandroid.kai.deneb.buildMonthGrid
import com.inspiredandroid.kai.deneb.eventDays
import com.inspiredandroid.kai.deneb.koreanDayOfWeek
import com.inspiredandroid.kai.deneb.layoutMonthBars
import com.inspiredandroid.kai.deneb.MailMessage
import com.inspiredandroid.kai.deneb.MailRow
import com.inspiredandroid.kai.ui.markdown.MarkdownContent
import com.inspiredandroid.kai.ui.DarkColorScheme
import com.inspiredandroid.kai.ui.DenebRow
import com.inspiredandroid.kai.ui.DenebScreenScaffold
import com.inspiredandroid.kai.ui.DenebType
import com.inspiredandroid.kai.ui.LightColorScheme
import com.inspiredandroid.kai.ui.denebHint
import com.inspiredandroid.kai.ui.chat.WorkFeedAction
import com.inspiredandroid.kai.ui.chat.WorkFeedItem
import com.inspiredandroid.kai.ui.chat.composables.DenebDrawerSheet
import com.inspiredandroid.kai.ui.chat.composables.WorkFeedPanel
import com.inspiredandroid.kai.ui.dynamicui.ChartNode
import com.inspiredandroid.kai.ui.dynamicui.KaiUiRenderer
import kotlinx.collections.immutable.persistentListOf
import org.jetbrains.skia.EncodedImageFormat
import java.io.File
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.ui.Alignment
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.PathParser
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.sp
import androidx.compose.material3.HorizontalDivider
import com.inspiredandroid.kai.ui.denebHairline
import kotlinx.datetime.LocalDate
import kotlinx.datetime.TimeZone

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
    renderAnalysis("analysis_clip.png", DarkColorScheme)
    renderChrome("chrome_dark.png", DarkColorScheme)
    renderChrome("chrome_light.png", LightColorScheme)
    renderDesignSample("design_dark.png", DarkColorScheme)
    renderDesignSample("design_light.png", LightColorScheme)
    renderCalendarEvent("calendar_event_dark.png", DarkColorScheme)
    renderCalendarEvent("calendar_event_light.png", LightColorScheme)
    renderCalendarMonth("calendar_month_dark.png", DarkColorScheme)
    renderCalendarMonth("calendar_month_light.png", LightColorScheme)
    renderCalendarAdd("calendar_add_dark.png", DarkColorScheme)
    renderCalendarAdd("calendar_add_light.png", LightColorScheme)
    renderChart("chart_dark.png", DarkColorScheme)
    renderChart("chart_light.png", LightColorScheme)
    renderWorkFeed("workfeed_dark.png", DarkColorScheme)
    renderWorkFeed("workfeed_light.png", LightColorScheme)
    renderWidget("widget_loaded.png", "6/3 14:00 · 기획조정실 주간 회의 3분기 점검", "김민준 부장 · 회의 자료 검토 부탁드립니다", "미읽음 3")
    renderWidget("widget_loading.png", "불러오는 중…", "", "")
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
                    CalendarEventContent(ev = sampleEvent, isLocal = true)
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

// Validates the new month-grid calendar body at phone width (412dp): the grid
// (dots on days with events, today + selected highlighted) and the selected
// day's event list. The weekday header is inlined here for column context.
private fun renderCalendarMonth(name: String, scheme: ColorScheme) {
    val month = CalMonth(2026, 6)
    val grid = buildMonthGrid(month)
    val today = LocalDate(2026, 6, 8)
    val selected = LocalDate(2026, 6, 3)
    val tz = TimeZone.UTC
    // Mix single-day and multi-day events; the latter exercise the ribbon lanes,
    // including one span (e4) that crosses a week boundary.
    val events = listOf(
        CalendarEvent("e1", "기획조정실 주간 회의", "본사 3층 대회의실", "2026-06-03T05:00:00Z", "2026-06-03T06:00:00Z", false),
        CalendarEvent("e2", "에코프로 구매팀 미팅", "남도에코에너지", "2026-06-03T07:30:00Z", "2026-06-03T08:30:00Z", false),
        CalendarEvent("e3", "출장 (서울)", "", "2026-06-10T00:00:00Z", "2026-06-13T00:00:00Z", true),
        CalendarEvent("e4", "RE100 전시 부스", "코엑스", "2026-06-19T00:00:00Z", "2026-06-24T00:00:00Z", true),
    )
    val bars = layoutMonthBars(events, grid, tz)
    val dayEvents = events.filter { selected in eventDays(it.start, it.end, it.allDay, tz) }
    val scene = ImageComposeScene(width = 824, height = 1280, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "일정", onBack = {}) {
                Column(Modifier.padding(horizontal = 16.dp)) {
                    Text(
                        "${month.year}년 ${month.month}월",
                        style = DenebType.subject,
                        color = MaterialTheme.colorScheme.onBackground,
                    )
                    Spacer(Modifier.height(8.dp))
                    Row(Modifier.fillMaxWidth()) {
                        koreanDayOfWeek.forEach { d ->
                            Text(
                                d,
                                style = DenebType.meta,
                                color = denebHint(),
                                textAlign = TextAlign.Center,
                                modifier = Modifier.weight(1f).padding(vertical = 4.dp),
                            )
                        }
                    }
                    CalendarMonthGrid(grid, today, selected, bars, {})
                    Spacer(Modifier.height(12.dp))
                    HorizontalDivider(color = denebHairline())
                    Spacer(Modifier.height(8.dp))
                    Text("6월 3일 (수) · ${dayEvents.size}건", style = DenebType.sectionLabel, color = MaterialTheme.colorScheme.primary)
                    CalendarDayList(dayEvents, selected, tz, {})
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

// Validates the manual add-event form: Material inputs (text fields, all-day
// switch, date/time picker buttons) under Deneb section labels.
private fun renderCalendarAdd(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 824, height = 1300, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "일정 추가", onBack = {}) {
                Column(Modifier.padding(horizontal = 24.dp)) {
                    CalendarAddContent(
                        title = "남도에코 모듈 입고 점검",
                        onTitle = {},
                        allDay = false,
                        onAllDay = {},
                        startDateLabel = "2026년 6월 10일 (수)",
                        onPickStartDate = {},
                        endDateLabel = "2026년 6월 10일 (수)",
                        onPickEndDate = {},
                        startLabel = "14:00",
                        onPickStart = {},
                        endLabel = "15:00",
                        onPickEnd = {},
                        location = "본사 3층",
                        onLocation = {},
                        description = "모듈 입고 수량 확인 및 검수 일정 조율",
                        onDescription = {},
                        error = null,
                        saving = false,
                        saveLabel = "추가",
                        onSave = {},
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

// Reproduces the work-feed analysis answer (long prose paragraph + 2-col 라벨|내용
// table) at exactly phone width (412dp = 824px @ density 2). If the prose/table
// clip here, the bug is in the markdown component itself; if they wrap cleanly,
// the clip is the native-app window/LazyColumn measurement (Android is fine).
private fun renderAnalysis(name: String, scheme: ColorScheme) {
    val analysisSample = """
        ## 사람과 조직
        | 구분 | 내용 |
        |:---|:---|
        | **발신** | 탑솔라 고건 대리(기획조정실) — 이전에도 대한전선 2차 사업 물량산출 자료를 동일 수신자에게 발송한 바 있음 |
        | **수신** | gocharge89@taihan.com — 태한(태양광 EPC 협력사) 담당자. 동일인의 다른 계정인지 불명 |

        **신호**: CC에 오선택 전무(남도에코에너지 대표 겸직)가 포함된 것은 통상적이나, 김대희·김유영은 이전 5/12 물량산출 메일에서도 CC였던 동일 인물. 에스컬레이션이나 담당자 교체 징후는 없다.
    """.trimIndent()
    val scene = ImageComposeScene(width = 824, height = 1400, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Box(Modifier.width(412.dp)) {
                    MarkdownContent(analysisSample, modifier = Modifier.fillMaxWidth().padding(16.dp))
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

// --- Home widget mirror ---
// The Android home widget is RemoteViews (androidApp/deneb_widget.xml +
// DenebWidgetProvider.render). Paparazzi's layoutlib has no Linux-aarch64 native
// binary, so it can't render on this host; instead we reproduce the widget's
// exact layout, colors, and Material glyph paths in Compose/Skia. Keep these in
// sync with deneb_widget.xml whenever the widget changes.
private const val WIDGET_CAL_PATH =
    "M19,3h-1V1h-2v2H8V1H6v2H5C3.89,3 3,3.9 3,5v14c0,1.1 0.89,2 2,2h14c1.1,0 2,-0.9 2,-2V5C21,3.9 20.1,3 19,3zM19,19H5V8h14V19z"
private const val WIDGET_MAIL_PATH =
    "M20,4H4c-1.1,0 -1.99,0.9 -1.99,2L2,18c0,1.1 0.9,2 2,2h16c1.1,0 2,-0.9 2,-2V6c0,-1.1 -0.9,-2 -2,-2zM20,8l-8,5 -8,-5V6l8,5 8,-5v2z"

private fun widgetGlyph(pathData: String): ImageVector =
    ImageVector.Builder(
        defaultWidth = 24.dp,
        defaultHeight = 24.dp,
        viewportWidth = 24f,
        viewportHeight = 24f,
    ).apply {
        addPath(PathParser().parsePathString(pathData).toNodes(), fill = SolidColor(Color.White))
    }.build()

private fun renderWidget(name: String, meeting: String, latestMail: String, unread: String) {
    val homeBg = Color(0xFF0B0B12)
    val cardBg = Color(0xFF1A1B26)
    val accent = Color(0xFF7AA2F7)
    val titleColor = Color(0xFFE8EAF0)
    val mailColor = Color(0xFFD5D8E5)
    val subColor = Color(0xFFA9B1D6)
    val scene = ImageComposeScene(width = 640, height = 420, density = Density(2f)) {
        Box(Modifier.fillMaxSize().background(homeBg).padding(24.dp)) {
            Column(
                Modifier.fillMaxWidth()
                    .clip(RoundedCornerShape(20.dp))
                    .background(cardBg)
                    .padding(16.dp),
            ) {
                Text("Deneb", color = accent, fontSize = 12.sp, fontWeight = FontWeight.Bold)
                Spacer(Modifier.height(10.dp))
                Row {
                    Icon(widgetGlyph(WIDGET_CAL_PATH), null, Modifier.padding(top = 2.dp).size(16.dp), tint = accent)
                    Spacer(Modifier.width(8.dp))
                    Text(
                        meeting,
                        color = titleColor,
                        fontSize = 15.sp,
                        fontWeight = FontWeight.Bold,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                if (latestMail.isNotEmpty()) {
                    Spacer(Modifier.height(10.dp))
                    Row {
                        Icon(widgetGlyph(WIDGET_MAIL_PATH), null, Modifier.padding(top = 1.dp).size(14.dp), tint = subColor)
                        Spacer(Modifier.width(8.dp))
                        Column {
                            Text(
                                latestMail,
                                color = mailColor,
                                fontSize = 13.sp,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                            if (unread.isNotEmpty()) {
                                Spacer(Modifier.height(1.dp))
                                Text(unread, color = subColor, fontSize = 11.sp)
                            }
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

// Validates the decluttered work-feed bottom sheet: clean rows (title + relative
// time + 2-line summary) with trailing icon-only quick actions instead of a wall
// of labeled buttons. Renders several mock items at phone width.
private val sampleFeed = persistentListOf(
    WorkFeedItem(
        id = "wf1",
        source = "proactive",
        title = "업무 리포트",
        summary = "🔴 긴급: 에코프로 모듈 견적 회신 기한이 오늘까지입니다. 김민준 부장 확인 필요.",
        status = "unread",
        actions = listOf(
            WorkFeedAction("open", "open", "열기"),
            WorkFeedAction("followup", "followup", "후속 정리"),
            WorkFeedAction("snooze", "snooze", "나중에"),
            WorkFeedAction("ack", "ack", "완료"),
        ),
        createdAtMs = System.currentTimeMillis() - 15 * 60_000L,
    ),
    WorkFeedItem(
        id = "wf2",
        source = "capture_audio",
        title = "공유 녹음",
        summary = "기획조정실 주간 회의 — RE100 고객사 계약 진행률, 주차장 태양광 견적 리뷰를 논의했습니다.",
        status = "unread",
        actions = listOf(
            WorkFeedAction("open", "open", "열기"),
            WorkFeedAction("followup", "followup", "액션 정리"),
            WorkFeedAction("snooze", "snooze", "나중에"),
            WorkFeedAction("ack", "ack", "완료"),
        ),
        createdAtMs = System.currentTimeMillis() - 3 * 3_600_000L,
    ),
    WorkFeedItem(
        id = "wf3",
        source = "proactive",
        title = "업무 리포트",
        summary = "GitHub PR #1853 main 병합 완료 — 추가 조치 불필요.",
        status = "unread",
        actions = listOf(
            WorkFeedAction("open", "open", "열기"),
            WorkFeedAction("snooze", "snooze", "나중에"),
            WorkFeedAction("ack", "ack", "완료"),
        ),
        createdAtMs = System.currentTimeMillis() - 26 * 3_600_000L,
    ),
)

private fun renderWorkFeed(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 824, height = 980, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Box(Modifier.width(412.dp)) {
                    WorkFeedPanel(
                        items = sampleFeed,
                        onOpen = {},
                        onRunAction = { _, _ -> },
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
