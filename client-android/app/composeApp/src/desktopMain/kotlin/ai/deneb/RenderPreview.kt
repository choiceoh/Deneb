@file:OptIn(androidx.compose.ui.ExperimentalComposeUiApi::class)

package ai.deneb

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
import ai.deneb.deneb.CalMonth
import ai.deneb.deneb.CalendarAddContent
import ai.deneb.deneb.CronEditContent
import ai.deneb.deneb.IntervalUnit
import ai.deneb.deneb.SchedMode
import ai.deneb.deneb.ScheduleDraft
import ai.deneb.deneb.CalendarDayList
import ai.deneb.deneb.CalendarEmptyDay
import ai.deneb.deneb.CalendarEvent
import ai.deneb.deneb.CalendarEventContent
import ai.deneb.deneb.CalendarEventDetail
import ai.deneb.deneb.CalendarMonthGrid
import ai.deneb.deneb.DenebMarkdown
import ai.deneb.deneb.buildMonthGrid
import ai.deneb.deneb.eventDays
import ai.deneb.deneb.koreanDayOfWeek
import ai.deneb.deneb.layoutMonthBars
import ai.deneb.deneb.MailMessage
import ai.deneb.deneb.MailRow
import ai.deneb.deneb.timedSingleDayDots
import ai.deneb.deneb.Todo
import ai.deneb.deneb.TodoAddContent
import ai.deneb.deneb.TodoListContent
import ai.deneb.ui.markdown.MarkdownContent
import ai.deneb.ui.DarkColorScheme
import ai.deneb.ui.DenebRow
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.LightColorScheme
import ai.deneb.ui.denebHint
import ai.deneb.ui.chat.WorkFeedAction
import ai.deneb.ui.chat.WorkFeedItem
import ai.deneb.ui.chat.composables.DenebDrawerSheet
import ai.deneb.ui.chat.composables.WaitingResponseRow
import ai.deneb.ui.chat.composables.WorkFeedPanel
import ai.deneb.ui.components.SkeletonList
import ai.deneb.ui.dynamicui.ChartNode
import ai.deneb.ui.dynamicui.DenebUiRenderer
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
import ai.deneb.ui.denebHairline
import kotlinx.datetime.LocalDate
import kotlinx.datetime.TimeZone

// Off-screen render harness: renders Deneb composables to PNG via Skia so the
// look (and bugs like invisible text) can be inspected without building +
// installing the APK. Run with `./gradlew :composeApp:renderPreviews`.

private val sample = listOf(
    MailMessage("1", "김철수 <kim@topsolar.kr>", "내일 회의 자료 확인 부탁드립니다", "안녕하세요, 첨부한 자료 검토 후 회신 부탁드립니다.", "2026-05-31T09:12:00Z", true, priority = "urgent", priorityHint = "마감 표현 · 회의"),
    MailMessage("2", "GitHub <noreply@github.com>", "[deneb] PR #1814 merged", "Your pull request was merged into main.", "2026-05-31T08:40:00Z", false),
    MailMessage("4", "박영업 <park@vendor.co.kr>", "모듈 견적서 송부 — 1,950매", "견적 금액은 첨부 파일을 참조 부탁드립니다.", "2026-05-31T07:30:00Z", false, priority = "attention", priorityHint = "견적 · 금액"),
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
    renderCalendarEvent("calendar_event_multiday_light.png", LightColorScheme, sampleSpanEvent)
    renderCalendarMonth("calendar_month_dark.png", DarkColorScheme)
    renderCalendarMonth("calendar_month_light.png", LightColorScheme)
    renderCalendarAdd("calendar_add_dark.png", DarkColorScheme)
    renderCalendarAdd("calendar_add_light.png", LightColorScheme)
    renderCalendarEmpty("calendar_empty_dark.png", DarkColorScheme)
    renderCalendarEmpty("calendar_empty_light.png", LightColorScheme)
    renderTodoList("todo_list_dark.png", DarkColorScheme)
    renderTodoList("todo_list_light.png", LightColorScheme)
    renderTodoAdd("todo_add_dark.png", DarkColorScheme)
    renderTodoAdd("todo_add_light.png", LightColorScheme)
    renderCronEdit("cron_edit_dark.png", DarkColorScheme, cronWeeklyDraft, "Asia/Seoul")
    renderCronEdit("cron_edit_light.png", LightColorScheme, cronWeeklyDraft, "Asia/Seoul")
    renderCronEdit("cron_edit_interval.png", DarkColorScheme, cronIntervalDraft, "")
    renderCronEdit("cron_edit_advanced.png", DarkColorScheme, cronAdvancedDraft, "Asia/Seoul")
    renderChart("chart_dark.png", DarkColorScheme)
    renderChart("chart_light.png", LightColorScheme)
    renderWorkFeed("workfeed_dark.png", DarkColorScheme)
    renderWorkFeed("workfeed_light.png", LightColorScheme)
    renderWidget("widget_loaded.png", "6/3 14:00 · 기획조정실 주간 회의 3분기 점검", "김민준 부장 · 회의 자료 검토 부탁드립니다", "미읽음 3")
    renderWidget("widget_loading.png", "불러오는 중…", "", "")
    renderSkeleton("skeleton_dark.png", DarkColorScheme)
    renderSkeleton("skeleton_light.png", LightColorScheme)
    renderWaitingChip("waiting_chip_dark.png", DarkColorScheme)
    renderWaitingChip("waiting_chip_light.png", LightColorScheme)
    println("rendered -> /tmp/deneb-render/")
}

private val sampleMail = listOf(
    Triple("김민준 부장", "내일 회의 자료 검토 부탁드립니다", true),
    Triple("GitHub", "[deneb] PR #1853 merged into main", false),
    Triple("에코프로 구매팀", "모듈 견적 회신 요청 — 6월말 납기", false),
    Triple("이서연", "(제목 없음)", false),
)

// Validates the chat waiting chip in its live-progress states: generic rotating
// text (no info yet), a gateway tool label fed by TurnProgress ("메일 확인 중",
// status-only), and the thinking status. The multi-tool count (tools_count) and
// the elapsed-time suffix don't render in a single-shot scene — the former's
// format-args stringResource never resolves here, the latter needs 10s of wall
// clock — so those are exercised by compile + live runs instead.
private fun renderWaitingChip(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 760, height = 380, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = scheme.background) {
                Column(Modifier.fillMaxSize().padding(8.dp)) {
                    WaitingResponseRow(executingTools = persistentListOf())
                    WaitingResponseRow(
                        executingTools = persistentListOf("t1" to "메일 확인 중"),
                        isStatusOnly = true,
                    )
                    WaitingResponseRow(
                        executingTools = persistentListOf("t1" to "깊이 생각 중…"),
                        isStatusOnly = true,
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

// A multi-day timed event, so the detail's whenLabel shows the span end day too.
private val sampleSpanEvent = CalendarEventDetail(
    id = "e2",
    title = "동계 워크숍 — 1박 2일 전략 세션",
    description = "1일차 RE100 로드맵\n2일차 루프탑·주차장 사업 점검",
    location = "양양 연수원",
    start = "2026-06-03T05:00:00Z",
    end = "2026-06-04T08:00:00Z",
    allDay = false,
    organizer = "오선택 전무",
    attendees = listOf("기획조정실 전원"),
    status = "confirmed",
)

// Validates the calendar-event detail in the hybrid idiom: Deneb type skin
// (subject + section labels + body) with the Meet join as a Material button.
private fun renderCalendarEvent(name: String, scheme: ColorScheme, ev: CalendarEventDetail = sampleEvent) {
    val scene = ImageComposeScene(width = 760, height = 1100, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "일정", onBack = {}) {
                Column(Modifier.padding(horizontal = 24.dp)) {
                    CalendarEventContent(ev = ev, isLocal = true)
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
    val dots = timedSingleDayDots(events, tz)
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
                    CalendarMonthGrid(grid, today, selected, bars, dots, {})
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

// Validates the empty-day state: a quiet line plus the inline "add to this day" CTA.
private fun renderCalendarEmpty(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 824, height = 520, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "일정", onBack = {}) {
                Column(Modifier.padding(horizontal = 16.dp)) {
                    Text("6월 9일 (화)", style = DenebType.sectionLabel, color = MaterialTheme.colorScheme.primary)
                    Spacer(Modifier.height(4.dp))
                    CalendarEmptyDay(onAdd = {})
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

private val sampleTodos = listOf(
    Todo("todo:1", "남도에코 모듈 견적 회신", note = "6월말 납기 확인", due = "2026-06-09T00:00:00Z", dueAllDay = true),
    Todo("todo:2", "RE100 계약서 검토", due = "2026-06-10T05:00:00Z"),
    Todo("todo:3", "법인카드 정산", note = "5월분"),
    Todo("todo:4", "주간 보고 작성", due = "2026-06-08T00:00:00Z", dueAllDay = true, done = true),
)

// Validates the to-do list: active items (Material checkbox + struck-through done)
// under "할 일", completed under "완료", in the Deneb row idiom.
private fun renderTodoList(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 824, height = 760, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "할 일", onBack = {}) {
                Column(Modifier.padding(horizontal = 24.dp)) {
                    TodoListContent(sampleTodos, onToggle = { _, _ -> }, onOpen = {})
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

// Validates the add/edit-to-do form: Material inputs (title, note, due switches +
// date/time picker buttons) under Deneb section labels.
private fun renderTodoAdd(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 824, height = 980, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "할 일 추가", onBack = {}) {
                Column(Modifier.padding(horizontal = 24.dp)) {
                    TodoAddContent(
                        title = "남도에코 모듈 견적 회신",
                        onTitle = {},
                        note = "6월말 납기 확인",
                        onNote = {},
                        hasDue = true,
                        onHasDue = {},
                        allDay = false,
                        onAllDay = {},
                        dueDateLabel = "2026년 6월 10일 (수)",
                        onPickDate = {},
                        dueTimeLabel = "14:00",
                        onPickTime = {},
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
                        multiDay = false,
                        onMultiDay = {},
                        startDateLabel = "2026년 6월 10일 (수)",
                        onPickStartDate = {},
                        endDateLabel = "2026년 6월 11일 (목)",
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

// Sample drafts for the cron edit previews — one per schedule mode so the segmented
// control, weekday chips, interval row, and raw-cron fallback all get exercised.
private val cronWeeklyDraft = ScheduleDraft(SchedMode.WEEKLY, "08:00", setOf(1, 3, 5), "30", IntervalUnit.MIN, LocalDate.parse("2026-06-13"), "")
private val cronIntervalDraft = ScheduleDraft(SchedMode.INTERVAL, "09:00", emptySet(), "15", IntervalUnit.MIN, LocalDate.parse("2026-06-13"), "")
private val cronAdvancedDraft = ScheduleDraft(SchedMode.ADVANCED, "09:00", emptySet(), "30", IntervalUnit.MIN, LocalDate.parse("2026-06-13"), "*/5 8-22 * * 1-6")

// Validates the cron edit form: soft filled fields, the frequency segmented control,
// and the per-mode inputs (weekday chips / time / interval / raw-cron) under Deneb
// section labels. Driven per schedule mode via [draft].
private fun renderCronEdit(name: String, scheme: ColorScheme, draft: ScheduleDraft, tz: String) {
    val scene = ImageComposeScene(width = 824, height = 1300, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "크론 편집", onBack = {}) {
                Column(Modifier.padding(horizontal = 24.dp)) {
                    CronEditContent(
                        name = "주간 업무 보고",
                        onName = {},
                        draft = draft,
                        onDraft = {},
                        onceDateLabel = "2026년 6월 13일",
                        onPickOnceDate = {},
                        tz = tz,
                        onTz = {},
                        prompt = "이번 주 진행 상황과 미결 항목을 정리해 보고해 줘.",
                        onPrompt = {},
                        model = "",
                        onModel = {},
                        error = null,
                        saving = false,
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
                    DenebUiRenderer(
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
                    DenebUiRenderer(
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
        title = "📧 JOCA Cable 최신 메일 분석 보고",
        summary = "발신 fred@jocacable.com — 2800km solar cable 대량 발주 가격 제안, 발주 수량·시점 회신 요청.",
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
        source = "proactive",
        title = "분석 — 왜 지금 왔는가",
        summary = "무림 울산공장 풍력 사업의 첫 검토안 제출 — 박종원 부장이 외부 업체 제안 자료를 전달.",
        status = "unread",
        actions = listOf(
            WorkFeedAction("open", "open", "열기"),
            WorkFeedAction("followup", "followup", "후속 정리"),
            WorkFeedAction("snooze", "snooze", "나중에"),
            WorkFeedAction("ack", "ack", "완료"),
        ),
        createdAtMs = System.currentTimeMillis() - 40 * 60_000L,
    ),
    WorkFeedItem(
        id = "wf3",
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
        id = "wf4",
        source = "capture_image",
        title = "공유 이미지",
        summary = "현대차 울산 견적서 OCR — 합계 ₩2,800,000, 납기 6/20, 결제 조건 30일.",
        status = "unread",
        actions = listOf(
            WorkFeedAction("open", "open", "열기"),
            WorkFeedAction("followup", "followup", "문서화"),
            WorkFeedAction("ack", "ack", "완료"),
        ),
        createdAtMs = System.currentTimeMillis() - 5 * 3_600_000L,
    ),
    WorkFeedItem(
        id = "wf5",
        source = "proactive",
        title = "📋 주간업무보고 — 기획조정실",
        summary = "이번 주 사업개발 3건·모듈 견적 5건 발송, 루프탑 2건 계약 임박.",
        status = "acked",
        actions = listOf(
            WorkFeedAction("open", "open", "열기"),
            WorkFeedAction("ack", "ack", "완료"),
        ),
        createdAtMs = System.currentTimeMillis() - 26 * 3_600_000L,
    ),
)

private fun renderWorkFeed(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 824, height = 1100, density = Density(2f)) {
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

// Validates the loading skeleton (sweeping-shimmer placeholders). A static capture
// shows the base tint at rest; the highlight band only appears mid-sweep, so this
// mainly guards that the placeholder reads as visible (not a blank screen) and that
// the draw-phase shimmer doesn't crash.
private fun renderSkeleton(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 824, height = 700, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                SkeletonList(rows = 6)
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
