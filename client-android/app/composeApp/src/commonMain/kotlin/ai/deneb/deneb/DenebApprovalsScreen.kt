package ai.deneb.deneb

import ai.deneb.deneb.generated.GroupwareApprovalRow
import ai.deneb.ui.DenebFeedApprovalPage
import ai.deneb.ui.DenebFeedApprovalPivots
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebSiblingSwipeHost
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import ai.deneb.ui.handCursor
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch
import kotlinx.datetime.LocalDate
import kotlinx.datetime.TimeZone
import kotlinx.datetime.daysUntil
import kotlinx.datetime.todayIn
import kotlin.time.Clock

/**
 * 최근 전체 결재 surface (`miniapp.groupware.approvals.list`, folder=total).
 * First page from StateFlow (seeded from settings cache); near-end scroll
 * appends via afterDocId. Detail act patches the same flow so 미결 moves
 * without a refetch. Row tap → detail.
 */

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DenebApprovalsScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onOpenDetail: (GroupwareApprovalRow) -> Unit = {},
    onOpenFeed: (() -> Unit)? = null,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    remember {
        client.seedApprovalsFromCache()
        true
    }
    val rows by client.denebApprovals.collectAsState()
    val nextAfter by client.denebApprovalsNextAfter.collectAsState()
    val ready by client.denebApprovalsReady.collectAsState()
    var failed by remember { mutableStateOf(false) }
    var refreshing by remember { mutableStateOf(false) }
    var loadingMore by remember { mutableStateOf(false) }
    val haptics = rememberHaptics()
    val scope = rememberCoroutineScope()
    val listState = rememberLazyListState()
    val openFeed: (() -> Unit)? = onOpenFeed?.let { open ->
        {
            haptics.tap()
            open()
        }
    }

    suspend fun load(forceRefresh: Boolean = false) {
        failed = false
        val fetched = client.fetchApprovals(
            folder = "total",
            limit = APPROVALS_PERSIST_PAGE_SIZE,
            forceRefresh = forceRefresh,
        )
        if (fetched == null) {
            failed = true
        } else {
            fetched.filter { it.canAct }.take(4).forEach { doc ->
                scope.launch { client.fetchApprovalBody(doc.docId, doc.title, doc.folder) }
            }
        }
    }

    suspend fun loadMore() {
        if (nextAfter == null || loadingMore || refreshing) return
        loadingMore = true
        try {
            client.loadMoreApprovals(folder = "total", limit = APPROVALS_PERSIST_PAGE_SIZE)
        } finally {
            loadingMore = false
        }
    }

    LaunchedEffect(Unit) { load() }
    LaunchedEffect(listState, nextAfter) {
        snapshotFlow {
            val layout = listState.layoutInfo
            val lastVisible = layout.visibleItemsInfo.lastOrNull()?.index ?: -1
            layout.totalItemsCount > 0 && lastVisible >= layout.totalItemsCount - 4
        }.distinctUntilChanged().collect { nearEnd ->
            if (nearEnd) loadMore()
        }
    }

    val pendingRows = remember(rows) {
        rows.filter { it.canAct }.sortedByDescending { it.docId }
    }
    val recentRows = remember(rows) {
        rows.filterNot { it.canAct }.sortedByDescending { it.docId }
    }
    val hasMore = nextAfter != null

    DenebSiblingSwipeHost(onSwipeRight = openFeed) {
        DenebScreenScaffold(
            title = "결재",
            onBack = onBack,
            tabBar = navigationTabBar,
            titleContent = {
                DenebFeedApprovalPivots(
                    active = DenebFeedApprovalPage.Approvals,
                    onOpenFeed = openFeed,
                )
            },
        ) {
            PullToRefreshBox(
                isRefreshing = refreshing,
                onRefresh = {
                    haptics.refresh()
                    scope.launch {
                        refreshing = true
                        load(forceRefresh = true)
                        refreshing = false
                    }
                },
                modifier = Modifier.fillMaxWidth().weight(1f),
            ) {
                when {
                    failed && rows.isEmpty() -> DenebError(
                        "결재 목록을 불러오지 못했습니다.",
                        onRetry = { scope.launch { load(forceRefresh = true) } },
                    )

                    !ready && rows.isEmpty() -> DenebLoading()

                    pendingRows.isEmpty() && recentRows.isEmpty() -> Column(
                        Modifier.fillMaxSize().verticalScroll(rememberScrollState()),
                    ) {
                        DenebEmpty("최근 결재 문서가 없습니다")
                    }

                    else -> LazyColumn(Modifier.fillMaxSize(), state = listState) {
                        if (pendingRows.isNotEmpty()) {
                            item { DenebSectionLabel("미결", Modifier.padding(start = 12.dp)) }
                            items(pendingRows, key = { "pending-${it.docId}" }) { doc ->
                                ApprovalRow(
                                    doc = doc,
                                    onOpen = {
                                        haptics.tap()
                                        onOpenDetail(doc)
                                    },
                                )
                                HorizontalDivider(color = denebHairline(), thickness = 0.5.dp)
                            }
                        }
                        if (recentRows.isNotEmpty()) {
                            if (pendingRows.isNotEmpty()) {
                                item { DenebSectionLabel("최근", Modifier.padding(start = 12.dp)) }
                            }
                            items(recentRows, key = { "recent-${it.docId}" }) { doc ->
                                ApprovalRow(
                                    doc = doc,
                                    onOpen = {
                                        haptics.tap()
                                        onOpenDetail(doc)
                                    },
                                )
                                HorizontalDivider(color = denebHairline(), thickness = 0.5.dp)
                            }
                        }
                        if (hasMore || loadingMore) {
                            item {
                                Box(
                                    Modifier.fillMaxWidth().padding(vertical = 14.dp),
                                    contentAlignment = Alignment.Center,
                                ) {
                                    if (loadingMore) {
                                        CircularProgressIndicator(Modifier.size(22.dp), strokeWidth = 2.dp)
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun ApprovalRow(
    doc: GroupwareApprovalRow,
    onOpen: () -> Unit,
) {
    Column(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onOpen, onClickLabel = "결재 상세", role = Role.Button)
            .handCursor()
            .padding(horizontal = 16.dp, vertical = 12.dp),
    ) {
        Text(
            text = doc.title.ifBlank { "(제목 없음)" },
            style = DenebType.rowTitle.copy(
                fontWeight = if (doc.canAct) FontWeight.SemiBold else FontWeight.Normal,
            ),
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
        Spacer(Modifier.height(4.dp))
        val meta = listOfNotNull(
            doc.drafter.takeIf { it.isNotBlank() }?.let { "기안 $it" },
            doc.date.takeIf { it.isNotBlank() },
            doc.docNo.takeIf { it.isNotBlank() },
        ).joinToString(" · ")
        val age = remember(doc.date) { approvalAgeDays(doc.date) }
        Row(verticalAlignment = Alignment.CenterVertically) {
            if (meta.isNotBlank()) {
                Text(text = meta, style = DenebType.meta, color = denebHint())
            }
            // Elapsed time, not just the drafting date. On an approval queue the
            // delay IS the risk, and an absolute date alone renders a document
            // that has sat for ten days identically to yesterday's. Every other
            // list in the app already speaks in elapsed time ("9시간 전"); this
            // one only spoke in calendar dates.
            val label = approvalAgeLabel(age)
            if (label != null) {
                Text(
                    text = " · $label",
                    style = DenebType.meta,
                    // A document that is BOTH stale and yours to act on is the one
                    // worth the accent — same warm accent the feed's urgent marker
                    // uses. Stale-but-not-actionable stays quiet.
                    color = if (doc.canAct && age != null && age >= APPROVAL_STALE_DAYS) {
                        denebInsight()
                    } else {
                        denebHint()
                    },
                )
            }
        }
    }
}

/** A document sitting this long is worth calling out when it is yours to act on. */
internal const val APPROVAL_STALE_DAYS = 7

/**
 * Days elapsed since the drafting date, or null when the date cannot be read.
 *
 * The gateway hands this field through verbatim from Amaranth and the format
 * DEPENDS ON THE FOLDER — measured 2026-07-26, the same RPC returns
 * "2026-07-24 (금)" for folder=total and "26.07.24" for folder=pending. Both are
 * parsed; anything else yields null and the row simply shows no age rather than
 * a wrong one.
 */
internal fun approvalAgeDays(raw: String, today: LocalDate? = null): Int? {
    val date = parseApprovalDate(raw) ?: return null
    val now = today ?: Clock.System.todayIn(TimeZone.currentSystemDefault())
    val days = date.daysUntil(now)
    return if (days >= 0) days else null
}

private fun parseApprovalDate(raw: String): LocalDate? {
    val s = raw.trim()
    if (s.isEmpty()) return null
    // "2026-07-24 (금)" / "2026-07-24"
    Regex("""^(\d{4})-(\d{2})-(\d{2})""").find(s)?.let { m ->
        val (y, mo, d) = m.destructured
        return runCatching { LocalDate(y.toInt(), mo.toInt(), d.toInt()) }.getOrNull()
    }
    // "26.07.24" — two-digit year, this century.
    Regex("""^(\d{2})\.(\d{2})\.(\d{2})$""").find(s)?.let { m ->
        val (y, mo, d) = m.destructured
        return runCatching { LocalDate(2000 + y.toInt(), mo.toInt(), d.toInt()) }.getOrNull()
    }
    return null
}

/** "오늘" is not worth a word; anything older is. Null = show nothing. */
internal fun approvalAgeLabel(days: Int?): String? = when {
    days == null || days <= 0 -> null
    days == 1 -> "어제"
    else -> "${days}일 경과"
}
