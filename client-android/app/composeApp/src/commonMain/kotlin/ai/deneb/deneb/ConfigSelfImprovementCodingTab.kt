package ai.deneb.deneb

import ai.deneb.deneb.generated.SelfCorrectionCandidate
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import ai.deneb.ui.denebInsightContainer
import ai.deneb.ui.handCursor
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

@Composable
internal fun SelfImprovementCodingTab(client: DenebGatewayClient) {
    val scope = rememberCoroutineScope()
    var candidates by remember { mutableStateOf<List<SelfCorrectionCandidate>?>(null) }
    var loadFailed by remember { mutableStateOf(false) }

    suspend fun reload() {
        loadFailed = false
        val fetched = client.fetchSelfImprovementCodingCandidates()
        candidates = fetched
        if (fetched == null) loadFailed = true
    }

    LaunchedEffect(Unit) { reload() }

    when {
        loadFailed && candidates == null -> DenebError(
            "자가개선 코딩 후보를 불러오지 못했습니다.",
            onRetry = { scope.launch { reload() } },
        )

        candidates == null -> DenebLoading()

        candidates.orEmpty().isEmpty() -> EmptyTab("대기 중인 자가개선 코딩 후보가 없습니다.")

        else -> SelfImprovementCodingContent(candidates.orEmpty())
    }
}

@Composable
internal fun SelfImprovementCodingContent(candidates: List<SelfCorrectionCandidate>) {
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
                Text("미적용 후보 ${candidates.size}건", style = DenebType.meta, color = denebHint())
            }
            HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
        }
        items(candidates, key = { it.id }) { candidate ->
            SelfImprovementCodingCandidateRow(candidate)
            HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
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
