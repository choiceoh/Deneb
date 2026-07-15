package ai.deneb.deneb

import ai.deneb.deneb.generated.GroupwareApprovalRow
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebTitlePivot
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.handCursor
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowLeft
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import kotlinx.datetime.DateTimeUnit
import kotlinx.datetime.LocalDate
import kotlinx.datetime.TimeZone
import kotlinx.datetime.minus
import kotlinx.datetime.plus
import kotlinx.datetime.todayIn
import kotlin.time.Clock

private const val APPROVALS_LOOKBACK_DAYS = 31

private val koreanWeekday = listOf("월", "화", "수", "목", "금", "토", "일")

/**
 * 최근 전체 결재 surface (`miniapp.groupware.approvals.list`, folder=total).
 * Day-pager like 피드/메일; 미결 우선 정렬·배지; 미결만 모아보기; 행 탭 → 상세.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DenebApprovalsScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onOpenDetail: (GroupwareApprovalRow) -> Unit = {},
    onOpenFeed: (() -> Unit)? = null,
    onOpenMail: (() -> Unit)? = null,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var rows by remember { mutableStateOf<List<GroupwareApprovalRow>?>(null) }
    var failed by remember { mutableStateOf(false) }
    val today = remember { Clock.System.todayIn(TimeZone.currentSystemDefault()) }
    var selectedDate by remember { mutableStateOf(today) }
    var pendingOnly by remember { mutableStateOf(false) }
    var refreshing by remember { mutableStateOf(false) }
    val haptics = rememberHaptics()
    val scope = rememberCoroutineScope()

    suspend fun load(clear: Boolean = true) {
        failed = false
        if (clear) rows = null
        val fetched = client.fetchApprovals(folder = "total", limit = 100)
        if (fetched == null) {
            failed = true
        } else {
            rows = fetched
        }
    }
    LaunchedEffect(Unit) { load() }

    val list = rows
    // 미결만: cross-day inbox of everything still awaiting my 승인/반려 — a pending
    // doc from days ago must not hide behind the day pager.
    val pendingRows = remember(list) {
        list?.filter { it.canAct }?.sortedByDescending { it.docId }.orEmpty()
    }
    val dayRows = remember(list, selectedDate) {
        list
            ?.filter { approvalLocalDate(it.date) == selectedDate }
            ?.sortedWith(
                compareByDescending<GroupwareApprovalRow> { it.canAct }
                    .thenByDescending { it.docId },
            )
            .orEmpty()
    }
    val shownRows = if (pendingOnly) pendingRows else dayRows
    val pendingCount = dayRows.count { it.canAct }
    val minDate = today.minus(APPROVALS_LOOKBACK_DAYS, DateTimeUnit.DAY)
    val canPrev = selectedDate > minDate
    val canNext = selectedDate < today

    DenebScreenScaffold(
        title = "결재",
        onBack = onBack,
        tabBar = navigationTabBar,
        titlePivot = {
            onOpenFeed?.let { DenebTitlePivot("피드", onClick = it) }
            onOpenMail?.let { DenebTitlePivot("메일", onClick = it) }
        },
    ) {
        if (!pendingOnly) {
            ApprovalsDateBar(
                label = approvalsDateLabel(selectedDate, today),
                countLabel = when {
                    dayRows.isEmpty() -> null
                    pendingCount > 0 -> "${dayRows.size}건 · 미결 $pendingCount"
                    else -> "${dayRows.size}건"
                },
                canGoPrev = canPrev,
                canGoNext = canNext,
                showToday = selectedDate != today,
                onPrev = { if (canPrev) selectedDate = selectedDate.minus(1, DateTimeUnit.DAY) },
                onNext = { if (canNext) selectedDate = selectedDate.plus(1, DateTimeUnit.DAY) },
                onToday = { selectedDate = today },
            )
        }
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 2.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            PendingOnlyToggle(
                count = pendingRows.size,
                active = pendingOnly,
                onToggle = {
                    haptics.tap()
                    pendingOnly = !pendingOnly
                },
            )
            if (pendingOnly) {
                Text("전체 기간의 미결 문서", style = DenebType.meta, color = denebHint())
            }
        }
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = {
                haptics.refresh()
                scope.launch {
                    refreshing = true
                    load(clear = false)
                    refreshing = false
                }
            },
            modifier = Modifier.fillMaxWidth().weight(1f),
        ) {
            when {
                failed -> DenebError(
                    "결재 목록을 불러오지 못했습니다.",
                    onRetry = { scope.launch { load() } },
                )

                list == null -> DenebLoading()

                shownRows.isEmpty() -> Column(
                    Modifier.fillMaxSize().verticalScroll(rememberScrollState()),
                ) {
                    DenebEmpty(
                        if (pendingOnly) "미결 문서가 없습니다" else approvalsEmptyLabel(selectedDate, today),
                    )
                }

                else -> LazyColumn(Modifier.fillMaxSize()) {
                    items(shownRows, key = { it.docId }) { doc ->
                        ApprovalRow(
                            doc = doc,
                            showDate = pendingOnly,
                            onOpen = {
                                haptics.tap()
                                onOpenDetail(doc)
                            },
                        )
                        HorizontalDivider(color = denebHairline(), thickness = 0.5.dp)
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
    showDate: Boolean = false,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onOpen, onClickLabel = "결재 상세", role = Role.Button)
            .handCursor()
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.Top,
    ) {
        Column(Modifier.weight(1f)) {
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
                doc.date.takeIf { showDate && it.isNotBlank() },
                doc.docNo.takeIf { it.isNotBlank() },
            ).joinToString(" · ")
            if (meta.isNotBlank()) {
                Text(text = meta, style = DenebType.meta, color = denebHint())
            }
        }
        Spacer(Modifier.width(10.dp))
        ApprovalStatusBadge(
            label = when {
                doc.canAct -> "미결"
                doc.status.isNotBlank() -> doc.status
                else -> "—"
            },
            pending = doc.canAct,
        )
    }
}

// Quiet inline filter — meta-sized text pill, not a full chip (a bordered 48dp
// chip shouted over the list). Active = tinted primary pill, inactive = hint text.
@Composable
private fun PendingOnlyToggle(count: Int, active: Boolean, onToggle: () -> Unit) {
    Text(
        text = if (count > 0) "미결만 $count" else "미결만",
        style = DenebType.meta.copy(fontWeight = if (active) FontWeight.SemiBold else FontWeight.Normal),
        color = if (active) MaterialTheme.colorScheme.primary else denebHint(),
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(
                if (active) MaterialTheme.colorScheme.primary.copy(alpha = 0.12f) else Color.Transparent,
            )
            .clickable(
                onClickLabel = if (active) "전체 보기" else "미결만 보기",
                role = Role.Button,
                onClick = onToggle,
            )
            .handCursor()
            .padding(horizontal = 10.dp, vertical = 5.dp),
    )
}

@Composable
private fun ApprovalStatusBadge(label: String, pending: Boolean) {
    val bg = if (pending) {
        MaterialTheme.colorScheme.primary.copy(alpha = 0.14f)
    } else {
        MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.7f)
    }
    val fg = if (pending) {
        MaterialTheme.colorScheme.primary
    } else {
        denebHint()
    }
    Text(
        text = label,
        style = DenebType.meta.copy(fontWeight = if (pending) FontWeight.SemiBold else FontWeight.Normal),
        color = fg,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(bg)
            .padding(horizontal = 8.dp, vertical = 3.dp),
    )
}

@Composable
private fun ApprovalsDateBar(
    label: String,
    countLabel: String?,
    canGoPrev: Boolean,
    canGoNext: Boolean,
    showToday: Boolean,
    onPrev: () -> Unit,
    onNext: () -> Unit,
    onToday: () -> Unit,
) {
    Column(Modifier.fillMaxWidth().padding(bottom = 4.dp)) {
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            ApprovalsDateArrow(Icons.AutoMirrored.Filled.KeyboardArrowLeft, "이전 날", canGoPrev, onPrev)
            Column(Modifier.weight(1f), horizontalAlignment = Alignment.CenterHorizontally) {
                Text(
                    text = label,
                    style = DenebType.rowTitle,
                    color = MaterialTheme.colorScheme.onBackground,
                    textAlign = TextAlign.Center,
                )
                if (!countLabel.isNullOrBlank()) {
                    Text(countLabel, style = DenebType.meta, color = denebHint())
                }
            }
            ApprovalsDateArrow(Icons.AutoMirrored.Filled.KeyboardArrowRight, "다음 날", canGoNext, onNext)
        }
        if (showToday) {
            TextButton(
                onClick = onToday,
                modifier = Modifier.align(Alignment.CenterHorizontally),
            ) { Text("오늘로") }
        }
    }
}

@Composable
private fun ApprovalsDateArrow(
    icon: ImageVector,
    label: String,
    enabled: Boolean,
    onClick: () -> Unit,
) {
    Box(
        modifier = Modifier
            .size(width = 40.dp, height = 32.dp)
            .clickable(enabled = enabled, onClickLabel = label, role = Role.Button, onClick = onClick)
            .handCursor(),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = icon,
            contentDescription = label,
            tint = if (enabled) denebHint() else denebHint().copy(alpha = 0.25f),
            modifier = Modifier.size(22.dp),
        )
    }
}

/** Parse Amaranth date stamps (2026-07-16 / 2026.07.16 / 20260716) to a LocalDate. */
internal fun approvalLocalDate(raw: String): LocalDate? {
    val s = raw.trim()
    if (s.isEmpty()) return null
    val digits = s.filter { it.isDigit() }
    if (digits.length < 8) return null
    return runCatching {
        LocalDate(
            digits.substring(0, 4).toInt(),
            digits.substring(4, 6).toInt(),
            digits.substring(6, 8).toInt(),
        )
    }.getOrNull()
}

private fun approvalsDateLabel(date: LocalDate, today: LocalDate): String {
    val dow = koreanWeekday.getOrElse(date.dayOfWeek.ordinal) { "" }
    val md = "${date.month.ordinal + 1}월 ${date.day}일 ($dow)"
    return when (date) {
        today -> "오늘 · $md"
        today.minus(1, DateTimeUnit.DAY) -> "어제 · $md"
        else -> md
    }
}

private fun approvalsEmptyLabel(date: LocalDate, today: LocalDate): String = when (date) {
    today -> "오늘 결재 문서가 없습니다"
    today.minus(1, DateTimeUnit.DAY) -> "어제 결재 문서가 없습니다"
    else -> "이 날 결재 문서가 없습니다"
}
