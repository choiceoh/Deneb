@file:OptIn(androidx.compose.ui.ExperimentalComposeUiApi::class)

package ai.deneb

import ai.deneb.deneb.CalMonth
import ai.deneb.deneb.CalendarAddContent
import ai.deneb.deneb.CalendarDayList
import ai.deneb.deneb.CalendarEmptyDay
import ai.deneb.deneb.CalendarEvent
import ai.deneb.deneb.CalendarEventContent
import ai.deneb.deneb.CalendarMonthGrid
import ai.deneb.deneb.ContactsList
import ai.deneb.deneb.CronEditContent
import ai.deneb.deneb.DashboardLanesContent
import ai.deneb.deneb.DealNotebookLinkRow
import ai.deneb.deneb.DenebEmpty
import ai.deneb.deneb.DenebError
import ai.deneb.deneb.DenebLoading
import ai.deneb.deneb.FilesTextViewerContent
import ai.deneb.deneb.MailRow
import ai.deneb.deneb.OrgChartContent
import ai.deneb.deneb.OrgNodeEditor
import ai.deneb.deneb.PromptStyleEditor
import ai.deneb.deneb.RsiStatusContent
import ai.deneb.deneb.ScheduleDraft
import ai.deneb.deneb.SearchContent
import ai.deneb.deneb.SelfImprovementCodingContent
import ai.deneb.deneb.SkillDetailContent
import ai.deneb.deneb.SkillLifecycleContent
import ai.deneb.deneb.SkillLifecycleRow
import ai.deneb.deneb.SkillListContent
import ai.deneb.deneb.SkillsViewSwitcher
import ai.deneb.deneb.TodoAddContent
import ai.deneb.deneb.TodoListContent
import ai.deneb.deneb.buildMonthGrid
import ai.deneb.deneb.eventDays
import ai.deneb.deneb.generated.ContactRow
import ai.deneb.deneb.generated.SkillDetailResponse
import ai.deneb.deneb.koreanDayOfWeek
import ai.deneb.deneb.layoutMonthBars
import ai.deneb.deneb.timedSingleDayDots
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import ai.deneb.ui.chat.WorkFeedAction
import ai.deneb.ui.chat.WorkFeedItem
import ai.deneb.ui.chat.composables.EmptyState
import ai.deneb.ui.chat.composables.WorkFeedAnswerBlock
import ai.deneb.ui.chat.composables.WorkFeedPanel
import ai.deneb.ui.components.DenebUnderlineSearchField
import ai.deneb.ui.components.SectionedScrubList
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.icons.outlined.Restore
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
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
import androidx.compose.material.icons.Icons
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
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import kotlinx.datetime.LocalDate
import kotlinx.datetime.TimeZone

// Single source of truth for inspectable screens: name -> body. Reused by the PNG
// renderer in RenderPreviewDynamicUi.kt and the headless semantics inspector
// (PreviewInspect.kt / ui-inspect.sh), so the same composition projects to pixels
// or to a text semantics tree. Each body is the bare screen under its theme; the
// caller supplies the surface and size.
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
    "workfeed_answer" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Column(
                    Modifier.width(412.dp).padding(12.dp),
                    verticalArrangement = Arrangement.spacedBy(20.dp),
                ) {
                    DenebSectionLabel("선택형 질문 — 답변 칩")
                    WorkFeedAnswerBlock(
                        item = WorkFeedItem(
                            id = "q1",
                            title = "새 거래: 한빛에너지",
                            summary = "어느 팀이 담당할까요?",
                            question = true,
                            actions = listOf(
                                WorkFeedAction(id = "dept:pl1", label = "1팀"),
                                WorkFeedAction(id = "dept:pl2", label = "2팀"),
                                WorkFeedAction(id = "dept:pl3", label = "3팀"),
                                WorkFeedAction(id = "dept:nde", label = "남도에코"),
                                WorkFeedAction(id = "dept:none", label = "딜 아님"),
                            ),
                        ),
                        onAnswer = { _, _, _, _ -> },
                    )
                    DenebSectionLabel("자유 답변 — 입력란")
                    WorkFeedAnswerBlock(
                        item = WorkFeedItem(
                            id = "q2",
                            title = "회신 필요",
                            summary = "내일 미팅 시간을 몇 시로 잡을까요?",
                            question = true,
                        ),
                        onAnswer = { _, _, _, _ -> },
                    )
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
    "rsi" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            DenebScreenScaffold(title = "재귀적 자가개선", onBack = {}) {
                RsiStatusContent(sampleRsi)
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
    // The three shared data states every content screen composes from (폴리시 기준
    // ★상태 완결성): skeleton loading, empty with action, error with retry — one
    // frame so their vertical rhythm and voice are reviewed together.
    // Chat transient states — only reachable on a device when something specific
    // happens, so this fixture is their only visual regression surface.
    "chat_states" to { scheme -> chatStatesBody(scheme) },
    "chat_input" to { scheme -> chatInputBody(scheme) },
    "session_drawer" to { scheme -> sessionDrawerBody(scheme) },
    "session_drawer_search" to { scheme -> sessionDrawerBody(scheme, searchOpen = true) },
    "session_drawer_actions" to { scheme -> sessionDrawerBody(scheme, revealedId = "client:main:nda") },
    "states" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Column(Modifier.width(412.dp)) {
                    DenebSectionLabel("로딩", Modifier.padding(start = 24.dp, top = 16.dp))
                    DenebLoading()
                    DenebSectionLabel("빈 상태", Modifier.padding(start = 24.dp))
                    DenebEmpty("최근 30일 메일 없음", actionLabel = "새로고침", onAction = {})
                    DenebSectionLabel("오류", Modifier.padding(start = 24.dp))
                    DenebError("메일을 불러오지 못했습니다.", onRetry = {})
                }
            }
        }
    },
    // Chat empty/welcome — muted sparkle glyph + personalized greeting (was a purple orb).
    "chat_empty" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                EmptyState(modifier = Modifier.fillMaxSize())
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
        val now = PREVIEW_NOW_MS
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
                // nowMs frozen too, not just the sample data — the relative timestamps
                // ("12일 전") are computed against it, so a live clock here would redraw
                // the golden every midnight.
                SelfImprovementCodingContent(
                    sampleSelfImprovementCodingQueue(PREVIEW_NOW_MS),
                    nowMs = PREVIEW_NOW_MS,
                )
            }
        }
    },
    "skills_lifecycle" to { scheme ->
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Column {
                    SkillsViewSwitcher(showLifecycle = true, onSelect = {})
                    val now = PREVIEW_NOW_MS
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
