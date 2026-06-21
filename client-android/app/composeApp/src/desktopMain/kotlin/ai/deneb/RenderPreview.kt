@file:OptIn(androidx.compose.ui.ExperimentalComposeUiApi::class)

package ai.deneb

import ai.deneb.deneb.AppTilesContent
import ai.deneb.deneb.CalMonth
import ai.deneb.deneb.CalendarAddContent
import ai.deneb.deneb.CalendarDayList
import ai.deneb.deneb.CalendarEmptyDay
import ai.deneb.deneb.CalendarEvent
import ai.deneb.deneb.CalendarEventContent
import ai.deneb.deneb.CalendarEventDetail
import ai.deneb.deneb.CalendarMonthGrid
import ai.deneb.deneb.ContactsList
import ai.deneb.deneb.CronEditContent
import ai.deneb.deneb.DashboardLanesContent
import ai.deneb.deneb.DealNotebookLinkRow
import ai.deneb.deneb.DenebBrowserChrome
import ai.deneb.deneb.DenebMoreScreen
import ai.deneb.deneb.DenebWebViewState
import ai.deneb.deneb.FilesSearchMode
import ai.deneb.deneb.FilesSearchModeRow
import ai.deneb.deneb.FilesTextViewerContent
import ai.deneb.deneb.IntervalUnit
import ai.deneb.deneb.MailMessage
import ai.deneb.deneb.MailRow
import ai.deneb.deneb.OrgChartContent
import ai.deneb.deneb.OrgNodeEditor
import ai.deneb.deneb.PersonHit
import ai.deneb.deneb.PromptStyleEditor
import ai.deneb.deneb.SchedMode
import ai.deneb.deneb.ScheduleDraft
import ai.deneb.deneb.SearchContent
import ai.deneb.deneb.SearchHit
import ai.deneb.deneb.SearchResults
import ai.deneb.deneb.SelfImprovementCodingContent
import ai.deneb.deneb.SkillDetailContent
import ai.deneb.deneb.SkillLifecycleContent
import ai.deneb.deneb.SkillLifecycleRow
import ai.deneb.deneb.SkillListContent
import ai.deneb.deneb.SkillsViewSwitcher
import ai.deneb.deneb.Todo
import ai.deneb.deneb.TodoAddContent
import ai.deneb.deneb.TodoListContent
import ai.deneb.deneb.buildMonthGrid
import ai.deneb.deneb.eventDays
import ai.deneb.deneb.generated.ContactRow
import ai.deneb.deneb.generated.DashboardItem
import ai.deneb.deneb.generated.LaneOut
import ai.deneb.deneb.generated.MemberOut
import ai.deneb.deneb.generated.OrgNodeOut
import ai.deneb.deneb.generated.SelfCorrectionCandidate
import ai.deneb.deneb.generated.SelfImprovementCodingListResponse
import ai.deneb.deneb.generated.SelfImprovementCodingStatusCount
import ai.deneb.deneb.generated.SkillDetailResponse
import ai.deneb.deneb.generated.SkillLifecycleEvent
import ai.deneb.deneb.generated.SkillRow
import ai.deneb.deneb.koreanDayOfWeek
import ai.deneb.deneb.layoutMonthBars
import ai.deneb.deneb.timedSingleDayDots
import ai.deneb.ui.DarkColorScheme
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebListRow
import ai.deneb.ui.DenebRow
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.LightColorScheme
import ai.deneb.ui.chat.WorkFeedAction
import ai.deneb.ui.chat.WorkFeedItem
import ai.deneb.ui.chat.composables.DenebBottomBar
import ai.deneb.ui.chat.composables.EmptyState
import ai.deneb.ui.chat.composables.WaitingResponseRow
import ai.deneb.ui.chat.composables.WorkFeedPanel
import ai.deneb.ui.components.DenebUnderlineSearchField
import ai.deneb.ui.components.SectionedScrubList
import ai.deneb.ui.components.SkeletonList
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import ai.deneb.ui.denebInsightContainer
import ai.deneb.ui.dynamicui.ChartNode
import ai.deneb.ui.dynamicui.DenebUiRenderer
import ai.deneb.ui.markdown.MarkdownContent
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Extension
import androidx.compose.material.icons.outlined.Memory
import androidx.compose.material.icons.outlined.Palette
import androidx.compose.material.icons.outlined.Restore
import androidx.compose.material.icons.outlined.Save
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material3.Checkbox
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.ImageComposeScene
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.PathParser
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.collections.immutable.persistentListOf
import kotlinx.datetime.LocalDate
import kotlinx.datetime.TimeZone
import org.jetbrains.skia.EncodedImageFormat
import java.io.File

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

// Plain (non-markdown) sample for the monospace branch of the files text viewer.
private val filesPlainSample = """
    2026-06-20T09:12:01Z INFO  gateway 시작 (port=18789)
    2026-06-20T09:12:02Z INFO  provider wormhole 연결됨
    2026-06-20T09:12:03Z WARN  prefix_cache 미적중 (cold start)
    2026-06-20T09:12:08Z INFO  miniapp.files.list ok (entries=14)
""".trimIndent()

private fun renderBrowser(name: String, scheme: ColorScheme) {
    val state = DenebWebViewState("https://en.wikipedia.org/wiki/Deneb").apply { translateEnabled = true }
    val scene = ImageComposeScene(width = 824, height = 900, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            DenebBrowserChrome(state = state, onBack = {}) {
                Box(Modifier.fillMaxWidth().weight(1f), contentAlignment = Alignment.Center) {
                    Text("(웹 페이지 — Android WebView)", style = DenebType.meta, color = denebHint())
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

private fun renderMore(name: String, scheme: ColorScheme, hidden: Set<String> = emptySet()) {
    val scene = ImageComposeScene(width = 824, height = 1500, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                DenebMoreScreen(onBack = {}, onOpen = {}, hiddenTiles = hidden)
            }
        }
    }
    val image = scene.render()
    val data = image.encodeToData(EncodedImageFormat.PNG) ?: error("PNG encode failed")
    File("/tmp/deneb-render").mkdirs()
    File("/tmp/deneb-render/$name").writeBytes(data.bytes)
    scene.close()
}

// The 설정 → "더보기 표시 항목" toggle section (stateless body). Renders with two tiles
// pre-hidden so the OFF (숨김) switch state is visible alongside the ON (표시) state.
private fun renderAppTiles(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 824, height = 1200, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                AppTilesContent(hidden = setOf("deneb_search", "deneb_browser"), onToggle = { _, _ -> })
            }
        }
    }
    val image = scene.render()
    val data = image.encodeToData(EncodedImageFormat.PNG) ?: error("PNG encode failed")
    File("/tmp/deneb-render").mkdirs()
    File("/tmp/deneb-render/$name").writeBytes(data.bytes)
    scene.close()
}

fun main() {
    System.setProperty("java.awt.headless", "true")
    renderScreen("mail_dark.png", "mail", DarkColorScheme, 840, 1100)
    renderScreen("mail_light.png", "mail", LightColorScheme, 840, 1100)
    renderBrowser("browser_dark.png", DarkColorScheme)
    renderBrowser("browser_light.png", LightColorScheme)
    renderMore("more_dark.png", DarkColorScheme)
    renderMore("more_light.png", LightColorScheme)
    // 더보기 숨김: the grid with two tiles hidden (검색·브라우저) — verify they drop out.
    renderMore("more_hidden_dark.png", DarkColorScheme, hidden = setOf("deneb_search", "deneb_browser"))
    // The 설정 toggle section that drives it.
    renderAppTiles("app_tiles_dark.png", DarkColorScheme)
    renderAppTiles("app_tiles_light.png", LightColorScheme)
    renderMarkdown("markdown_dark.png", DarkColorScheme)
    renderScreen("scrub_active_dark.png", "scrub_active", DarkColorScheme, 824, 1100)
    renderScreen("scrub_active_light.png", "scrub_active", LightColorScheme, 824, 1100)
    renderScreen("contacts_dark.png", "contacts", DarkColorScheme, 824, 1100)
    renderScreen("contacts_light.png", "contacts", LightColorScheme, 824, 1100)
    renderAnalysis("analysis_clip.png", DarkColorScheme)
    renderCollapsedReport("mail_collapsed_dark.png", DarkColorScheme, expanded = false)
    renderCollapsedReport("mail_collapsed_light.png", LightColorScheme, expanded = false)
    renderCollapsedReport("mail_expanded_dark.png", DarkColorScheme, expanded = true)
    renderDesignRefresh("design_refresh_dark.png", DarkColorScheme)
    renderDesignRefresh("design_refresh_light.png", LightColorScheme)
    // Five-slot bar: 피드·메일·채팅·달력·더보기. One shot per selectable screen tab
    // (피드/채팅/더보기) so the filled-vs-outlined active glyph is checked; 메일/달력 are
    // navigate-actions (never selected) and show on every shot.
    renderBottomBar("bottombar_feed_dark.png", DarkColorScheme, "deneb_feed")
    renderBottomBar("bottombar_feed_light.png", LightColorScheme, "deneb_feed")
    renderBottomBar("bottombar_chat_dark.png", DarkColorScheme, "home")
    renderBottomBar("bottombar_more_dark.png", DarkColorScheme, "deneb_more")
    // 메일·달력 are now selectable tabs — they highlight when their section is active.
    renderBottomBar("bottombar_mail_dark.png", DarkColorScheme, "deneb_mail")
    // Chat empty/welcome — muted sparkle + greeting (replaced the purple orb).
    renderScreen("chat_empty_dark.png", "chat_empty", DarkColorScheme, 824, 720)
    renderDesignSample("design_dark.png", DarkColorScheme)
    renderDesignSample("design_light.png", LightColorScheme)
    renderScreen("calendar_event_dark.png", "calendar_event", DarkColorScheme, 760, 1100)
    renderScreen("calendar_event_light.png", "calendar_event", LightColorScheme, 760, 1100)
    renderScreen("calendar_event_multiday_light.png", "calendar_event_multiday", LightColorScheme, 760, 1100)
    renderScreen("calendar_month_dark.png", "calendar_month", DarkColorScheme, 824, 1280)
    renderScreen("calendar_month_light.png", "calendar_month", LightColorScheme, 824, 1280)
    renderScreen("calendar_add_dark.png", "calendar_add", DarkColorScheme, 824, 1300)
    renderScreen("calendar_add_light.png", "calendar_add", LightColorScheme, 824, 1300)
    renderScreen("calendar_empty_dark.png", "calendar_empty", DarkColorScheme, 824, 520)
    renderScreen("calendar_empty_light.png", "calendar_empty", LightColorScheme, 824, 520)
    renderScreen("todo_list_dark.png", "todo_list", DarkColorScheme, 824, 760)
    renderScreen("todo_list_light.png", "todo_list", LightColorScheme, 824, 760)
    renderScreen("todo_add_dark.png", "todo_add", DarkColorScheme, 824, 980)
    renderScreen("todo_add_light.png", "todo_add", LightColorScheme, 824, 980)
    renderScreen("cron_edit_dark.png", "cron_edit", DarkColorScheme, 824, 1300)
    renderScreen("cron_edit_light.png", "cron_edit", LightColorScheme, 824, 1300)
    renderScreen("cron_edit_interval.png", "cron_edit_interval", DarkColorScheme, 824, 1300)
    renderScreen("cron_edit_advanced.png", "cron_edit_advanced", DarkColorScheme, 824, 1300)
    renderScreen("prompt_editor_dark.png", "prompt_editor", DarkColorScheme, 824, 980)
    renderScreen("prompt_editor_light.png", "prompt_editor", LightColorScheme, 824, 980)
    renderScreen("topic_doc_editor_dark.png", "topic_doc_editor", DarkColorScheme, 824, 980)
    renderScreen("topic_doc_editor_light.png", "topic_doc_editor", LightColorScheme, 824, 980)
    renderChart("chart_dark.png", DarkColorScheme)
    renderChart("chart_light.png", LightColorScheme)
    renderScreen("workfeed_dark.png", "workfeed", DarkColorScheme, 824, 1100)
    renderScreen("workfeed_light.png", "workfeed", LightColorScheme, 824, 1100)
    renderScreen("dashboard_dark.png", "dashboard", DarkColorScheme, 824, 1900)
    renderScreen("dashboard_light.png", "dashboard", LightColorScheme, 824, 1900)
    renderScreen("org_chart_dark.png", "org_chart", DarkColorScheme, 824, 1500)
    renderScreen("org_chart_light.png", "org_chart", LightColorScheme, 824, 1500)
    renderScreen("org_chart_edit_light.png", "org_chart_edit", LightColorScheme, 824, 1500)
    renderScreen("wiki_notebook_link_light.png", "wiki_notebook_link", LightColorScheme, 824, 560)
    renderScreen("wiki_notebook_link_dark.png", "wiki_notebook_link", DarkColorScheme, 824, 560)
    renderScreen("org_chart_search_dark.png", "org_chart_search", DarkColorScheme, 824, 1500)
    renderScreen("org_editor_dark.png", "org_editor", DarkColorScheme, 824, 1280)
    renderScreen("org_editor_light.png", "org_editor", LightColorScheme, 824, 1280)
    renderWidget("widget_loaded.png", "6/3 14:00 · 기획조정실 주간 회의 3분기 점검", "김민준 부장 · 회의 자료 검토 부탁드립니다", "미읽음 3")
    renderWidget("widget_loading.png", "불러오는 중…", "", "")
    renderSkeleton("skeleton_dark.png", DarkColorScheme)
    renderSkeleton("skeleton_light.png", LightColorScheme)
    renderWaitingChip("waiting_chip_dark.png", DarkColorScheme)
    renderWaitingChip("waiting_chip_light.png", LightColorScheme)
    renderScreen("skills_list_dark.png", "skills_list", DarkColorScheme, 824, 700)
    renderScreen("skills_list_light.png", "skills_list", LightColorScheme, 824, 700)
    renderScreen("self_improvement_coding_dark.png", "self_improvement_coding", DarkColorScheme, 824, 760)
    renderScreen("self_improvement_coding_light.png", "self_improvement_coding", LightColorScheme, 824, 760)
    renderScreen("skills_lifecycle_dark.png", "skills_lifecycle", DarkColorScheme, 824, 700)
    renderScreen("skills_lifecycle_light.png", "skills_lifecycle", LightColorScheme, 824, 700)
    renderScreen("skill_detail_dark.png", "skill_detail", DarkColorScheme, 824, 1400)
    renderScreen("skill_detail_light.png", "skill_detail", LightColorScheme, 824, 1400)
    renderScreen("search_dark.png", "search", DarkColorScheme, 824, 900)
    renderScreen("search_light.png", "search", LightColorScheme, 824, 900)
    renderScreen("search_empty_dark.png", "search_empty", DarkColorScheme, 824, 380)
    renderScreen("search_empty_light.png", "search_empty", LightColorScheme, 824, 380)
    renderScreen("search_field_dark.png", "search_field", DarkColorScheme, 824, 460)
    renderScreen("search_field_light.png", "search_field", LightColorScheme, 824, 460)
    renderScreen("files_text_markdown_dark.png", "files_text_markdown", DarkColorScheme, 824, 900)
    renderScreen("files_text_markdown_light.png", "files_text_markdown", LightColorScheme, 824, 900)
    renderScreen("files_text_plain_dark.png", "files_text_plain", DarkColorScheme, 824, 900)
    renderFilesSearchMode("files_search_mode_name_dark.png", DarkColorScheme, FilesSearchMode.NAME)
    renderFilesSearchMode("files_search_mode_semantic_dark.png", DarkColorScheme, FilesSearchMode.SEMANTIC)
    renderFilesSearchMode("files_search_mode_content_light.png", LightColorScheme, FilesSearchMode.CONTENT)
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
    val scene = ImageComposeScene(width = 760, height = 640, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = scheme.background) {
                Column(Modifier.fillMaxSize().padding(8.dp)) {
                    WaitingResponseRow(executingTools = persistentListOf())
                    WaitingResponseRow(
                        executingTools = persistentListOf("t1" to "메일 확인 중"),
                        isStatusOnly = true,
                    )
                    // Detail hint from the tool input ("tool" frame detail field).
                    WaitingResponseRow(
                        executingTools = persistentListOf("t1" to "메일 확인 중: 아르고에너지 NDA"),
                        isStatusOnly = true,
                    )
                    // Failure form held briefly after an isError completion.
                    WaitingResponseRow(
                        executingTools = persistentListOf("t1" to "웹 검색 실패"),
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

private val sampleTodos = listOf(
    Todo("todo:1", "남도에코 모듈 견적 회신", note = "6월말 납기 확인", due = "2026-06-09T00:00:00Z", dueAllDay = true),
    Todo("todo:2", "RE100 계약서 검토", due = "2026-06-10T05:00:00Z"),
    Todo("todo:3", "법인카드 정산", note = "5월분"),
    Todo("todo:4", "주간 보고 작성", due = "2026-06-08T00:00:00Z", dueAllDay = true, done = true),
)

// ── Shared screen registry ───────────────────────────────────────────────────
// Single source of truth for inspectable screens: name -> body. Reused by the PNG
// renderer (renderScreen, below) AND the headless semantics inspector
// (PreviewInspect.kt / ui-inspect.sh), so the SAME composition projects to pixels
// (vision) or to a text semantics tree (vision-free). Each body is the bare screen
// under its theme; the caller supplies the surface + size. Screens migrate into this
// map incrementally — entries here are inspectable, render-only previews below are not
// yet. The body reuses the same mock data the old per-screen render* functions used.
private val sampleSearch = SearchResults(
    wiki = listOf(
        SearchHit("wiki/projects/re100", "RE100 전환 로드맵", "사업장 재생에너지 100% 전환 단계별 계획과 PPA 검토 …", "프로젝트"),
        SearchHit("wiki/people/kim-minjun", "김민준 부장", "에코프로 구매팀 · 모듈 단가 협상 담당", "인물"),
    ),
    people = listOf(
        PersonHit("이서연 차장", "lee@example.com", 42, "6월 모듈 납기 일정 회신 부탁드립니다"),
    ),
    diary = listOf(
        SearchHit("diary/2026-06-08", "2026-06-08", "남도에코 미팅 메모 — 케이블 물량 재확인, 준공 일정 당김 …", "일기"),
    ),
)

internal val previewScreens: Map<String, @Composable (ColorScheme) -> Unit> = mapOf(
    "search" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "검색", onBack = {}) {
                SearchContent(
                    modifier = Modifier.weight(1f),
                    query = "RE100",
                    onQueryChange = {},
                    onSearch = {},
                    searching = false,
                    failed = false,
                    results = sampleSearch,
                    onOpenWiki = {},
                    onOpenPerson = {},
                    onOpenCategories = {},
                )
            }
        }
    },
    "search_empty" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "검색", onBack = {}) {
                SearchContent(
                    modifier = Modifier.weight(1f),
                    query = "",
                    onQueryChange = {},
                    onSearch = {},
                    searching = false,
                    failed = false,
                    results = null,
                    onOpenWiki = {},
                    onOpenPerson = {},
                    onOpenCategories = {},
                )
            }
        }
    },
    "search_field" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "검색 필드", onBack = {}) {
                Column(Modifier.padding(horizontal = 24.dp)) {
                    Spacer(Modifier.height(12.dp))
                    DenebUnderlineSearchField(
                        query = "",
                        onQueryChange = {},
                        placeholder = "위키 · 일기 · 사람",
                        onSearch = {},
                    )
                    Spacer(Modifier.height(28.dp))
                    DenebUnderlineSearchField(
                        query = "qwen3",
                        onQueryChange = {},
                        placeholder = "HuggingFace 모델 검색",
                        textStyle = DenebType.body,
                        clearable = true,
                    )
                }
            }
        }
    },
    "calendar_event" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "일정", onBack = {}) {
                Column(Modifier.padding(horizontal = 24.dp)) {
                    CalendarEventContent(ev = sampleEvent, isLocal = true)
                }
            }
        }
    },
    "calendar_event_multiday" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "일정", onBack = {}) {
                Column(Modifier.padding(horizontal = 24.dp)) {
                    CalendarEventContent(ev = sampleSpanEvent, isLocal = true)
                }
            }
        }
    },
    "calendar_empty" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "일정", onBack = {}) {
                Column(Modifier.padding(horizontal = 16.dp)) {
                    Text("6월 9일 (화)", style = DenebType.sectionLabel, color = MaterialTheme.colorScheme.primary)
                    Spacer(Modifier.height(4.dp))
                    CalendarEmptyDay(onAdd = {})
                }
            }
        }
    },
    "todo_list" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "할 일", onBack = {}) {
                Column(Modifier.padding(horizontal = 24.dp)) {
                    TodoListContent(sampleTodos, onToggle = { _, _ -> }, onOpen = {})
                }
            }
        }
    },
    "calendar_month" to { scheme ->
        val month = CalMonth(2026, 6)
        val grid = buildMonthGrid(month)
        val today = LocalDate(2026, 6, 8)
        val selected = LocalDate(2026, 6, 3)
        val tz = TimeZone.UTC
        val events = listOf(
            CalendarEvent("e1", "기획조정실 주간 회의", "본사 3층 대회의실", "2026-06-03T05:00:00Z", "2026-06-03T06:00:00Z", false, category = "mine"),
            CalendarEvent("e2", "에코프로 구매팀 미팅", "남도에코에너지", "2026-06-03T07:30:00Z", "2026-06-03T08:30:00Z", false, category = "others"),
            CalendarEvent("e3", "출장 (서울)", "", "2026-06-10T00:00:00Z", "2026-06-13T00:00:00Z", true, category = "mine"),
            CalendarEvent("e4", "RE100 전시 부스", "코엑스", "2026-06-19T00:00:00Z", "2026-06-24T00:00:00Z", true, category = "others"),
            CalendarEvent("e5", "계약서 제출 마감", "", "2026-06-16T00:00:00Z", "2026-06-17T00:00:00Z", true, category = "deadline"),
        )
        val bars = layoutMonthBars(events, grid, tz)
        val dots = timedSingleDayDots(events, tz)
        val todoDueDates = setOf(LocalDate(2026, 6, 18))
        val dayEvents = events.filter { selected in eventDays(it.start, it.end, it.allDay, tz) }
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "일정", onBack = {}) {
                Column(Modifier.padding(horizontal = 16.dp)) {
                    Text("${month.year}년 ${month.month}월", style = DenebType.subject, color = MaterialTheme.colorScheme.onBackground)
                    Spacer(Modifier.height(8.dp))
                    Row(Modifier.fillMaxWidth()) {
                        koreanDayOfWeek.forEach { d ->
                            Text(d, style = DenebType.meta, color = denebHint(), textAlign = TextAlign.Center, modifier = Modifier.weight(1f).padding(vertical = 4.dp))
                        }
                    }
                    CalendarMonthGrid(grid, today, selected, bars, dots, todoDueDates, {})
                    Spacer(Modifier.height(12.dp))
                    HorizontalDivider(color = denebHairline())
                    Spacer(Modifier.height(8.dp))
                    Text("6월 3일 (수) · ${dayEvents.size}건", style = DenebType.sectionLabel, color = MaterialTheme.colorScheme.primary)
                    CalendarDayList(dayEvents, selected, tz, {})
                }
            }
        }
    },
    "calendar_add" to { scheme ->
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
    },
    "todo_add" to { scheme ->
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
    },
    "cron_edit" to { scheme -> cronEditBody(scheme, cronWeeklyDraft, "Asia/Seoul") },
    "cron_edit_interval" to { scheme -> cronEditBody(scheme, cronIntervalDraft, "") },
    "cron_edit_advanced" to { scheme -> cronEditBody(scheme, cronAdvancedDraft, "Asia/Seoul") },
    "prompt_editor" to { scheme ->
        val draft = """
            다음 메일을 한국어로 심층 분석하라.
            - 발신자/거래처 맥락을 위키에서 결합
            - 마감·금액·의사결정 신호를 추출
            - 중요도(긴급/주의/일반)로 분류
        """.trimIndent()
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "프롬프트 코너", onBack = {}) {
                PromptStyleEditor(
                    title = "자동 메일 분석",
                    meta = "productivity · 수정됨 · mail-analysis",
                    description = "새 메일 도착 시 자동 분석에 쓰이는 프롬프트입니다.",
                    draft = draft,
                    onDraft = {},
                    readOnly = false,
                    saving = false,
                    error = null,
                    notice = "저장됨",
                    onBack = {},
                    canSave = true,
                    onSave = {},
                    trailingActions = {
                        OutlinedButton(onClick = {}, enabled = true) {
                            Icon(Icons.Outlined.Restore, contentDescription = null, modifier = Modifier.size(18.dp))
                            Spacer(Modifier.width(6.dp))
                            Text("복구")
                        }
                    },
                )
            }
        }
    },
    "topic_doc_editor" to { scheme ->
        val draft = """
            # 탑솔라 업무 배경

            - 사업: 태양광 EPC · 모듈 유통 · RE100 고객사
            - 핵심 거래처: 남도에코에너지, 에코프로, JOCA Cable
            - 의사결정: 견적 단가·납기는 김민준 부장 확인 후 회신
        """.trimIndent()
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "프롬프트 코너", onBack = {}) {
                PromptStyleEditor(
                    title = "업무.md",
                    meta = "업무 · ${draft.encodeToByteArray().size}/24000B",
                    description = "시스템 프롬프트에 주입되는 이 토픽의 배경 지식입니다. 저장하면 다음 세션부터 반영됩니다.",
                    draft = draft,
                    onDraft = {},
                    readOnly = false,
                    saving = false,
                    error = null,
                    notice = "저장됨 · 다음 세션부터 반영",
                    onBack = {},
                    canSave = true,
                    onSave = {},
                    trailingActions = {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Checkbox(checked = true, onCheckedChange = {}, enabled = true)
                            Text("즉시 적용", style = DenebType.meta, color = denebHint())
                        }
                    },
                )
            }
        }
    },
    "workfeed" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Box(Modifier.width(412.dp)) {
                    WorkFeedPanel(items = sampleFeed, onOpen = {}, onRunAction = { _, _ -> }, onClose = {})
                }
            }
        }
    },
    "dashboard" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "파트별 업무 현황", onBack = {}) {
                DashboardLanesContent(sampleDashboard)
            }
        }
    },
    "org_chart" to { scheme -> orgChartBody(scheme, "") },
    "org_chart_edit" to { scheme -> orgChartBody(scheme, "", editMode = true) },
    // The "이 딜 노트북" link on a project wiki page, in a minimal page-header context.
    "wiki_notebook_link" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "위키", onBack = {}) {
                Column(Modifier.padding(horizontal = 24.dp)) {
                    Spacer(Modifier.height(8.dp))
                    Text(
                        "영산고 태양광 발전소",
                        style = DenebType.subject,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Text("프로젝트  ·  2026-06-21", style = DenebType.meta)
                    DealNotebookLinkRow(sourceCount = 7) {}
                }
            }
        }
    },
    "org_chart_search" to { scheme -> orgChartBody(scheme, "김철수") },
    "org_editor" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                OrgNodeEditor(node = sampleOrg.first { it.id == "team1a" }, onChange = {}, onDelete = {}, onDone = {})
            }
        }
    },
    "scrub_active" to { scheme ->
        // The shared scrub list rendered MID-SCRUB (previewActiveKey) so the static
        // image shows the magnified bubble + active-letter highlight + wider strip.
        val labels = listOf(
            "가온전자", "강원물산", "남도에코", "다온", "라온상사", "메일", "바다물산",
            "사진", "삼성전자", "아워홈", "이마트", "자이언트", "전화", "차차상사",
            "카카오톡", "타이거", "파인", "하나은행", "Google", "Notion", "Slack", "Zoom",
        )
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Box(Modifier.width(412.dp)) {
                    SectionedScrubList(
                        items = labels,
                        label = { it },
                        key = { it },
                        previewActiveKey = "ㅈ",
                    ) { label ->
                        Text(
                            label,
                            style = DenebType.rowTitle,
                            color = MaterialTheme.colorScheme.onBackground,
                            modifier = Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 11.dp),
                        )
                    }
                }
            }
        }
    },
    "contacts" to { scheme ->
        val contacts = listOf(
            Triple("김민준 부장", "탑솔라", listOf("010-1234-5678")),
            Triple("나성호", "남도에코", listOf("010-2222-3333")),
            Triple("이서연", "현대차 구매팀", emptyList()),
            Triple("박지훈", "", listOf("010-9876-5432")),
            Triple("최유나 과장", "LG전자", listOf("010-7777-8888")),
            Triple("한도현", "", emptyList()),
            Triple("James Park", "Google", listOf("010-1111-2222")),
            Triple("Müller", "BMW", emptyList()),
        ).map { ContactRow(name = it.first, org = it.second, phones = it.third) }
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Box(Modifier.width(412.dp)) {
                    ContactsList(contacts = contacts, onOpen = {})
                }
            }
        }
    },
    "mail" to { scheme ->
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
                }
            }
        }
    },
    // Chat empty/welcome — muted sparkle glyph + personalized greeting (was a purple orb).
    "chat_empty" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                EmptyState(recallEnabled = true, modifier = Modifier.fillMaxSize())
            }
        }
    },
    "files_text_markdown" to { scheme -> filesTextBody(scheme, "프로젝트_X.md", true, markdownSample) },
    "files_text_plain" to { scheme -> filesTextBody(scheme, "deploy.log", false, filesPlainSample) },
    "skills_list" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Column {
                    SkillsViewSwitcher(showLifecycle = false, onSelect = {})
                    SkillListContent(sampleSkillRows)
                }
            }
        }
    },
    "skill_detail" to { scheme ->
        val now = System.currentTimeMillis()
        val detail = SkillDetailResponse(
            skill = sampleSkillRows[1].copy(
                evolveCount = 1,
                lastEvolvedAt = now - 2 * 3_600_000L,
                totalUses = 2,
                lastUsedAt = now - 9 * 3_600_000L,
            ),
            body = """
                ---
                name: morning-letter-composite
                description: 아침 브리핑 편지를 일정·메일·할일 데이터로 합성하는 절차
                version: 0.1.0
                ---

                # 아침 편지 합성

                ## 절차
                1. 오늘 일정(`miniapp.calendar`)과 미결 할일을 모은다.
                2. 밤사이 도착한 메일 요약을 합친다.
                3. **한 통의 편지**로 합성해 아침 브리핑으로 보낸다.
            """.trimIndent(),
            path = "/home/u/.deneb/skills/genesis/productivity/morning-letter-composite/SKILL.md",
        )
        val events = sampleLifecycleEvents(now).filter { it.skillName == "morning-letter-composite" }
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Column(Modifier.padding(horizontal = 24.dp, vertical = 8.dp)) {
                    SkillDetailContent(detail, events)
                }
            }
        }
    },
    "self_improvement_coding" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                SelfImprovementCodingContent(sampleSelfImprovementCodingQueue(System.currentTimeMillis()))
            }
        }
    },
    "skills_lifecycle" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Column {
                    SkillsViewSwitcher(showLifecycle = true, onSelect = {})
                    val now = System.currentTimeMillis()
                    val events = sampleLifecycleEvents(now)
                    SkillLifecycleRow(events[1], initiallyExpanded = true, onOpenSkill = {})
                    HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
                    SkillLifecycleContent(events.filterIndexed { i, _ -> i != 1 })
                }
            }
        }
    },
)

// Org chart body (diagram + people search). A non-blank [query] seeds the search box so
// the search-active state (hit highlight + results strip) is previewable.
@Composable
private fun orgChartBody(scheme: ColorScheme, query: String, editMode: Boolean = false) {
    MaterialTheme(colorScheme = scheme) {
        DenebScreenScaffold(title = "조직도", onBack = {}) {
            OrgChartContent(
                nodes = sampleOrg,
                notice = null,
                error = null,
                editMode = editMode,
                onEditNode = {},
                onAddChild = {},
                onAddRoot = {},
                initialQuery = query,
            )
        }
    }
}

// Files text viewer body (markdown / plain), driven by [displayName]/[markdown]/[text].
@Composable
private fun filesTextBody(scheme: ColorScheme, displayName: String, markdown: Boolean, text: String) {
    MaterialTheme(colorScheme = scheme) {
        Surface(color = MaterialTheme.colorScheme.background) {
            FilesTextViewerContent(name = displayName, markdown = markdown, text = text, loadOk = true, onBack = {}, onRetry = {})
        }
    }
}

// Shared cron-edit body for the three schedule-mode variants (weekly / interval /
// advanced), each driven by a different [draft]. Used by the previewScreens entries.
@Composable
private fun cronEditBody(scheme: ColorScheme, draft: ScheduleDraft, tz: String) {
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

// Render a registry screen to a PNG at [width]x[height]@[density] — the common
// ImageComposeScene plumbing the per-screen render* functions used to repeat.
private fun renderScreen(pngName: String, screen: String, scheme: ColorScheme, width: Int, height: Int, density: Float = 2f) {
    val body = previewScreens[screen] ?: error("unknown preview screen '$screen'")
    val scene = ImageComposeScene(width = width, height = height, density = Density(density)) { body(scheme) }
    val image = scene.render()
    val data = image.encodeToData(EncodedImageFormat.PNG) ?: error("PNG encode failed")
    File("/tmp/deneb-render").mkdirs()
    File("/tmp/deneb-render/$pngName").writeBytes(data.bytes)
    scene.close()
}

// Sample drafts for the cron edit previews — one per schedule mode so the segmented
// control, weekday chips, interval row, and raw-cron fallback all get exercised.
private val cronWeeklyDraft = ScheduleDraft(SchedMode.WEEKLY, "08:00", setOf(1, 3, 5), "30", IntervalUnit.MIN, LocalDate.parse("2026-06-13"), "")
private val cronIntervalDraft = ScheduleDraft(SchedMode.INTERVAL, "09:00", emptySet(), "15", IntervalUnit.MIN, LocalDate.parse("2026-06-13"), "")
private val cronAdvancedDraft = ScheduleDraft(SchedMode.ADVANCED, "09:00", emptySet(), "30", IntervalUnit.MIN, LocalDate.parse("2026-06-13"), "*/5 8-22 * * 1-6")

private fun renderBottomBar(name: String, scheme: ColorScheme, route: String) {
    // Phone width (412dp = 824px @ density 2) so the bar matches the real device. The
    // navigate-action callbacks are no-ops here — this checks the icons/labels/selection
    // only (피드·메일·채팅·달력·더보기).
    val scene = ImageComposeScene(width = 824, height = 240, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Column(Modifier.fillMaxSize()) {
                    Spacer(Modifier.weight(1f))
                    DenebBottomBar(
                        currentRoute = route,
                        onNavigate = {},
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

// Design-refresh pilot (2026-06): the grouped-inset card idiom + the two-accent
// system — cool primary on the selected row, warm apricot on the AI-insight callout.
private fun renderDesignRefresh(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 824, height = 1380, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Column(Modifier.fillMaxSize().padding(top = 26.dp)) {
                    Text(
                        "설정",
                        style = DenebType.viewTitle,
                        color = MaterialTheme.colorScheme.onBackground,
                        modifier = Modifier.padding(start = 24.dp, bottom = 16.dp),
                    )
                    DenebGroup(label = "시스템") {
                        DenebListRow("게이트웨이", {}, icon = Icons.Outlined.Dns, subtitle = "연결 · 버전 · 동기화")
                        DenebListRow("화면", {}, icon = Icons.Outlined.Palette, subtitle = "테마 · UI 배율")
                        DenebListRow("모델", {}, icon = Icons.Outlined.Memory, subtitle = "역할별 지정 · 엔드포인트", selected = true, divider = false)
                    }
                    Spacer(Modifier.height(22.dp))
                    DenebGroup(label = "자동화 · 관찰") {
                        DenebListRow("스킬", {}, icon = Icons.Outlined.Extension, subtitle = "설치 · Propus")
                        DenebListRow("크론", {}, icon = Icons.Outlined.Schedule, subtitle = "예약 작업")
                        DenebListRow("관찰", {}, icon = Icons.Outlined.Visibility, subtitle = "동작 · 로그", divider = false)
                    }
                    Spacer(Modifier.height(26.dp))
                    Row(
                        Modifier
                            .padding(horizontal = 16.dp)
                            .fillMaxWidth()
                            .clip(RoundedCornerShape(16.dp))
                            .background(denebInsightContainer())
                            .padding(16.dp),
                        verticalAlignment = Alignment.Top,
                    ) {
                        Icon(Icons.Outlined.AutoAwesome, contentDescription = null, tint = denebInsight(), modifier = Modifier.size(22.dp))
                        Spacer(Modifier.width(12.dp))
                        Column {
                            Text("AI 분석", style = DenebType.rowTitleStrong, color = denebInsight())
                            Spacer(Modifier.height(2.dp))
                            Text(
                                "탑솔라 견적 3건이 환차익 구간에 들어왔습니다. 월요일 콜 권장.",
                                style = DenebType.rowSubtitle,
                                color = MaterialTheme.colorScheme.onBackground,
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

private fun renderMarkdown(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 840, height = 700, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                MarkdownContent(markdownSample, Modifier.padding(20.dp), baseStyle = MaterialTheme.typography.bodyMedium)
            }
        }
    }
    val image = scene.render()
    val data = image.encodeToData(EncodedImageFormat.PNG) ?: error("PNG encode failed")
    File("/tmp/deneb-render").mkdirs()
    File("/tmp/deneb-render/$name").writeBytes(data.bytes)
    scene.close()
}

// Validates the 이름 / 내용 / 의미 search-scope selector (FilesSearchModeRow): the
// Material SingleChoiceSegmentedButton with the given mode highlighted, at phone
// width — confirms the three Korean labels fit and the selected segment reads.
private fun renderFilesSearchMode(name: String, scheme: ColorScheme, mode: FilesSearchMode) {
    val scene = ImageComposeScene(width = 824, height = 140, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                FilesSearchModeRow(mode = mode, onModeChange = {})
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

// The exact transcript shape the gateway's denebui.CollapsedReportFence emits
// for a per-mail analysis: an accordion (title-only collapsed card) wrapping a
// markdown body. expanded=true previews the post-tap state so the markdown
// child's rendering (headings/list/table) is visually checked too.
private fun collapsedReportFence(expanded: Boolean): String {
    // Body starts after the title line — the gateway strips the heading that
    // became the accordion title (collapsedReportBody) so the expanded card
    // doesn't open by repeating its own header.
    val body = "**발신**: fred@jocacable.com — 견적 회신 요청\\n\\n### 왜 지금 왔는가\\n- 5/12 물량산출 메일의 후속, 단가 협상 단계 진입\\n- **회신 기한: 6/13(금)** 명시\\n\\n| 구분 | 내용 |\\n|:---|:---|\\n| 거래처 | JOCA Cable (케이블 협력사) |\\n| 요청 | 1,950매 모듈 물량 견적 회신 |\\n\\n### 권고\\n1. 김민준 부장에게 단가표 확인 요청\\n2. 금요일 오전까지 회신 초안 준비"
    val exp = if (expanded) "\"expanded\":true," else ""
    return "```deneb-ui\n{\"type\":\"accordion\",\"title\":\"📧 JOCA Cable 최신 메일 분석 보고\",$exp\"children\":[{\"type\":\"markdown\",\"value\":\"$body\"}]}\n```"
}

private fun renderCollapsedReport(name: String, scheme: ColorScheme, expanded: Boolean) {
    val scene = ImageComposeScene(width = 824, height = if (expanded) 1560 else 320, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Box(Modifier.width(412.dp)) {
                    MarkdownContent(collapsedReportFence(expanded), modifier = Modifier.fillMaxWidth().padding(16.dp))
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

private fun widgetGlyph(pathData: String): ImageVector = ImageVector.Builder(
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

// The part-grouped work dashboard (파트별 업무 현황): the five fixed 파트 lanes (one
// empty to exercise the "지금 할 일이 없습니다" line) plus the muted 미분류 triage lane.
// Mixed scheduled times (오늘/내일/dated) exercise dashboardTimeLabel; lane 2 is empty.
private val sampleDashboard = listOf(
    LaneOut(
        key = "team1",
        name = "기획조정실 1팀 (인허가)",
        items = listOf(
            DashboardItem("RE100 고객사 인허가 서류 제출", "본사 3층 · 김민준 부장", "calendar", "calendar", "e1", System.currentTimeMillis() + 2 * 3_600_000L),
            DashboardItem("남도에코 모듈 입고 점검", "현장 — 1,950매 검수", "calendar", "calendar", "e2", System.currentTimeMillis() + 26 * 3_600_000L),
        ),
    ),
    LaneOut(key = "team2", name = "기획조정실 2팀 (루프탑)", items = emptyList()),
    LaneOut(
        key = "team3",
        name = "기획조정실 3팀 (모듈)",
        items = listOf(
            DashboardItem("📧 JOCA Cable 견적 회신 요청", "발신 fred@jocacable.com — 회신 기한 6/13(금)", "mail_report", "workfeed", "wf1", System.currentTimeMillis() - 40 * 60_000L),
        ),
    ),
    LaneOut(
        key = "namdo",
        name = "남도에코에너지",
        items = listOf(
            DashboardItem("준공정산서 검토 — 남도에코", "₩19,500,000 · 결제 30일", "capture_image", "workfeed", "wf2", System.currentTimeMillis() + 3 * 3_600_000L),
        ),
    ),
    LaneOut(
        key = "personal",
        name = "개인 / 기타",
        items = listOf(
            DashboardItem("법인카드 정산 (5월분)", "마감 임박", "proactive", "workfeed", "wf3", 0L),
        ),
    ),
    LaneOut(
        key = "unclassified",
        name = "미분류",
        items = listOf(
            DashboardItem("무림 울산공장 풍력 검토안", "박종원 부장 — 담당 파트 미지정", "proactive", "workfeed", "wf4", System.currentTimeMillis() + 50 * 3_600_000L),
        ),
    ),
)

// The org chart (조직도): a group → 실/회사 → 팀 → 파트 hierarchy joined by parentId,
// with lane-tagged parts (the 파트 chip) and members carrying 직급/직책. Fake names only
// (mirrors org.example.json) — exercises the multi-level DIAGRAM (boxes + connector
// lines), type badges, lane chips, leader/member-count summaries, a 4th level (the two
// sub-parts under 1팀) to show depth, a 겸직 (김철수 appears in both 기획조정실 and 1팀)
// for the search demo, and the bare 개인/기타 node (no members) for the empty-summary box.
// A few members carry FAKE phones/emails (as the gateway's GET enrichment would attach
// from the contacts store) so the contact call/email shortcuts render in the editor +
// search previews: 김철수 has both (search-result + editor), 이몽룡 email-only and 성춘향
// phone-only (single-glyph cases), the rest none (no contact row).
private val sampleOrg = listOf(
    OrgNodeOut(id = "group", name = "예시그룹", type = "group", parentId = "", members = listOf(MemberOut("홍길동", "회장", "회장"))),
    OrgNodeOut(
        id = "planning",
        name = "기획조정실",
        type = "division",
        parentId = "group",
        members = listOf(MemberOut("김철수", "전무", "실장", phones = listOf("010-0000-0001"), emails = listOf("kim@example.test"))),
    ),
    OrgNodeOut(
        id = "team1",
        name = "기획조정실 1팀",
        type = "team",
        parentId = "planning",
        lane = "team1",
        members = listOf(
            MemberOut("김철수", "전무", "팀장", phones = listOf("010-0000-0001"), emails = listOf("kim@example.test")),
            MemberOut("이몽룡", "과장", "팀원", emails = listOf("lee@example.test")),
        ),
        keywords = listOf("인허가", "개발행위"),
        companies = listOf("사아건설"),
    ),
    // 4th level: two parts under 1팀 — exercises a deeper branch + the connector bus.
    OrgNodeOut(id = "team1a", name = "인허가파트", type = "team", parentId = "team1", lane = "team1a", keywords = listOf("인허가", "개발행위허가", "발전사업허가"), companies = listOf("한국전력", "산업부"), members = listOf(MemberOut("이몽룡", "과장", "팀장", emails = listOf("lee@example.test")))),
    OrgNodeOut(id = "team1b", name = "개발행위파트", type = "team", parentId = "team1", members = listOf(MemberOut("방자", "대리", "팀원"))),
    OrgNodeOut(
        id = "team2",
        name = "기획조정실 2팀",
        type = "team",
        parentId = "planning",
        lane = "team2",
        members = listOf(MemberOut("성춘향", "부장", "팀장", phones = listOf("010-0000-0002")), MemberOut("변학도", "대리", "팀원")),
        keywords = listOf("루프탑", "지붕"),
    ),
    OrgNodeOut(
        id = "namdo",
        name = "남도에코",
        type = "company",
        parentId = "group",
        lane = "namdo",
        members = listOf(MemberOut("장끼동", "상무", "대표"), MemberOut("까투리", "주임", "팀원")),
        keywords = listOf("케이블", "전선"),
        companies = listOf("가나에너지", "다라전기"),
    ),
    OrgNodeOut(id = "personal", name = "개인/기타", type = "team", parentId = "group"),
)

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

// --- Skills tab (settings) -------------------------------------------------
// Validates the origin badges (생성 vs 최초) on the skill list and the
// Propus timeline rows (genesis/evolved/rejected/review badges).

private val sampleSkillRows = listOf(
    SkillRow(
        name = "email-analysis",
        description = "새 메일 도착(cron 트리거) 또는 직접 요청 시 Gmail 단일 메일을 심층 분석하는 워크플로우 — 스레드 수집, 위키 컨텍스트 결합, 중요도 분류.",
        category = "productivity",
        source = "managed",
        version = "1.1.1",
        origin = "initial",
        evolveCount = 2,
        totalUses = 7,
        tags = listOf("mail", "analysis"),
        relatedSkills = listOf("meeting-minutes"),
        dependencySummary = listOf("bins gh", "env GMAIL_TOKEN"),
        installSummary = listOf("Install GitHub CLI"),
    ),
    SkillRow(
        name = "morning-letter-composite",
        description = "아침 브리핑 편지를 일정·메일·할일 데이터로 합성하는 절차",
        category = "productivity", source = "managed", version = "0.1.0",
        origin = "genesis", createdAt = 1L, curatorState = "active", totalUses = 2,
    ),
    SkillRow(
        name = "playwright",
        description = "브라우저 자동화 작업 절차",
        category = "integration",
        source = "managed",
        version = "1.0.0",
        origin = "initial",
    ),
)

private fun sampleLifecycleEvents(now: Long) = listOf(
    SkillLifecycleEvent(
        type = "evolved",
        skillName = "email-analysis",
        at = now - 2 * 3_600_000L,
        version = "1.1.1",
        detail = "gmail 도구 오류 시 가용 정보로 보고를 완료하도록 절차 보강",
    ),
    SkillLifecycleEvent(
        type = "review",
        skillName = "email-analysis",
        at = now - 3 * 3_600_000L,
        route = "no-op",
        detail = "기존 email-analysis 스킬이 해당 워크플로우를 이미 커버. 세션은 단일 메일 분석 요청으로 스킬 범위 내 — 새 스킬 생성이나 절차 변경 근거 없음.",
        evidence = "cron(email-single-analysis) → gmail 스레드 수집 → 위키 컨텍스트 결합 → 중요도 분류까지 기존 절차대로 완주",
    ),
    SkillLifecycleEvent(
        type = "evolve_rejected",
        skillName = "email-analysis",
        at = now - 26 * 3_600_000L,
        detail = "self-test rejected: 절차 퇴보 (위키 업데이트 단계 누락)",
    ),
    SkillLifecycleEvent(
        type = "genesis",
        skillName = "morning-letter-composite",
        at = now - 50 * 3_600_000L,
        detail = "아침 편지 합성 절차를 재사용 스킬로 추출",
    ),
)

private fun sampleSelfImprovementCodingQueue(now: Long) = SelfImprovementCodingListResponse(
    candidates = listOf(
        SelfCorrectionCandidate(
            id = "sc-coding-1",
            status = "proposed",
            scope = "code",
            title = "MOSS식 소스 후보 상태 표면",
            proposedChange = "자가개선 코딩 화면에서 코드 후보의 대기/적용/기각 상태를 분리해 표시",
            evidence = "코딩 에이전트가 기록한 후보가 JSONL에만 남아 native에서 검토되지 않음",
            risk = "적용 완료 이벤트와 후보가 섞이면 상태를 오해할 수 있음",
            targetFiles = listOf("ConfigSelfImprovementCodingTab.kt", "self_improvement_coding.go"),
            evidenceKinds = listOf("session", "evidence", "target_files", "risk"),
            reviewActions = listOf("open_session", "inspect_target_files", "run_focused_validation", "mark_review_status"),
            createdAt = now - 45 * 60_000L,
            updatedAt = now - 45 * 60_000L,
        ),
    ),
    count = 1,
    statusCounts = listOf(
        SelfImprovementCodingStatusCount(status = "proposed", count = 1),
        SelfImprovementCodingStatusCount(status = "accepted", count = 0),
        SelfImprovementCodingStatusCount(status = "applied", count = 1),
        SelfImprovementCodingStatusCount(status = "rejected", count = 1),
        SelfImprovementCodingStatusCount(status = "superseded", count = 0),
        SelfImprovementCodingStatusCount(status = "all", count = 3),
    ),
)
