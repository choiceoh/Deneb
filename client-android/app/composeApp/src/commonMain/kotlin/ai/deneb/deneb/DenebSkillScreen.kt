package ai.deneb.deneb

import ai.deneb.deneb.generated.SkillDetailResponse
import ai.deneb.deneb.generated.SkillLifecycleEvent
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.markdown.MarkdownContent
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import kotlinx.datetime.TimeZone
import kotlinx.datetime.toLocalDateTime
import kotlin.time.Instant

/**
 * Skill detail (`miniapp.skills.detail`): the full catalog meta the list row
 * truncates, the SKILL.md document itself, and the skill's own slice of the
 * Propus timeline (`miniapp.skills.lifecycle` filtered by skillName).
 * Read-only, like the Skills tab that opens it — skills are managed on the
 * gateway host (and by Propus), not edited from the client.
 */
@Composable
fun DenebSkillScreen(
    client: DenebGatewayClient,
    skillName: String,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var detail by remember(skillName) { mutableStateOf<SkillDetailResponse?>(null) }
    var events by remember(skillName) { mutableStateOf<List<SkillLifecycleEvent>?>(null) }
    var loadFailed by remember(skillName) { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    suspend fun reload() {
        loadFailed = false
        detail = null
        val d = client.fetchSkillDetail(skillName)
        detail = d
        loadFailed = d == null
        // Timeline is enrichment: a transport failure here degrades to an
        // empty section instead of failing the whole screen.
        events = if (d != null) client.fetchSkillLifecycle(limit = 30, skillName = skillName) else null
    }
    LaunchedEffect(skillName) { reload() }

    DenebScreenScaffold(title = "스킬", onBack = onBack, tabBar = navigationTabBar) {
        Column(
            Modifier
                .fillMaxWidth()
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp),
        ) {
            Spacer(Modifier.height(8.dp))
            val d = detail
            if (d == null) {
                if (loadFailed) {
                    DenebError("스킬을 불러오지 못했습니다.", onRetry = { scope.launch { reload() } })
                } else {
                    DenebLoading()
                }
            } else {
                SkillDetailContent(d, events.orEmpty())
            }
            Spacer(Modifier.height(24.dp))
        }
    }
}

// Stateless detail body — previewable without a gateway client.
@Composable
internal fun SkillDetailContent(detail: SkillDetailResponse, events: List<SkillLifecycleEvent>) {
    val skill = detail.skill

    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(
            skill.name,
            style = DenebType.subject,
            color = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.weight(1f, fill = false),
        )
        Spacer(Modifier.width(8.dp))
        SkillOriginBadge(skill.origin)
    }

    if (skill.description.isNotBlank()) {
        Spacer(Modifier.height(8.dp))
        Text(
            skill.description,
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
        )
    }

    Spacer(Modifier.height(8.dp))
    skillFactLines(skill = skill).forEach { line ->
        Text(
            line,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }

    Spacer(Modifier.height(16.dp))
    HorizontalDivider(color = denebHairline())
    Spacer(Modifier.height(12.dp))
    Text("문서", style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
    Spacer(Modifier.height(8.dp))
    val body = stripFrontmatter(detail.body)
    if (body.isNotBlank()) {
        MarkdownContent(body, baseStyle = MaterialTheme.typography.bodyMedium)
        if (detail.bodyTruncated) {
            Spacer(Modifier.height(4.dp))
            Text("(문서가 길어 일부만 표시합니다)", style = MaterialTheme.typography.bodySmall, color = denebHint())
        }
    } else {
        Text("SKILL.md 본문을 읽을 수 없습니다.", style = MaterialTheme.typography.bodySmall, color = denebHint())
    }

    Spacer(Modifier.height(16.dp))
    HorizontalDivider(color = denebHairline())
    Spacer(Modifier.height(12.dp))
    Text("Propus 로그", style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
    if (events.isEmpty()) {
        Spacer(Modifier.height(8.dp))
        Text("이 스킬의 Propus 활동이 아직 없습니다.", style = MaterialTheme.typography.bodySmall, color = denebHint())
    } else {
        events.forEachIndexed { idx, event ->
            SkillLifecycleRow(event, showSkillName = false, horizontalPadding = 0.dp)
            if (idx < events.lastIndex) HorizontalDivider(color = denebHairline())
        }
    }

    if (detail.path.isNotBlank()) {
        Spacer(Modifier.height(16.dp))
        Text(detail.path, style = MaterialTheme.typography.labelSmall, color = denebHint())
    }
}

/** The meta the list row has no room for, one fact per line, blanks omitted. */
private fun skillFactLines(skill: ai.deneb.deneb.generated.SkillRow): List<String> {
    val identity = listOfNotNull(
        skill.category.takeIf { it.isNotBlank() },
        skillSourceLabel(skill.source).takeIf { it.isNotBlank() },
        skill.version.takeIf { it.isNotBlank() }?.let { "v$it" },
    ).joinToString(" · ")
    val genesis = listOfNotNull(
        skill.createdAt.takeIf { it > 0 }?.let { "생성일 ${epochDate(it)}" },
        curatorStateLabel(skill.curatorState),
    ).joinToString(" · ")
    val usage = listOfNotNull(
        skill.totalUses.takeIf { it > 0 }?.let { "사용 ${it}회" },
        skill.lastUsedAt.takeIf { it > 0 }?.let { "마지막 사용 ${lifecycleTime(it)}" },
    ).joinToString(" · ")
    val evolve = listOfNotNull(
        skill.evolveCount.takeIf { it > 0 }?.let { "진화 ${it}회" },
        skill.lastEvolvedAt.takeIf { it > 0 }?.let { "마지막 진화 ${lifecycleTime(it)}" },
    ).joinToString(" · ")
    return listOf(identity, genesis, usage, evolve).filter { it.isNotBlank() }
}

/** Curator state for agent-created skills ("상태 활성" 등); null for initial
 *  skills (no curator record) or states we don't recognize. */
private fun curatorStateLabel(state: String): String? = when (state) {
    "active" -> "상태 활성"
    "stale" -> "상태 정체"
    "archived" -> "상태 보관됨"
    else -> null
}

private fun epochDate(ms: Long): String = Instant.fromEpochMilliseconds(ms).toLocalDateTime(TimeZone.currentSystemDefault()).date.toString()

/** Drops the leading YAML frontmatter fence from a SKILL.md body — the name /
 *  description / version it carries are already rendered as the header above,
 *  so showing the raw `---` block would only duplicate them as noise. */
internal fun stripFrontmatter(body: String): String {
    val text = body.trimStart('\uFEFF')
    if (!text.startsWith("---")) return body
    val firstLineEnd = text.indexOf('\n')
    if (firstLineEnd < 0 || text.substring(0, firstLineEnd).trim() != "---") return body
    var idx = firstLineEnd + 1
    while (idx <= text.length) {
        val lineEnd = text.indexOf('\n', idx).let { if (it < 0) text.length else it }
        if (text.substring(idx, lineEnd).trim() == "---") {
            return text.substring(minOf(lineEnd + 1, text.length)).trimStart('\n')
        }
        idx = lineEnd + 1
    }
    return body // unterminated fence — show as-is rather than eat the document
}
