package ai.deneb.deneb

import ai.deneb.deneb.generated.SelfCorrectionCandidate
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
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

@Composable
internal fun SelfImprovementCodingTab(client: DenebGatewayClient) {
    val scope = rememberCoroutineScope()
    var selectedStatus by rememberSaveable { mutableStateOf(selfImprovementCodingDefaultStatus) }
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
            Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp)) {
                Text(
                    "자가개선 코딩",
                    style = MaterialTheme.typography.titleSmall,
                    color = MaterialTheme.colorScheme.primary,
                    fontWeight = FontWeight.SemiBold,
                )
                Spacer(Modifier.height(2.dp))
                Text("대기 ${pendingCount}건 · 전체 ${totalCount}건", style = DenebType.meta, color = denebHint())
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
            items(candidates, key = { it.id }) { candidate ->
                SelfImprovementCodingCandidateRow(candidate)
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
                    style = MaterialTheme.typography.bodyMedium,
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
private fun SelfImprovementCodingCandidateRow(candidate: SelfCorrectionCandidate) {
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
            Text(lifecycleTime(candidate.updatedAt), style = DenebType.meta, color = denebHint())
        }
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
        if (expanded) {
            val detailLines = listOfNotNull(
                candidate.evidence.takeIf { it.isNotBlank() }?.let { "근거: $it" },
                candidate.risk.takeIf { it.isNotBlank() }?.let { "리스크: $it" },
                candidate.reason.takeIf { it.isNotBlank() }?.let { "메모: $it" },
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

@Composable
private fun SelfImprovementCodingStatusBadge(status: String) {
    val (bg, fg) = when (status) {
        "accepted", "applied" -> MaterialTheme.colorScheme.primaryContainer to MaterialTheme.colorScheme.onPrimaryContainer
        "rejected", "superseded" -> MaterialTheme.colorScheme.errorContainer to MaterialTheme.colorScheme.onErrorContainer
        else -> denebInsightContainer() to denebInsight()
    }
    Text(
        selfImprovementCodingStatusLabel(status),
        style = MaterialTheme.typography.labelSmall,
        color = fg,
        modifier = Modifier
            .clip(RoundedCornerShape(4.dp))
            .background(bg)
            .padding(horizontal = 6.dp, vertical = 1.dp),
    )
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

private const val selfImprovementCodingDefaultStatus = "proposed"

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
    else -> "자가개선 코딩 후보가 없습니다."
}
