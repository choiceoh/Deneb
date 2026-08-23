@file:OptIn(androidx.compose.ui.ExperimentalComposeUiApi::class)

package ai.deneb

import ai.deneb.deneb.AppTilesContent
import ai.deneb.deneb.BrowserBookmark
import ai.deneb.deneb.BrowserStartPane
import ai.deneb.deneb.BrowserVisit
import ai.deneb.deneb.CalendarEventDetail
import ai.deneb.deneb.DenebBrowserChrome
import ai.deneb.deneb.DenebMoreScreen
import ai.deneb.deneb.DenebWebViewState
import ai.deneb.deneb.FilesSearchMode
import ai.deneb.deneb.MailMessage
import ai.deneb.deneb.PersonHit
import ai.deneb.deneb.SearchFileResult
import ai.deneb.deneb.SearchHit
import ai.deneb.deneb.SearchMailResult
import ai.deneb.deneb.SearchResults
import ai.deneb.deneb.SearchSourceAvailability
import ai.deneb.deneb.Todo
import ai.deneb.ui.DarkColorScheme
import ai.deneb.ui.DenebRow
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.LightColorScheme
import ai.deneb.ui.chat.composables.WaitingResponseRow
import ai.deneb.ui.denebHint
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.ui.Alignment
import androidx.compose.ui.ImageComposeScene
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import kotlinx.collections.immutable.persistentListOf
import org.jetbrains.skia.EncodedImageFormat
import java.io.File

// Off-screen render harness: renders Deneb composables to PNG via Skia so the
// look (and bugs like invisible text) can be inspected without building +
// installing the APK. Run with `./gradlew :composeApp:renderPreviews`.

internal val sample = listOf(
    MailMessage("1", "김철수 <kim@topsolar.kr>", "내일 회의 자료 확인 부탁드립니다", "안녕하세요, 첨부한 자료 검토 후 회신 부탁드립니다.", "2026-05-31T09:12:00Z", true, priority = "urgent", priorityHint = "마감 표현 · 회의"),
    MailMessage("2", "GitHub <noreply@github.com>", "[deneb] PR #1814 merged", "Your pull request was merged into main.", "2026-05-31T08:40:00Z", false),
    MailMessage("4", "박영업 <park@vendor.co.kr>", "모듈 견적서 송부 — 1,950매", "견적 금액은 첨부 파일을 참조 부탁드립니다.", "2026-05-31T07:30:00Z", false, priority = "attention", priorityHint = "견적 · 금액"),
    MailMessage("3", "이영희 <lee@example.com>", "(제목 없음)", "", "2026-05-30T22:05:00Z", false),
)

internal val markdownSample = """
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
internal val filesPlainSample = """
    2026-06-20T09:12:01Z INFO  gateway 시작 (port=18789)
    2026-06-20T09:12:02Z INFO  provider wormhole 연결됨
    2026-06-20T09:12:03Z WARN  prefix_cache 미적중 (cold start)
    2026-06-20T09:12:08Z INFO  miniapp.files.list ok (entries=14)
""".trimIndent()

private fun renderBrowser(name: String, scheme: ColorScheme) {
    val state = DenebWebViewState("https://en.wikipedia.org/wiki/Deneb").apply { translateEnabled = true }
    val scene = ImageComposeScene(width = 824, height = 900, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            DenebBrowserChrome(state = state, onBack = {}, tabCount = 3, onShowTabs = {}) {
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

private fun renderBrowserError(name: String, scheme: ColorScheme) {
    val state = DenebWebViewState("https://offline.example").apply {
        markMainFrameFailed("주소를 찾을 수 없습니다 — 도메인이 맞는지 확인해 주세요")
    }
    val scene = ImageComposeScene(width = 824, height = 900, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            DenebBrowserChrome(state = state, onBack = {}, tabCount = 3, onShowTabs = {}) {
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

private fun renderBrowserStart(name: String, scheme: ColorScheme, empty: Boolean) {
    val state = DenebWebViewState("")
    val bookmarks = if (empty) {
        emptyList()
    } else {
        listOf(BrowserBookmark(url = "https://en.wikipedia.org/wiki/Deneb", title = "Deneb"))
    }
    val visits = if (empty) {
        emptyList()
    } else {
        listOf(BrowserVisit(url = "https://example.com", title = "Example"))
    }
    val scene = ImageComposeScene(width = 824, height = 900, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            DenebBrowserChrome(state = state, onBack = {}) {
                BrowserStartPane(
                    bookmarks = bookmarks,
                    visits = visits,
                    onOpen = {},
                    modifier = Modifier.fillMaxWidth().weight(1f),
                )
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
    renderScreen("chat_states_dark.png", "chat_states", DarkColorScheme, 824, 2300)
    renderScreen("chat_states_light.png", "chat_states", LightColorScheme, 824, 2300)
    renderScreen("session_drawer_dark.png", "session_drawer", DarkColorScheme, 720, 1440)
    renderScreen("session_drawer_search_dark.png", "session_drawer_search", DarkColorScheme, 720, 1440)
    renderScreen("session_drawer_actions_dark.png", "session_drawer_actions", DarkColorScheme, 720, 1440)
    renderScreen("states_dark.png", "states", DarkColorScheme, 824, 1500)
    renderScreen("states_light.png", "states", LightColorScheme, 824, 1500)
    renderBrowser("browser_dark.png", DarkColorScheme)
    renderBrowser("browser_light.png", LightColorScheme)
    renderBrowserError("browser_error_dark.png", DarkColorScheme)
    renderBrowserError("browser_error_light.png", LightColorScheme)
    renderBrowserStart("browser_start_dark.png", DarkColorScheme, empty = false)
    renderBrowserStart("browser_start_light.png", LightColorScheme, empty = false)
    renderBrowserStart("browser_start_empty_dark.png", DarkColorScheme, empty = true)
    renderMore("more_dark.png", DarkColorScheme)
    renderMore("more_light.png", LightColorScheme)
    // 더보기 숨김: the grid with two tiles hidden (검색·브라우저) — verify they drop out.
    renderMore("more_hidden_dark.png", DarkColorScheme, hidden = setOf("deneb_search", "deneb_browser"))
    // The 설정 toggle section that drives it.
    renderAppTiles("app_tiles_dark.png", DarkColorScheme)
    renderAppTiles("app_tiles_light.png", LightColorScheme)
    renderMarkdown("markdown_dark.png", DarkColorScheme)
    renderTableAB("table_ab_dark.png", DarkColorScheme)
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
    renderLetterCard("letter_morning_dark.png", DarkColorScheme, morningLetterNode(), 1900)
    renderLetterCard("letter_morning_light.png", LightColorScheme, morningLetterNode(), 1900)
    renderLetterCard("letter_evening_dark.png", DarkColorScheme, eveningLetterNode(), 1040)
    renderLetterCard("letter_evening_light.png", LightColorScheme, eveningLetterNode(), 1040)
    // Full deneb-ui node gallery — every display node with dense Korean
    // business data (the letter skeletons only exercise a friendly subset).
    // This is the visual regression surface for card rendering quality.
    renderLetterCard("ui_gallery_dark.png", DarkColorScheme, uiGalleryNode(), 3200)
    renderLetterCard("ui_gallery_light.png", LightColorScheme, uiGalleryNode(), 3200)
    // Opt-in corpus audit over real transcript cards (env-gated, no-op in CI).
    renderCardCorpus()
    renderMessageCorpus()
    renderScreen("workfeed_dark.png", "workfeed", DarkColorScheme, 824, 1100)
    renderScreen("workfeed_light.png", "workfeed", LightColorScheme, 824, 1100)
    renderScreen("workfeed_answer_dark.png", "workfeed_answer", DarkColorScheme, 480, 720)
    renderScreen("workfeed_answer_light.png", "workfeed_answer", LightColorScheme, 480, 720)
    renderScreen("dashboard_dark.png", "dashboard", DarkColorScheme, 824, 1900)
    renderScreen("dashboard_light.png", "dashboard", LightColorScheme, 824, 1900)
    renderScreen("rsi_dark.png", "rsi", DarkColorScheme, 824, 1500)
    renderScreen("rsi_light.png", "rsi", LightColorScheme, 824, 1500)
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
                    // The live A↔B pair the user actually sees while a tool runs:
                    // rotating generic text vs the same text + " · tool label"
                    // (isStatusOnly = false) — these two must align identically.
                    WaitingResponseRow(executingTools = persistentListOf("t1" to "메일 확인 중"))
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
                    // A LONG status — must stay ONE start-aligned ellipsized line, same
                    // row height as above (the centered two-line-wrap misalignment fix).
                    WaitingResponseRow(
                        executingTools = persistentListOf(
                            "t1" to "웹 검색 중: 아르고에너지 NDA 표준 조항 비교 — 국내 EPC 계약 관례와 손해배상 상한 조사",
                        ),
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

internal val sampleEvent = CalendarEventDetail(
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
internal val sampleSpanEvent = CalendarEventDetail(
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

internal val sampleTodos = listOf(
    Todo("todo:1", "남도에코 모듈 견적 회신", note = "6월말 납기 확인", due = "2026-06-09T00:00:00Z", dueAllDay = true),
    Todo("todo:2", "RE100 계약서 검토", due = "2026-06-10T05:00:00Z"),
    Todo("todo:3", "법인카드 정산", note = "5월분"),
    Todo("todo:4", "주간 보고 작성", due = "2026-06-08T00:00:00Z", dueAllDay = true, done = true),
)

internal val sampleSearch = SearchResults(
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
    files = listOf(
        SearchFileResult(
            path = "/계약/RE100-PPA-검토.md",
            name = "RE100-PPA-검토.md",
            snippet = "자동 갱신 조항은 만료 30일 전 서면 통지가 필요합니다.",
            score = 0.93,
            startLine = 42,
            endLine = 47,
            kind = "markdown",
            heading = "해지 및 갱신",
        ),
    ),
    mail = listOf(
        SearchMailResult(
            id = "preview-mail-1",
            from = "법무팀 <legal@example.com>",
            subject = "RE100 계약 갱신 검토",
            date = "2026-06-08",
            snippet = "갱신 조건과 단가표를 이번 주까지 확인해 주세요.",
            mailbox = "INBOX",
        ),
    ),
    sourceStatus = SearchSourceAvailability(
        wiki = "ok",
        diary = "ok",
        people = "ok",
        files = "ok",
        mail = "ok",
    ),
)
