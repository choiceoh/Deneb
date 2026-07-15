package ai.deneb.deneb

import ai.deneb.deneb.generated.GroupwareApprovalRow
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.handCursor
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowLeft
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
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
 * Day-pager like 피드/메일: ← [날짜] → filters the snapshot to one local day;
 * 미결 rows offer 승인/반려 (operator mutate path).
 */
@Composable
fun DenebApprovalsScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var rows by remember { mutableStateOf<List<GroupwareApprovalRow>?>(null) }
    var failed by remember { mutableStateOf(false) }
    var acting by remember { mutableStateOf(false) }
    var pendingAct by remember { mutableStateOf<Pair<GroupwareApprovalRow, String>?>(null) }
    val today = remember { Clock.System.todayIn(TimeZone.currentSystemDefault()) }
    var selectedDate by remember { mutableStateOf(today) }
    val haptics = rememberHaptics()
    val scope = rememberCoroutineScope()

    suspend fun load() {
        failed = false
        rows = null
        val fetched = client.fetchApprovals(folder = "total", limit = 100)
        if (fetched == null) failed = true else rows = fetched
    }
    LaunchedEffect(Unit) { load() }

    val list = rows
    val dayRows = remember(list, selectedDate) {
        list?.filter { approvalLocalDate(it.date) == selectedDate }.orEmpty()
    }
    val minDate = today.minus(APPROVALS_LOOKBACK_DAYS, DateTimeUnit.DAY)
    val canPrev = selectedDate > minDate
    val canNext = selectedDate < today

    DenebScreenScaffold(title = "결재", onBack = onBack, tabBar = navigationTabBar) {
        ApprovalsDateBar(
            label = approvalsDateLabel(selectedDate, today),
            canGoPrev = canPrev,
            canGoNext = canNext,
            onPrev = { if (canPrev) selectedDate = selectedDate.minus(1, DateTimeUnit.DAY) },
            onNext = { if (canNext) selectedDate = selectedDate.plus(1, DateTimeUnit.DAY) },
        )
        when {
            failed -> DenebError(
                "결재 목록을 불러오지 못했습니다.",
                onRetry = { scope.launch { load() } },
            )

            list == null -> DenebLoading()

            dayRows.isEmpty() -> DenebEmpty(approvalsEmptyLabel(selectedDate, today))

            else -> LazyColumn(Modifier.fillMaxWidth().weight(1f)) {
                items(dayRows, key = { it.docId }) { doc ->
                    ApprovalRow(
                        doc = doc,
                        acting = acting,
                        onApprove = {
                            haptics.tap()
                            pendingAct = doc to "approve"
                        },
                        onReject = {
                            haptics.tap()
                            pendingAct = doc to "reject"
                        },
                    )
                    HorizontalDivider(color = denebHairline(), thickness = 0.5.dp)
                }
            }
        }
    }

    pendingAct?.let { (doc, decision) ->
        val label = if (decision == "approve") "승인" else "반려"
        AlertDialog(
            onDismissRequest = { if (!acting) pendingAct = null },
            title = { Text("${label}할까요?") },
            text = {
                Text(
                    buildString {
                        append(doc.title.ifBlank { "이 결재 문서" })
                        if (doc.docId.isNotBlank()) append(" (doc ${doc.docId})")
                        append("\n그룹웨어에 즉시 반영됩니다.")
                    },
                )
            },
            confirmButton = {
                TextButton(
                    enabled = !acting,
                    onClick = {
                        scope.launch {
                            acting = true
                            val ok = client.actApproval(doc.docId, decision)?.ok == true
                            acting = false
                            pendingAct = null
                            if (ok) load()
                        }
                    },
                ) { Text(label) }
            },
            dismissButton = {
                TextButton(enabled = !acting, onClick = { pendingAct = null }) {
                    Text("취소")
                }
            },
        )
    }
}

@Composable
private fun ApprovalsDateBar(
    label: String,
    canGoPrev: Boolean,
    canGoNext: Boolean,
    onPrev: () -> Unit,
    onNext: () -> Unit,
) {
    Row(
        Modifier.fillMaxWidth().padding(start = 12.dp, end = 12.dp, top = 0.dp, bottom = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        ApprovalsDateArrow(Icons.AutoMirrored.Filled.KeyboardArrowLeft, "이전 날", canGoPrev, onPrev)
        Text(
            text = label,
            style = DenebType.rowTitle,
            color = MaterialTheme.colorScheme.onBackground,
            textAlign = TextAlign.Center,
            modifier = Modifier.weight(1f),
        )
        ApprovalsDateArrow(Icons.AutoMirrored.Filled.KeyboardArrowRight, "다음 날", canGoNext, onNext)
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

@Composable
private fun ApprovalRow(
    doc: GroupwareApprovalRow,
    acting: Boolean,
    onApprove: () -> Unit,
    onReject: () -> Unit,
) {
    Column(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 12.dp),
    ) {
        Text(
            text = doc.title.ifBlank { "(제목 없음)" },
            style = DenebType.rowTitle,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
        Spacer(Modifier.height(2.dp))
        val meta = listOfNotNull(
            doc.drafter.takeIf { it.isNotBlank() }?.let { "기안 $it" },
            doc.status.takeIf { it.isNotBlank() },
            doc.docNo.takeIf { it.isNotBlank() },
        ).joinToString(" · ")
        if (meta.isNotBlank()) {
            Text(text = meta, style = DenebType.meta, color = denebHint())
        }
        if (doc.canAct) {
            Spacer(Modifier.height(8.dp))
            Row {
                TextButton(enabled = !acting, onClick = onApprove) {
                    Text("승인")
                }
                TextButton(enabled = !acting, onClick = onReject) {
                    Text("반려")
                }
            }
        }
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
