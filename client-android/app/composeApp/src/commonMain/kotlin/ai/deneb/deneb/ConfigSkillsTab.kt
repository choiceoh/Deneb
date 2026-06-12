package ai.deneb.deneb

import ai.deneb.deneb.generated.SkillLifecycleEvent
import ai.deneb.deneb.generated.SkillRow
import ai.deneb.ui.denebHairline
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
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
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import kotlin.time.Clock

// Settings hub "스킬" tab: skills the agent can use (read-only) plus the
// self-evolution timeline. The list mirrors the system-prompt skill catalog via
// miniapp.skills.list — name, description, category, source — now enriched with
// the skill's origin (생성 = the self-evolution loop authored it, 최초 = it was
// installed or hand-written) and evolve/usage counters. The 진화 내역 segment
// streams miniapp.skills.lifecycle: genesis creations, committed evolves,
// rejected evolve attempts, and review verdicts, newest first.
// No toggles: discovery is filesystem-driven, so the list reflects what's
// installed on the gateway host. Hosted by [DenebConfigScreen]'s pager.
@Composable
internal fun SkillsTab(client: DenebGatewayClient) {
    val skills by client.denebSkills.collectAsState()
    val scope = rememberCoroutineScope()
    var loadFailed by remember { mutableStateOf(false) }
    var showLifecycle by remember { mutableStateOf(false) }
    var lifecycleEvents by remember { mutableStateOf<List<SkillLifecycleEvent>?>(null) }
    var lifecycleFailed by remember { mutableStateOf(false) }

    suspend fun loadLifecycle() {
        lifecycleFailed = false
        val fetched = client.fetchSkillLifecycle()
        lifecycleEvents = fetched
        if (fetched == null) lifecycleFailed = true
    }

    LaunchedEffect(Unit) { loadFailed = !client.refreshSkills() }
    LaunchedEffect(showLifecycle) {
        if (showLifecycle && lifecycleEvents == null) loadLifecycle()
    }

    Column(Modifier.fillMaxSize()) {
        SingleChoiceSegmentedButtonRow(
            Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
        ) {
            SegmentedButton(
                selected = !showLifecycle,
                onClick = { showLifecycle = false },
                shape = SegmentedButtonDefaults.itemShape(index = 0, count = 2),
            ) { Text("스킬 목록") }
            SegmentedButton(
                selected = showLifecycle,
                onClick = { showLifecycle = true },
                shape = SegmentedButtonDefaults.itemShape(index = 1, count = 2),
            ) { Text("진화 내역") }
        }
        if (!showLifecycle) {
            when {
                skills.isEmpty() && loadFailed -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    DenebError(
                        "스킬을 불러오지 못했습니다.",
                        onRetry = { scope.launch { loadFailed = !client.refreshSkills() } },
                    )
                }

                skills.isEmpty() -> EmptyTab("사용할 수 있는 스킬이 없습니다.")

                else -> SkillListContent(skills)
            }
        } else {
            val events = lifecycleEvents
            when {
                lifecycleFailed && events == null -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    DenebError(
                        "진화 내역을 불러오지 못했습니다.",
                        onRetry = { scope.launch { loadLifecycle() } },
                    )
                }

                events == null -> DenebLoading()

                events.isEmpty() -> EmptyTab("아직 자기진화 활동이 없습니다.")

                else -> SkillLifecycleContent(events)
            }
        }
    }
}

// Stateless skill list — previewable without a gateway client.
@Composable
internal fun SkillListContent(skills: List<SkillRow>) {
    LazyColumn(Modifier.fillMaxSize()) {
        items(skills, key = { it.name }) { skill ->
            Column(
                Modifier.animateItem().fillMaxWidth().padding(horizontal = 16.dp, vertical = 14.dp),
            ) {
                // Skill name only — no runnable slash command. The live slash
                // dispatcher matches a lowercased raw name (not a sanitized
                // command) and only for local/system skills, so showing a
                // command here would risk advertising one that doesn't route.
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        skill.name,
                        style = MaterialTheme.typography.bodyLarge,
                        fontWeight = FontWeight.Medium,
                        color = MaterialTheme.colorScheme.onSurface,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f, fill = false),
                    )
                    Spacer(Modifier.width(6.dp))
                    SkillOriginBadge(skill.origin)
                }
                if (skill.description.isNotBlank()) {
                    Text(
                        skill.description,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                val meta = skillMetaLine(skill)
                if (meta.isNotBlank()) {
                    Spacer(Modifier.height(2.dp))
                    Text(
                        meta,
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
        }
    }
}

// Stateless self-evolution timeline — previewable without a gateway client.
@Composable
internal fun SkillLifecycleContent(events: List<SkillLifecycleEvent>) {
    LazyColumn(Modifier.fillMaxSize()) {
        itemsIndexed(events) { _, event ->
            Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    LifecycleTypeBadge(event.type)
                    Spacer(Modifier.width(8.dp))
                    Text(
                        event.skillName.ifBlank { "(스킬 미지정)" },
                        style = MaterialTheme.typography.bodyMedium,
                        fontWeight = FontWeight.Medium,
                        color = MaterialTheme.colorScheme.onSurface,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                    Text(
                        lifecycleTime(event.at),
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                val detail = listOfNotNull(
                    event.version.takeIf { it.isNotBlank() }?.let { "v$it" },
                    event.detail.takeIf { it.isNotBlank() },
                ).joinToString(" — ")
                if (detail.isNotBlank()) {
                    Spacer(Modifier.height(2.dp))
                    Text(
                        detail,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 3,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
            HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
        }
    }
}

// Origin badge: 생성 (self-evolution authored) vs 최초 (installed/hand-written).
// Both render so the distinction is explicit, not inferred from absence.
@Composable
private fun SkillOriginBadge(origin: String) {
    val generated = origin == "genesis"
    val bg = if (generated) MaterialTheme.colorScheme.primaryContainer else MaterialTheme.colorScheme.surfaceVariant
    val fg = if (generated) MaterialTheme.colorScheme.onPrimaryContainer else MaterialTheme.colorScheme.onSurfaceVariant
    Text(
        if (generated) "생성" else "최초",
        style = MaterialTheme.typography.labelSmall,
        color = fg,
        modifier = Modifier
            .clip(RoundedCornerShape(4.dp))
            .background(bg)
            .padding(horizontal = 6.dp, vertical = 1.dp),
    )
}

@Composable
private fun LifecycleTypeBadge(type: String) {
    val (label, bg, fg) = when (type) {
        "genesis" -> Triple(
            "생성",
            MaterialTheme.colorScheme.tertiaryContainer,
            MaterialTheme.colorScheme.onTertiaryContainer,
        )

        "evolved" -> Triple(
            "진화",
            MaterialTheme.colorScheme.primaryContainer,
            MaterialTheme.colorScheme.onPrimaryContainer,
        )

        "evolve_rejected" -> Triple(
            "기각",
            MaterialTheme.colorScheme.errorContainer,
            MaterialTheme.colorScheme.onErrorContainer,
        )

        else -> Triple(
            "리뷰",
            MaterialTheme.colorScheme.surfaceVariant,
            MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
    Text(
        label,
        style = MaterialTheme.typography.labelSmall,
        color = fg,
        modifier = Modifier
            .clip(RoundedCornerShape(4.dp))
            .background(bg)
            .padding(horizontal = 6.dp, vertical = 1.dp),
    )
}

// skillSourceLabel maps the gateway's discovery-origin string to a Korean label,
// matching DenebConfigScreen's literal-string convention (this screen doesn't use
// stringResource). Falls back to the raw value for origins we don't surface yet.
private fun skillSourceLabel(source: String): String = when (source) {
    "managed" -> "관리형"
    "workspace" -> "워크스페이스"
    "agents-skills-personal" -> "개인"
    "agents-skills-project" -> "프로젝트"
    "bundled" -> "기본 제공"
    "plugin" -> "플러그인"
    "extra" -> "추가"
    else -> source
}

// skillMetaLine renders "category · source · vN · 진화 N회 · 사용 N회",
// omitting whichever is blank/zero.
private fun skillMetaLine(skill: SkillRow): String = listOfNotNull(
    skill.category.takeIf { it.isNotBlank() },
    skillSourceLabel(skill.source).takeIf { it.isNotBlank() },
    skill.version.takeIf { it.isNotBlank() }?.let { "v$it" },
    skill.evolveCount.takeIf { it > 0 }?.let { "진화 ${it}회" },
    skill.totalUses.takeIf { it > 0 }?.let { "사용 ${it}회" },
).joinToString(" · ")

/** Short Korean relative time for timeline rows ("방금" / "N분 전" / "N시간 전" /
 *  "N일 전"). Blank for missing/future timestamps so the row omits the stamp. */
private fun lifecycleTime(epochMs: Long): String {
    if (epochMs <= 0L) return ""
    val diff = Clock.System.now().toEpochMilliseconds() - epochMs
    return when {
        diff < 0L -> ""
        diff < 60_000L -> "방금"
        diff < 3_600_000L -> "${diff / 60_000L}분 전"
        diff < 86_400_000L -> "${diff / 3_600_000L}시간 전"
        else -> "${diff / 86_400_000L}일 전"
    }
}
