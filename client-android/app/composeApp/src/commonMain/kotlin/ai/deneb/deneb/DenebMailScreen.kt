package ai.deneb.deneb

import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebChip
import ai.deneb.ui.components.DenebSearchField
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebPressable
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.FilterList
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch
import kotlinx.datetime.Instant
import kotlinx.datetime.LocalDate
import kotlinx.datetime.TimeZone
import kotlinx.datetime.daysUntil
import kotlinx.datetime.toLocalDateTime
import kotlinx.datetime.todayIn
import kotlin.time.Clock

/**
 * Native mail triage backed by `miniapp.gmail.list_recent`, in the Deneb idiom:
 * ultralight view title, full-width hairline rules between rows, DenebType roles,
 * and time-bucketed section labels (오늘 / 어제 / 이번 주 / 이전). Pull to refresh;
 * long-press a row (with haptic) to multi-select; a flat bottom bar runs bulk
 * read / archive / trash, and "더 보기" pages through nextPageToken. Tapping a
 * row opens the detail screen. The search field runs a full-mailbox native mail
 * query (IME search submits; clearing it returns to the recent mail view). Search and
 * native filters stay out of the list until the top-right toolbar icons open
 * them, so the mail tab starts directly at the messages. Controls (checkbox,
 * buttons, search field, pull refresh) stay Material; only the presentation is
 * Deneb.
 */
@OptIn(ExperimentalFoundationApi::class, ExperimentalMaterial3Api::class)
@Composable
fun DenebMailScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onOpenDetail: (String) -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
    // panelMode = rendered as the left pane of the desktop split-view (fills the parent
    // 380dp box instead of the 760dp centered column; back affordance hidden since the
    // pane is always shown). selectedId = the mail currently open in the right detail
    // pane, for row highlight.
    panelMode: Boolean = false,
    selectedId: String? = null,
) {
    val mail by client.denebMail.collectAsState()
    val nextToken by client.denebMailNextToken.collectAsState()
    val nativeStatus by client.denebMailNativeStatus.collectAsState()
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()

    // null = first load in flight, true = loaded ok, false = fetch failed.
    var loadOk by remember { mutableStateOf<Boolean?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    var selecting by remember { mutableStateOf(false) }
    val selected = remember { mutableStateListOf<String>() }
    var busy by remember { mutableStateOf(false) }
    var loadingMore by remember { mutableStateOf(false) }
    // Full-mailbox mail search: the field holds what the user types; activeQuery
    // is the query the current list actually came from (null = default recent mail).
    var searchText by remember { mutableStateOf("") }
    var activeQuery by remember { mutableStateOf<String?>(null) }
    var searchVisible by remember { mutableStateOf(false) }
    var filtersVisible by remember { mutableStateOf(false) }

    LaunchedEffect(Unit) {
        // refreshMail() already refreshes native_status on success, so calling it
        // here too would fetch miniapp.gmail.native_status twice on every open. Only
        // fetch it standalone when the list fails, so the status line (offline /
        // local-archive state) still shows when the live fetch can't.
        loadOk = client.refreshMail()
        if (loadOk != true) client.refreshMailNativeStatus()
    }

    fun runSearch(raw: String) {
        val q = raw.trim().ifBlank { null }
        if (q == activeQuery) return
        activeQuery = q
        scope.launch {
            loadOk = null
            loadOk = client.refreshMail(q)
        }
    }

    fun clearSelection() {
        selecting = false
        selected.clear()
    }

    fun bulk(action: suspend (String) -> Unit) {
        if (busy) return
        haptics.confirm()
        val ids = selected.toList()
        scope.launch {
            busy = true
            ids.forEach { action(it) }
            busy = false
            clearSelection()
        }
    }

    val offlineCapable = nativeStatus?.offlineCapable == true
    val activeFilter = remember(activeQuery, offlineCapable) {
        mailNativeFilters(offlineCapable)
            .firstOrNull { activeQuery.normalizeMailQuery() == it.query.normalizeMailQuery() }
    }
    val hasFreeTextSearch = activeQuery.normalizeMailQuery() != null && activeFilter?.query.normalizeMailQuery() != activeQuery.normalizeMailQuery()
    val showSearchField = searchVisible || hasFreeTextSearch
    val filterActive = activeFilter?.query.normalizeMailQuery() != null
    val screenTitle = when {
        hasFreeTextSearch -> "메일 검색"
        filterActive -> activeFilter?.label ?: "받은 메일"
        else -> "받은 메일"
    }

    DenebScreenScaffold(
        title = screenTitle,
        onBack = onBack,
        tabBar = navigationTabBar,
        actions = {
            if (!selecting) {
                IconButton(
                    onClick = {
                        haptics.tap()
                        if (showSearchField && !hasFreeTextSearch) {
                            searchVisible = false
                            searchText = ""
                        } else {
                            searchVisible = true
                        }
                    },
                    modifier = Modifier.size(40.dp),
                ) {
                    Icon(
                        Icons.Outlined.Search,
                        contentDescription = if (showSearchField) "메일 검색 닫기" else "메일 검색 열기",
                        tint = if (showSearchField) MaterialTheme.colorScheme.primary else denebHint(),
                    )
                }
                IconButton(
                    onClick = {
                        haptics.tap()
                        filtersVisible = !filtersVisible
                    },
                    modifier = Modifier.size(40.dp),
                ) {
                    Icon(
                        Icons.Outlined.FilterList,
                        contentDescription = if (filtersVisible) "메일 필터 닫기" else "메일 필터 열기",
                        tint = if (filtersVisible || filterActive) MaterialTheme.colorScheme.primary else denebHint(),
                    )
                }
            }
        },
        // The desktop split-view always shows this pane, so a back affordance is noise;
        // fillWidth keeps the column inside the 380dp pane instead of the desktop cap.
        showBack = !panelMode,
        fillWidth = panelMode,
    ) {
        if (selecting) {
            Row(
                modifier = Modifier.fillMaxWidth().padding(start = 24.dp, end = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    "${selected.size}개 선택",
                    style = DenebType.subject.copy(fontSize = 22.sp),
                    color = MaterialTheme.colorScheme.onBackground,
                    modifier = Modifier.weight(1f),
                )
                TextButton(onClick = { clearSelection() }) { Text("취소") }
            }
        }

        val listState = rememberLazyListState()
        LaunchedEffect(showSearchField, filtersVisible) {
            if (showSearchField || filtersVisible) {
                listState.animateScrollToItem(0)
            }
        }
        LaunchedEffect(listState, nextToken, activeQuery) {
            snapshotFlow {
                val layout = listState.layoutInfo
                val lastVisible = layout.visibleItemsInfo.lastOrNull()?.index ?: -1
                layout.totalItemsCount > 0 && lastVisible >= layout.totalItemsCount - 4
            }.distinctUntilChanged().collect { nearEnd ->
                if (nearEnd && nextToken != null && !loadingMore) {
                    loadingMore = true
                    client.loadMoreMail()
                    loadingMore = false
                }
            }
        }

        Box(Modifier.weight(1f).fillMaxWidth()) {
            if (mail.isEmpty() && loadOk == null) {
                Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { DenebLoading() }
            } else {
                PullToRefreshBox(
                    isRefreshing = refreshing,
                    onRefresh = {
                        scope.launch {
                            refreshing = true
                            loadOk = client.refreshMail(activeQuery)
                            refreshing = false
                        }
                    },
                    modifier = Modifier.fillMaxSize(),
                ) {
                    val today = Clock.System.todayIn(TimeZone.currentSystemDefault())
                    val sections = remember(mail, today) { mailSections(mail, today) }
                    LazyColumn(Modifier.fillMaxSize(), state = listState) {
                        if (!selecting) {
                            if (showSearchField) {
                                item(key = "search") {
                                    DenebSearchField(
                                        query = searchText,
                                        onQueryChange = {
                                            searchText = it
                                            // Clearing the field (✕ or backspace-to-empty) returns to recent mail.
                                            if (it.isBlank() && activeQuery != null) runSearch("")
                                        },
                                        placeholder = "전체 메일 검색 (키워드, from:…)",
                                        onSearch = { runSearch(searchText) },
                                        clearContentDescription = "검색 지우기",
                                        // Field carries its own 8dp inset; 16dp more aligns the pill with
                                        // the screen's 24dp content margins.
                                        modifier = Modifier.padding(horizontal = 16.dp),
                                    )
                                }
                            }
                            if (filtersVisible) {
                                item(key = "native-filters") {
                                    MailNativeFilterRow(
                                        activeQuery = activeQuery,
                                        offlineCapable = offlineCapable,
                                        onPick = { query ->
                                            searchText = query.orEmpty()
                                            runSearch(query.orEmpty())
                                        },
                                    )
                                }
                            }
                        }
                        // Error and empty render inside the list so toolbar search
                        // and filter controls can still be opened after a failure or
                        // empty result.
                        if (mail.isEmpty() && loadOk == false) {
                            item(key = "load-error") {
                                Box(Modifier.fillParentMaxSize(), contentAlignment = Alignment.Center) {
                                    DenebError(
                                        "메일을 불러오지 못했습니다.",
                                        onRetry = {
                                            scope.launch {
                                                loadOk = null
                                                loadOk = client.refreshMail(activeQuery)
                                            }
                                        },
                                    )
                                }
                            }
                        } else if (mail.isEmpty()) {
                            item(key = "empty") {
                                Box(Modifier.padding(horizontal = 24.dp)) {
                                    DenebEmpty(if (activeQuery != null) "검색 결과 없음" else "최근 30일 메일 없음")
                                }
                            }
                        }
                        sections.forEachIndexed { index, section ->
                            item(key = "section-${section.label}") {
                                // The first section sits directly under the screen title when no
                                // search field, filter row, or selection header precedes it. Drop
                                // its 22dp separator there so the title-to-list gap isn't an empty
                                // band — the removed stats header used to fill that space.
                                val firstUnderTitle = index == 0 && !selecting && !showSearchField && !filtersVisible
                                DenebSectionLabel(
                                    section.label,
                                    Modifier.padding(horizontal = 24.dp),
                                    topPadding = if (firstUnderTitle) 6.dp else 22.dp,
                                )
                            }
                            items(section.items, key = { it.id }) { m ->
                                Column(Modifier.animateItem()) {
                                    MailRow(
                                        message = m,
                                        selecting = selecting,
                                        isSelected = m.id in selected,
                                        isCurrent = panelMode && m.id == selectedId,
                                        today = today,
                                        onTap = {
                                            haptics.tap()
                                            if (selecting) {
                                                if (m.id in selected) selected.remove(m.id) else selected.add(m.id)
                                                if (selected.isEmpty()) selecting = false
                                            } else {
                                                onOpenDetail(m.id)
                                            }
                                        },
                                        onLongPress = {
                                            haptics.longPress()
                                            selecting = true
                                            if (m.id !in selected) selected.add(m.id)
                                        },
                                    )
                                    HorizontalDivider(color = denebHairline())
                                }
                            }
                        }
                        if (nextToken != null) {
                            item {
                                Box(Modifier.fillMaxWidth().padding(vertical = 14.dp), contentAlignment = Alignment.Center) {
                                    if (loadingMore) {
                                        CircularProgressIndicator(Modifier.size(22.dp), strokeWidth = 2.dp)
                                    } else {
                                        TextButton(onClick = {
                                            scope.launch {
                                                loadingMore = true
                                                client.loadMoreMail()
                                                loadingMore = false
                                            }
                                        }) { Text("더 보기") }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }

        if (selecting && selected.isNotEmpty()) {
            // Flat action bar in the Deneb idiom: a hairline above, no elevation.
            Column(Modifier.fillMaxWidth()) {
                HorizontalDivider(color = denebHairline())
                Row(
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text("${selected.size}개 선택", style = MaterialTheme.typography.titleSmall)
                    Spacer(Modifier.weight(1f))
                    TextButton(onClick = { bulk { client.markMailRead(it) } }, enabled = !busy) { Text("읽음") }
                    TextButton(onClick = { bulk { client.archiveMail(it) } }, enabled = !busy) { Text("보관") }
                    TextButton(onClick = { bulk { client.trashMail(it) } }, enabled = !busy) { Text("휴지통") }
                }
            }
        }
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
internal fun MailRow(
    message: MailMessage,
    selecting: Boolean,
    isSelected: Boolean,
    onTap: () -> Unit,
    onLongPress: () -> Unit,
    isCurrent: Boolean = false,
    today: LocalDate? = null,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .denebPressable(onClick = onTap, onLongClick = onLongPress)
            .background(
                when {
                    // Multi-select (checkbox) wins; else current-open-in-panel highlight; else none.
                    isSelected -> MaterialTheme.colorScheme.primaryContainer.copy(alpha = 0.5f)

                    isCurrent -> MaterialTheme.colorScheme.primaryContainer.copy(alpha = 0.25f)

                    else -> Color.Transparent
                },
            )
            .padding(horizontal = 24.dp, vertical = 14.dp),
        verticalAlignment = Alignment.Top,
    ) {
        if (selecting) {
            Checkbox(checked = isSelected, onCheckedChange = null, modifier = Modifier.padding(end = 10.dp))
        }
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (message.unread) {
                    Box(Modifier.size(8.dp).clip(CircleShape).background(MaterialTheme.colorScheme.primary))
                    Spacer(Modifier.width(8.dp))
                }
                Text(
                    senderName(message.from).ifBlank { "(발신자 없음)" },
                    style = if (message.unread) DenebType.rowTitleStrong else DenebType.rowTitle,
                    // The row open in the desktop detail pane is the active item: its
                    // title takes the cool interactive accent (just the mark, not the
                    // whole row — the faint background tint already carries the fill).
                    color = if (isCurrent) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onBackground,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                Spacer(Modifier.width(8.dp))
                mailRowAnalysisStatusLabel(message.workState)?.let { status ->
                    Text(
                        status,
                        style = DenebType.meta,
                        color = mailRowAnalysisStatusColor(message.workState),
                        maxLines = 1,
                    )
                    Spacer(Modifier.width(6.dp))
                }
                Text(
                    mailTimeLabel(message.date, today),
                    style = DenebType.meta,
                    color = denebHint(),
                )
            }
            Spacer(Modifier.height(3.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                // Glanceable priority marker from the gateway's heuristic
                // scorer (red = urgent, tertiary = attention); routine mail
                // renders no marker. The signal hint feeds TalkBack.
                val priorityColor = when (message.priority) {
                    "urgent" -> MaterialTheme.colorScheme.error
                    "attention" -> MaterialTheme.colorScheme.tertiary
                    else -> null
                }
                if (priorityColor != null) {
                    val label = if (message.priority == "urgent") "긴급" else "주의"
                    val hint = message.priorityHint.ifBlank { label }
                    Box(
                        Modifier
                            .size(7.dp)
                            .clip(CircleShape)
                            .background(priorityColor)
                            .semantics { contentDescription = "$label: $hint" },
                    )
                    Spacer(Modifier.width(6.dp))
                }
                Text(
                    message.subject.ifBlank { "(제목 없음)" },
                    style = DenebType.rowSubtitle,
                    color = if (message.unread) MaterialTheme.colorScheme.onBackground else denebHint(),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
            }
            mailRowNativeMeta(message)?.let { meta ->
                Spacer(Modifier.height(2.dp))
                Text(
                    meta,
                    style = DenebType.meta,
                    color = mailRowMetaColor(message),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

/** One time bucket of the mail list, newest-first within the original list order. */
internal data class MailSection(val label: String, val items: List<MailMessage>)

/**
 * Buckets the (newest-first) mail list into 오늘 / 어제 / 이번 주 / 이전 sections for
 * the section labels. Unparseable dates land in 이전 so a bad timestamp can
 * never crash or reorder the list. Section order is fixed regardless of the
 * encounter order, so a stray out-of-order item cannot duplicate a header.
 */
internal fun mailSections(mail: List<MailMessage>, today: LocalDate): List<MailSection> {
    val order = listOf("오늘", "어제", "이번 주", "이전")
    val grouped = mail.groupBy { bucketLabel(it.date, today) }
    return order.mapNotNull { label -> grouped[label]?.let { MailSection(label, it) } }
}

private fun bucketLabel(date: String, today: LocalDate): String = runCatching {
    val d = Instant.parse(date).toLocalDateTime(TimeZone.currentSystemDefault()).date
    when {
        d.daysUntil(today) <= 0 -> "오늘"

        // future-dated (clock skew) counts as today
        d.daysUntil(today) == 1 -> "어제"

        d.daysUntil(today) < 7 -> "이번 주"

        else -> "이전"
    }
}.getOrElse { "이전" }

/**
 * Bucket-adaptive timestamp: today shows the clock time, yesterday "어제", the
 * rest of the week a Korean weekday, older mail "MM-dd". Falls back to
 * [shortDate] when the date cannot be parsed or no [today] is supplied.
 */
internal fun mailTimeLabel(date: String, today: LocalDate?): String {
    if (today == null) return shortDate(date)
    return runCatching {
        val t = Instant.parse(date).toLocalDateTime(TimeZone.currentSystemDefault())
        fun p(n: Int) = n.toString().padStart(2, '0')
        when {
            t.date.daysUntil(today) <= 0 -> "${p(t.hour)}:${p(t.minute)}"
            t.date.daysUntil(today) == 1 -> "어제"
            t.date.daysUntil(today) < 7 -> koreanDayOfWeek.getOrElse(t.date.dayOfWeek.ordinal) { "" }
            else -> "${p(t.monthNumber)}-${p(t.dayOfMonth)}"
        }
    }.getOrElse { shortDate(date) }
}

/** "Name <email>" -> "Name"; a bare address is returned as-is. */
private fun senderName(from: String): String {
    val lt = from.indexOf('<')
    return if (lt > 0) from.substring(0, lt).trim().trim('"') else from.trim()
}

internal fun mailRowNativeMeta(message: MailMessage): String? = buildList {
    // Surface the *absence* of a work-feed card, not its presence: fed mail is
    // already handled, so the row instead flags mail the analysis finished
    // without producing a feed (the gateway's feed_missing state — analysis
    // done && feedStatus != created). This highlights what slipped through.
    if (message.workState.analysisStatus == "done" && message.workState.feedStatus != "created") {
        add("피드 없음")
    }
    if (message.workState.calendarProposalCount > 0) {
        add("일정 ${message.workState.calendarProposalCount}")
    }
    if (message.workState.todoCount > 0) {
        add("할 일 ${message.workState.todoCount}")
    }
    mailRowMailboxLabel(message.mailbox)?.let { add(it) }
}.joinToString(" · ").ifBlank { null }

internal fun mailRowAnalysisStatusLabel(state: MailWorkState): String? = when (state.analysisStatus) {
    "failed" -> "실패"
    "analyzing" -> "분석중"
    "queued" -> "대기"
    "stale" -> "재분석"
    "done" -> "분석"
    else -> null
}

@Composable
private fun mailRowMetaColor(message: MailMessage): Color = when {
    // "피드 없음" stays muted (it is an absence, not extracted work); only real
    // signals — calendar proposals or todos — lift the meta line to the accent.
    message.workState.calendarProposalCount > 0 ||
        message.workState.todoCount > 0 -> MaterialTheme.colorScheme.primary

    else -> denebHint()
}

@Composable
private fun mailRowAnalysisStatusColor(state: MailWorkState): Color = when (state.analysisStatus) {
    "failed" -> MaterialTheme.colorScheme.error
    "analyzing", "queued", "stale" -> MaterialTheme.colorScheme.tertiary
    "done" -> MaterialTheme.colorScheme.primary
    else -> denebHint()
}

private fun mailRowMailboxLabel(mailbox: String): String? {
    val normalized = mailbox.trim()
    if (normalized.isBlank() || normalized.equals("INBOX", ignoreCase = true)) return null
    return when {
        normalized.equals("Gmail", ignoreCase = true) -> "Gmail 보관함"
        else -> normalized
    }
}

/**
 * UTC ISO-8601 from the gateway (e.g. "2026-05-30T12:41:31Z") -> local "MM-dd HH:mm".
 * The gateway sends UTC (gmail.go normalizeDate uses t.UTC()), so we MUST convert to the
 * device zone before display — otherwise a 10:49 KST mail showed as 01:49. Raw-substring
 * fallback only if parsing fails.
 */
internal fun shortDate(date: String): String = runCatching {
    val t = Instant.parse(date).toLocalDateTime(TimeZone.currentSystemDefault())
    fun p(n: Int) = n.toString().padStart(2, '0')
    "${p(t.monthNumber)}-${p(t.dayOfMonth)} ${p(t.hour)}:${p(t.minute)}"
}.getOrElse { if (date.length >= 16) date.substring(5, 16).replace('T', ' ') else date }

private data class MailNativeFilter(val label: String, val query: String?)

private fun mailNativeFilters(offlineCapable: Boolean): List<MailNativeFilter> = buildList {
    add(MailNativeFilter(if (offlineCapable) "전체" else "최근", null))
    add(MailNativeFilter("받은함", "in:inbox newer_than:30d"))
    add(MailNativeFilter("오늘", "newer_than:1d"))
    add(MailNativeFilter("첨부", "has:attachment"))
    add(MailNativeFilter("7일", "newer_than:7d"))
    add(MailNativeFilter("분석 실패", "deneb:analysis_failed"))
    add(MailNativeFilter("피드 대기", "deneb:feed_missing"))
    add(MailNativeFilter("일정", "deneb:calendar_candidate"))
    add(MailNativeFilter("할 일", "deneb:todo"))
}

@Composable
private fun MailNativeFilterRow(
    activeQuery: String?,
    offlineCapable: Boolean,
    onPick: (String?) -> Unit,
) {
    val filters = remember(offlineCapable) { mailNativeFilters(offlineCapable) }
    Row(
        Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState())
            .padding(start = 16.dp, end = 16.dp, top = 8.dp, bottom = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        filters.forEachIndexed { index, filter ->
            if (index > 0) Spacer(Modifier.width(8.dp))
            DenebChip(
                selected = activeQuery.normalizeMailQuery() == filter.query.normalizeMailQuery(),
                onClick = { onPick(filter.query) },
            ) {
                Text(filter.label, style = MaterialTheme.typography.labelLarge)
            }
        }
    }
}

private fun String?.normalizeMailQuery(): String? = this?.trim()?.ifBlank { null }

internal fun mailNativeStatusLine(status: MailNativeStatus?): String? {
    status ?: return null
    return when {
        status.source == "archive" && status.available -> {
            val total = status.mailboxes.sumOf { it.total }
            val unread = status.mailboxes.sumOf { it.unread }
            val boxes = status.mailboxes.count { it.total > 0 }.coerceAtLeast(status.mailboxes.size)
            val overlay = status.overlay.archived + status.overlay.trashed
            buildList {
                add("로컬 보관함")
                add("${boxes}개함 ${total}통")
                if (unread > 0) add("미읽음 $unread")
                if (overlay > 0) add("로컬정리 $overlay")
                addAll(mailPipelineStatusParts(status.pipeline))
                if (status.offlineCapable) add("오프라인")
            }.joinToString(" · ")
        }

        status.source == "archive" -> "로컬 보관함 연결 불안정"

        status.source == "gmail" -> "Gmail 대체 경로"

        else -> null
    }
}

private fun mailPipelineStatusParts(pipeline: MailNativePipeline): List<String> = buildList {
    if (pipeline.error.isNotBlank()) add("분석상태 오류")
    if (pipeline.analyzed > 0) add("분석 ${pipeline.analyzed}")
    if (pipeline.analyzing > 0) add("진행 ${pipeline.analyzing}")
    if (pipeline.failed > 0) add("실패 ${pipeline.failed}")
    if (pipeline.feedMissing > 0) add("피드대기 ${pipeline.feedMissing}")
    if (pipeline.calendarCandidates > 0) add("일정 ${pipeline.calendarCandidates}")
    if (pipeline.todoCandidates > 0) add("할일 ${pipeline.todoCandidates}")
}

/** Desktop split-view right-pane placeholder, shown until a mail is selected. */
@Composable
internal fun EmptyMailPanel() {
    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            Text(
                "메일을 선택하세요",
                style = MaterialTheme.typography.bodyMedium,
                color = denebHint(),
            )
        }
    }
}
