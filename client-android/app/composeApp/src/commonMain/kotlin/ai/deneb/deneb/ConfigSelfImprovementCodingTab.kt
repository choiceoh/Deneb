package ai.deneb.deneb

import ai.deneb.deneb.generated.SelfCorrectionCandidate
import ai.deneb.deneb.generated.SelfImprovementCodingFunnel
import ai.deneb.deneb.generated.SelfImprovementCodingListResponse
import ai.deneb.deneb.generated.SelfImprovementCodingStatusCount
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import ai.deneb.ui.denebInsightContainer
import ai.deneb.ui.handCursor
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import kotlin.time.Clock

@Composable
internal fun SelfImprovementCodingTab(client: DenebGatewayClient) {
    val scope = rememberCoroutineScope()
    var selectedStatus by rememberSaveable { mutableStateOf(selfImprovementCodingDefaultFilter) }
    var queue by remember { mutableStateOf<SelfImprovementCodingListResponse?>(null) }
    var loadFailed by remember { mutableStateOf(false) }

    suspend fun reload(status: String = selectedStatus) {
        loadFailed = false
        val fetched = client.fetchSelfImprovementCodingQueue(status = status)
        queue = fetched
        if (fetched == null) loadFailed = true
    }

    LaunchedEffect(selectedStatus) { reload(selectedStatus) }

    when {
        loadFailed && queue == null -> DenebError(
            "자가개선 코딩 후보를 불러오지 못했습니다.",
            onRetry = { scope.launch { reload() } },
        )

        queue == null -> DenebLoading()

        else -> SelfImprovementCodingContent(
            queue = queue ?: SelfImprovementCodingListResponse(),
            selectedStatus = selectedStatus,
            onSelectStatus = { status ->
                if (selectedStatus != status) {
                    selectedStatus = status
                }
            },
        )
    }
}

@Composable
internal fun SelfImprovementCodingContent(
    queue: SelfImprovementCodingListResponse,
    selectedStatus: String = selfImprovementCodingDefaultStatus,
    onSelectStatus: (String) -> Unit = {},
    // Frozen by the preview renderer so the relative timestamps below stay put; see
    // lifecycleTime.
    nowMs: Long = Clock.System.now().toEpochMilliseconds(),
) {
    val candidates = queue.candidates
    val pendingCount = if (queue.statusCounts.isEmpty() && selectedStatus == selfImprovementCodingDefaultStatus) {
        candidates.size
    } else {
        selfImprovementCodingStatusCount(queue.statusCounts, selfImprovementCodingDefaultStatus)
    }
    val totalCount = if (queue.statusCounts.isEmpty()) {
        candidates.size
    } else {
        selfImprovementCodingStatusCount(queue.statusCounts, "all")
    }
    LazyColumn(Modifier.fillMaxSize()) {
        item {
            // No inline page title: DenebConfigScreen's scaffold already shows this
            // section's name in the top bar, and repeating it here in the heaviest
            // token made the label outweigh the status it labels (ADR 0007 원리 1).
            Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp)) {
                Text(
                    if (pendingCount > 0) {
                        "대기 ${pendingCount}건 — 다음 하트비트가 자동 검토합니다 · 전체 ${totalCount}건"
                    } else {
                        "처리 ${totalCount}건 · 새 후보는 하트비트가 자동 검토·적용합니다"
                    },
                    style = DenebType.meta,
                    color = denebHint(),
                )
                val funnelLine = selfImprovementCodingFunnelLine(queue.funnel, nowMs)
                if (funnelLine.isNotBlank()) {
                    Spacer(Modifier.height(2.dp))
                    Text(funnelLine, style = DenebType.meta, color = denebHint())
                }
            }
            SelfImprovementCodingStatusFilters(
                counts = queue.statusCounts,
                selectedStatus = selectedStatus,
                onSelectStatus = onSelectStatus,
            )
            HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
        }
        if (candidates.isEmpty()) {
            item {
                Text(
                    selfImprovementCodingEmptyText(selectedStatus),
                    style = DenebType.rowSubtitle,
                    color = denebHint(),
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 24.dp),
                )
            }
        } else {
            // Pending first (there is rarely more than one), then newest-processed.
            val ordered = candidates.sortedWith(
                compareByDescending<SelfCorrectionCandidate> { it.status == "proposed" }
                    .thenByDescending { it.updatedAt },
            )
            items(ordered, key = { it.id }) { candidate ->
                SelfImprovementCodingCandidateRow(candidate, nowMs)
                HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
            }
        }
    }
}

@Composable
private fun SelfImprovementCodingStatusFilters(
    counts: List<SelfImprovementCodingStatusCount>,
    selectedStatus: String,
    onSelectStatus: (String) -> Unit,
) {
    val haptics = rememberHaptics()
    val countByStatus = counts.associate { it.status to it.count }
    Row(
        Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState())
            .padding(start = 16.dp, end = 16.dp, top = 6.dp, bottom = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        selfImprovementCodingFilters
            .filter { filter ->
                filter.status == selfImprovementCodingDefaultStatus ||
                    filter.status == "all" ||
                    filter.status == selectedStatus ||
                    (countByStatus[filter.status] ?: 0) > 0
            }
            .forEach { filter ->
                val selected = selectedStatus == filter.status
                val count = countByStatus[filter.status] ?: 0
                Text(
                    "${filter.label} $count",
                    style = DenebType.rowTitle,
                    fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal,
                    color = if (selected) MaterialTheme.colorScheme.primary else denebHint(),
                    modifier = Modifier
                        .handCursor()
                        .selectable(
                            selected = selected,
                            role = Role.Tab,
                            onClick = {
                                if (!selected) {
                                    haptics.tap()
                                    onSelectStatus(filter.status)
                                }
                            },
                        )
                        .padding(horizontal = 2.dp, vertical = 6.dp),
                )
            }
    }
}

@Composable
private fun SelfImprovementCodingCandidateRow(candidate: SelfCorrectionCandidate, nowMs: Long) {
    val haptics = rememberHaptics()
    var expanded by rememberSaveable(candidate.id) { mutableStateOf(false) }
    Column(
        Modifier
            .fillMaxWidth()
            .handCursor()
            .clickable {
                haptics.tap()
                expanded = !expanded
            }
            .padding(horizontal = 16.dp, vertical = 12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            SelfImprovementCodingStatusBadge(candidate.status)
            Spacer(Modifier.width(8.dp))
            Text(
                selfImprovementCodingTitle(candidate),
                style = DenebType.rowTitleStrong,
                color = MaterialTheme.colorScheme.onSurface,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Text(lifecycleTime(candidate.updatedAt, nowMs), style = DenebType.meta, color = denebHint())
        }
        SelfImprovementCodingChips(candidate)
        val primary = candidate.proposedChange.ifBlank { candidate.candidate }
        if (primary.isNotBlank()) {
            Spacer(Modifier.height(2.dp))
            Text(
                primary,
                style = DenebType.rowSubtitle,
                color = denebHint(),
                maxLines = if (expanded) Int.MAX_VALUE else 3,
                overflow = TextOverflow.Ellipsis,
            )
        }
        val outcome = candidate.reviewNote.takeIf { it.isNotBlank() && candidate.status != "proposed" }
        if (outcome != null) {
            Spacer(Modifier.height(2.dp))
            Text(
                "결과: $outcome",
                style = DenebType.rowSubtitle,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = if (expanded) Int.MAX_VALUE else 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
        if (expanded) {
            val detailLines = listOfNotNull(
                candidate.reviewer.takeIf { it.isNotBlank() }?.let { "처리: $it" },
                candidate.evidenceKinds.takeIf { it.isNotEmpty() }
                    ?.joinToString(" · ") { selfImprovementCodingEvidenceLabel(it) }
                    ?.let { "증거 상태: $it" },
                candidate.reviewActions.takeIf { it.isNotEmpty() }
                    ?.joinToString(" · ") { selfImprovementCodingActionLabel(it) }
                    ?.let { "검토 순서: $it" },
                candidate.evidence.takeIf { it.isNotBlank() }?.let { "근거: $it" },
                candidate.risk.takeIf { it.isNotBlank() }?.let { "리스크: $it" },
                candidate.reason.takeIf { it.isNotBlank() }?.let { "메모: $it" },
                candidate.source.takeIf { it.isNotBlank() }?.let { "출처: $it" },
                candidate.targetFiles.takeIf { it.isNotEmpty() }?.joinToString(" · ")?.let { "대상: $it" },
                selfImprovementCodingMeta(candidate).takeIf { it.isNotBlank() },
            )
            detailLines.forEach { line ->
                Spacer(Modifier.height(6.dp))
                Text(line, style = DenebType.rowSubtitle, color = denebHint())
            }
        }
    }
}

/** Provenance + dispatch-track chips shown at a glance (no expand needed): WHICH
 *  miner surfaced the candidate, and whether its source is graduated onto the
 *  auto-dispatch track (자동수리 — auto-implements + lands through the gates) or
 *  staged for review (검토 대기). 자동수리 uses the cool interaction accent (it
 *  will act); the source chip stays neutral. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun SelfImprovementCodingChips(candidate: SelfCorrectionCandidate) {
    val sourceLabel = selfImprovementCodingSourceLabel(candidate.source)
    val showTrack = candidate.scope == "code"
    if (sourceLabel == null && !showTrack) return
    Spacer(Modifier.height(6.dp))
    FlowRow(
        Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        if (sourceLabel != null) {
            CodingChip(sourceLabel, MaterialTheme.colorScheme.surfaceVariant, denebHint())
        }
        if (showTrack) {
            if (candidate.autoDispatch) {
                CodingChip("자동수리", MaterialTheme.colorScheme.primaryContainer, MaterialTheme.colorScheme.onPrimaryContainer)
            } else {
                CodingChip("검토 대기", MaterialTheme.colorScheme.surfaceVariant, denebHint())
            }
        }
    }
}

/** Maps a candidate `source` namespace to a short Korean label. Returns null for
 *  a blank source. Suffix-aware for tool-quality (:desc description / :latency
 *  perf). Keep in sync with the miner source prefixes (the scripts/audit
 *  *_miner.py miners, genesis L4 sources). */
private fun selfImprovementCodingSourceLabel(source: String): String? {
    val s = source.trim()
    if (s.isEmpty()) return null
    return when {
        s.startsWith("runtime-error") -> "런타임 오류"
        s.startsWith("health-finding:runtime-") -> "런타임 건강"
        s.startsWith("health-finding") -> "코드 건강"
        s.startsWith("deadcode-finding") -> "죽은 코드"
        s.endsWith(":latency") && s.startsWith("tool-quality") -> "도구 지연"
        s.startsWith("tool-quality") -> "도구 설명"
        s.startsWith("evolve-tool-gap") -> "도구 갭"
        s.startsWith("self-harness") -> "하네스"
        s.startsWith("sop-mining") -> "SOP"
        else -> s.substringBefore(':').ifBlank { s }
    }
}

@Composable
private fun CodingChip(text: String, bg: Color, fg: Color) {
    Text(
        text = text,
        style = DenebType.meta,
        color = fg,
        modifier = Modifier
            .background(bg, RoundedCornerShape(6.dp))
            .padding(horizontal = 8.dp, vertical = 3.dp),
    )
}

@Composable
private fun SelfImprovementCodingStatusBadge(status: String) {
    val (bg, fg) = when (status) {
        "accepted", "applied" -> MaterialTheme.colorScheme.primaryContainer to MaterialTheme.colorScheme.onPrimaryContainer
        "rejected", "superseded" -> MaterialTheme.colorScheme.errorContainer to MaterialTheme.colorScheme.onErrorContainer
        else -> denebInsightContainer() to denebInsight()
    }
    Text(
        selfImprovementCodingStatusLabel(status),
        style = DenebType.meta,
        color = fg,
        modifier = Modifier
            .clip(RoundedCornerShape(4.dp))
            .background(bg)
            .padding(horizontal = 6.dp, vertical = 1.dp),
    )
}

/** Capture-side health line: distinguishes "queue consumed" from "capture
 *  broke" — last capture, how many recent rejections even qualified for
 *  promotion, and when the heartbeat review lane last ran. Blank when the
 *  pipeline has no history at all (fresh install). */
private fun selfImprovementCodingFunnelLine(funnel: SelfImprovementCodingFunnel, nowMs: Long): String {
    val noHistory = funnel.lastCaptureAt <= 0L && funnel.lastRejectionAt <= 0L &&
        funnel.rejections7d <= 0 && funnel.lastNudgeAt <= 0L
    if (noHistory) return ""
    val parts = mutableListOf<String>()
    parts += if (funnel.lastCaptureAt > 0L) {
        "후보 포착 ${lifecycleTime(funnel.lastCaptureAt, nowMs)}"
    } else {
        "후보 포착 이력 없음"
    }
    parts += "7일 거절 ${funnel.rejections7d}건(승격자격 ${funnel.promotableRejections7d})"
    if (funnel.rejections7d <= 0 && funnel.lastRejectionAt > 0L) {
        parts += "마지막 거절 ${lifecycleTime(funnel.lastRejectionAt, nowMs)}"
    }
    parts += if (funnel.lastNudgeAt > 0L) {
        "하트비트 검토 ${lifecycleTime(funnel.lastNudgeAt, nowMs)}"
    } else {
        "하트비트 검토 이력 없음"
    }
    return parts.joinToString(" · ")
}

private fun selfImprovementCodingTitle(candidate: SelfCorrectionCandidate): String = candidate.title.ifBlank {
    candidate.proposedChange.ifBlank {
        candidate.candidate.ifBlank { candidate.id.ifBlank { "(후보)" } }
    }
}

private fun selfImprovementCodingMeta(candidate: SelfCorrectionCandidate): String = listOfNotNull(
    candidate.scope.takeIf { it.isNotBlank() }?.let { selfImprovementCodingScopeLabel(it) },
    candidate.skillName.takeIf { it.isNotBlank() }?.let { "스킬 $it" },
    candidate.sessionKey.takeIf { it.isNotBlank() }?.let { "세션 $it" },
    lifecycleDateTime(candidate.createdAt).takeIf { it.isNotBlank() },
    candidate.id.takeIf { it.isNotBlank() }?.let { "ID $it" },
).joinToString(" · ")

private fun selfImprovementCodingStatusLabel(status: String): String = when (status) {
    "proposed" -> "대기"
    "accepted" -> "채택"
    "rejected" -> "기각"
    "superseded" -> "대체"
    "applied" -> "적용"
    else -> "후보"
}

private fun selfImprovementCodingScopeLabel(scope: String): String = when (scope) {
    "skill" -> "스킬"
    "code" -> "코드"
    "prompt" -> "프롬프트"
    "docs" -> "문서"
    "ops" -> "운영"
    "config" -> "설정"
    "test" -> "테스트"
    else -> scope
}

private fun selfImprovementCodingEvidenceLabel(kind: String): String = when (kind) {
    "session" -> "세션"
    "evidence" -> "근거"
    "target_files" -> "대상 파일"
    "risk" -> "리스크"
    "review" -> "리뷰"
    "needs_evidence" -> "근거 필요"
    else -> kind
}

private fun selfImprovementCodingActionLabel(action: String): String = when (action) {
    "open_session" -> "세션 확인"
    "inspect_target_files" -> "파일 확인"
    "add_evidence" -> "근거 보강"
    "assess_risk" -> "리스크 판단"
    "run_focused_validation" -> "집중 검증"
    "mark_review_status" -> "상태 기록"
    else -> action
}

private const val selfImprovementCodingDefaultStatus = "proposed"

// Default view since the heartbeat self-coding lane (#3177): the queue is
// consumed automatically, so the screen's job is the processing HISTORY —
// what the loop noticed and what was done about it — not a to-do list.
private const val selfImprovementCodingDefaultFilter = "all"

private data class SelfImprovementCodingFilter(val status: String, val label: String)

private val selfImprovementCodingFilters = listOf(
    SelfImprovementCodingFilter(selfImprovementCodingDefaultStatus, "대기"),
    SelfImprovementCodingFilter("accepted", "채택"),
    SelfImprovementCodingFilter("applied", "적용"),
    SelfImprovementCodingFilter("rejected", "기각"),
    SelfImprovementCodingFilter("superseded", "대체"),
    SelfImprovementCodingFilter("all", "전체"),
)

private fun selfImprovementCodingStatusCount(
    counts: List<SelfImprovementCodingStatusCount>,
    status: String,
): Int = counts.firstOrNull { it.status == status }?.count ?: 0

private fun selfImprovementCodingEmptyText(status: String): String = when (status) {
    selfImprovementCodingDefaultStatus -> "대기 중인 자가개선 코딩 후보가 없습니다."
    "accepted" -> "채택된 자가개선 코딩 후보가 없습니다."
    "applied" -> "적용된 자가개선 코딩 후보가 없습니다."
    "rejected" -> "기각된 자가개선 코딩 후보가 없습니다."
    "superseded" -> "대체된 자가개선 코딩 후보가 없습니다."
    "all" -> "아직 자가개선 후보가 없습니다. 후보가 생기면 하트비트가 자동으로 검토합니다."
    else -> "자가개선 코딩 후보가 없습니다."
}
