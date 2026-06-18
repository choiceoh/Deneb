package ai.deneb.deneb

import ai.deneb.deneb.generated.SkillLifecycleEvent
import ai.deneb.deneb.generated.SkillRow
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import ai.deneb.ui.denebInsightContainer
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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.item
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
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
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import kotlinx.datetime.TimeZone
import kotlinx.datetime.toLocalDateTime
import kotlin.time.Clock
import kotlin.time.Instant

// Settings hub "스킬" tab: skills the agent can use (read-only) plus the
// Propus log. Propus is Deneb's self-improvement system: proposal, genesis,
// evolution, validation, rollback, and deferred self-correction in one loop.
// The list mirrors the system-prompt skill catalog via miniapp.skills.list —
// name, description, category, source — now enriched with the skill's origin
// (생성 = Propus authored it, 최초 = it was installed or hand-written) and
// evolve/usage counters. The Propus segment streams miniapp.skills.lifecycle:
// genesis creations, committed evolves, rejected/rolled-back evolve attempts,
// and review verdicts, newest first.
// Timeline rows expand on tap — full reason, review evidence, absolute time,
// verdict — and link through to the skill when the event names one.
// No toggles: discovery is filesystem-driven, so the list reflects what's
// installed on the gateway host. Tapping a row opens [DenebSkillScreen]
// (full meta + SKILL.md body + per-skill timeline) via [onOpenSkill].
// Hosted by [DenebConfigScreen]'s pager.
@Composable
internal fun SkillsTab(client: DenebGatewayClient, onOpenSkill: (String) -> Unit = {}) {
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
        SkillsViewSwitcher(showLifecycle) { showLifecycle = it }
        if (!showLifecycle) {
            when {
                skills.isEmpty() && loadFailed -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    DenebError(
                        "스킬을 불러오지 못했습니다.",
                        onRetry = { scope.launch { loadFailed = !client.refreshSkills() } },
                    )
                }

                skills.isEmpty() -> EmptyTab("사용할 수 있는 스킬이 없습니다.")

                else -> SkillListContent(skills, onOpenSkill)
            }
        } else {
            val events = lifecycleEvents
            when {
                lifecycleFailed && events == null -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    DenebError(
                        "Propus 로그를 불러오지 못했습니다.",
                        onRetry = { scope.launch { loadLifecycle() } },
                    )
                }

                events == null -> DenebLoading()

                events.isEmpty() -> EmptyTab("아직 Propus 활동이 없습니다.")

                else -> SkillLifecycleContent(events, onOpenSkill)
            }
        }
    }
}

// Flat view switcher in the Deneb idiom — accent-vs-hint text over a shared
// hairline, no capsule or fill. The Material SegmentedButton it replaces read
// as a third chrome layer stacked under the settings hub's pill tab bar; this
// is view navigation (not a form input), so presentation belongs to Deneb
// while each label keeps Material selectable + Role.Tab semantics. Per the
// 2026-06 accent doctrine the active label takes the cool interactive `primary`
// (was suppressed to ink) so the selected view reads at a glance.
@Composable
internal fun SkillsViewSwitcher(showLifecycle: Boolean, onSelect: (Boolean) -> Unit) {
    val haptics = rememberHaptics()
    Column(Modifier.fillMaxWidth()) {
        Row(
            Modifier.padding(horizontal = 16.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            listOf("스킬 목록" to false, "Propus 로그" to true).forEach { (label, lifecycle) ->
                val selected = showLifecycle == lifecycle
                Text(
                    label,
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
                                    onSelect(lifecycle)
                                }
                            },
                        )
                        .padding(horizontal = 8.dp, vertical = 10.dp),
                )
            }
        }
        HorizontalDivider(color = denebHairline())
    }
}

// Stateless skill list — previewable without a gateway client. Rows tap
// through to the skill detail screen.
@Composable
internal fun SkillListContent(skills: List<SkillRow>, onOpenSkill: (String) -> Unit = {}) {
    val haptics = rememberHaptics()
    LazyColumn(Modifier.fillMaxSize()) {
        items(skills, key = { it.name }) { skill ->
            Column(
                Modifier
                    .animateItem()
                    .fillMaxWidth()
                    .clickable {
                        haptics.tap()
                        onOpenSkill(skill.name)
                    }
                    .padding(horizontal = 16.dp, vertical = 14.dp),
            ) {
                // Skill name only — no runnable slash command. The live slash
                // dispatcher matches a lowercased raw name (not a sanitized
                // command) and only for local/system skills, so showing a
                // command here would risk advertising one that doesn't route.
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        skill.name,
                        style = DenebType.rowTitleStrong,
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
                        style = DenebType.rowSubtitle,
                        color = denebHint(),
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                val meta = skillMetaLine(skill)
                if (meta.isNotBlank()) {
                    Spacer(Modifier.height(2.dp))
                    Text(
                        meta,
                        style = DenebType.meta,
                        color = denebHint(),
                    )
                }
            }
            HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
        }
    }
}

// Stateless Propus timeline — previewable without a gateway client.
// Rows expand on tap; [onOpenSkill] backs the expanded row's 스킬 보기 link.
@Composable
internal fun SkillLifecycleContent(events: List<SkillLifecycleEvent>, onOpenSkill: (String) -> Unit = {}) {
    LazyColumn(Modifier.fillMaxSize()) {
        item {
            PropusTimelineHeader(events)
            HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
        }
        itemsIndexed(events) { _, event ->
            SkillLifecycleRow(event, onOpenSkill = onOpenSkill)
            HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
        }
    }
}

private data class PropusTimelineSummary(
    val total: Int,
    val genesis: Int,
    val evolved: Int,
    val review: Int,
    val attention: Int,
    val latestAt: Long,
)

@Composable
private fun PropusTimelineHeader(events: List<SkillLifecycleEvent>) {
    val summary = remember(events) { propusTimelineSummary(events) }
    val activity = listOfNotNull(
        "최근 ${summary.total}건",
        "생성 ${summary.genesis}",
        "진화 ${summary.evolved}",
        "리뷰 ${summary.review}",
        summary.latestAt.takeIf { it > 0L }?.let { "마지막 ${lifecycleTime(it)}" },
    ).joinToString(" · ")
    val state = if (summary.attention > 0) {
        "주의 ${summary.attention}건 · 기각/롤백 포함"
    } else {
        "정상 관찰 중"
    }
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp)) {
        Text(
            "Propus",
            style = MaterialTheme.typography.titleSmall,
            color = MaterialTheme.colorScheme.primary,
            fontWeight = FontWeight.SemiBold,
        )
        Spacer(Modifier.height(2.dp))
        Text(activity, style = DenebType.meta, color = denebHint())
        Spacer(Modifier.height(2.dp))
        Text(
            state,
            style = DenebType.rowSubtitle,
            color = if (summary.attention > 0) MaterialTheme.colorScheme.error else denebHint(),
        )
    }
}

private fun propusTimelineSummary(events: List<SkillLifecycleEvent>): PropusTimelineSummary {
    var genesis = 0
    var evolved = 0
    var review = 0
    var attention = 0
    var latestAt = 0L
    for (event in events) {
        if (event.at > latestAt) latestAt = event.at
        when (event.type) {
            "genesis" -> genesis++
            "evolved" -> evolved++
            "evolve_rejected", "evolve_rolled_back" -> attention++
            else -> review++
        }
    }
    return PropusTimelineSummary(
        total = events.size,
        genesis = genesis,
        evolved = evolved,
        review = review,
        attention = attention,
        latestAt = latestAt,
    )
}

// One lifecycle event row — shared by the tab timeline (above) and the
// per-skill section in [DenebSkillScreen]. [showSkillName] is off in the
// detail screen, where every event belongs to the same skill (the type badge
// alone carries the row) and the parent column already pads horizontally.
//
// Collapsed, the detail clamps to 3 lines; tapping the row expands it to the
// full reason plus the review evidence, absolute timestamp, and verdict. The
// expansion is in-place because over half of real review events carry no
// skill name (no-op verdicts) — a tap-through-only design would leave those
// rows dead. When the event does name a skill and [onOpenSkill] is wired, the
// expanded row offers a 스킬 보기 link into [DenebSkillScreen].
@Composable
internal fun SkillLifecycleRow(
    event: SkillLifecycleEvent,
    showSkillName: Boolean = true,
    horizontalPadding: Dp = 16.dp,
    initiallyExpanded: Boolean = false,
    onOpenSkill: ((String) -> Unit)? = null,
) {
    val haptics = rememberHaptics()
    var expanded by rememberSaveable { mutableStateOf(initiallyExpanded) }
    Column(
        Modifier
            .fillMaxWidth()
            .handCursor()
            .clickable {
                haptics.tap()
                expanded = !expanded
            }
            .padding(horizontal = horizontalPadding, vertical = 12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            LifecycleTypeBadge(event.type)
            Spacer(Modifier.width(8.dp))
            if (showSkillName) {
                Text(
                    event.skillName.ifBlank { "(스킬 미지정)" },
                    style = DenebType.rowTitleStrong,
                    color = MaterialTheme.colorScheme.onSurface,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
            } else {
                Spacer(Modifier.weight(1f))
            }
            Text(
                lifecycleTime(event.at),
                style = DenebType.meta,
                color = denebHint(),
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
                style = DenebType.rowSubtitle,
                color = denebHint(),
                maxLines = if (expanded) Int.MAX_VALUE else 3,
                overflow = TextOverflow.Ellipsis,
            )
        }
        if (expanded) {
            if (event.evidence.isNotBlank()) {
                Spacer(Modifier.height(6.dp))
                Text(
                    "근거: ${event.evidence}",
                    style = DenebType.rowSubtitle,
                    color = denebHint(),
                )
            }
            val meta = listOfNotNull(
                lifecycleDateTime(event.at).takeIf { it.isNotBlank() },
                lifecycleRouteLabel(event.route),
            ).joinToString(" · ")
            if (meta.isNotBlank()) {
                Spacer(Modifier.height(6.dp))
                Text(meta, style = DenebType.meta, color = denebHint())
            }
            if (onOpenSkill != null && event.skillName.isNotBlank()) {
                Spacer(Modifier.height(6.dp))
                Text(
                    "스킬 보기 →",
                    style = MaterialTheme.typography.labelLarge,
                    color = MaterialTheme.colorScheme.primary,
                    modifier = Modifier
                        .handCursor()
                        .clickable {
                            haptics.tap()
                            onOpenSkill(event.skillName)
                        }
                        .padding(vertical = 2.dp),
                )
            }
        }
    }
}

// Origin badge: 생성 (Propus authored) vs 최초 (installed/hand-written).
// Both render so the distinction is explicit, not inferred from absence. A
// self-authored skill is AI-analysis output, so 생성 takes the warm apricot
// insight accent (2026-06 doctrine); 최초 stays a neutral mono chip.
@Composable
internal fun SkillOriginBadge(origin: String) {
    val generated = origin == "genesis"
    val bg = if (generated) denebInsightContainer() else MaterialTheme.colorScheme.surfaceVariant
    val fg = if (generated) denebInsight() else MaterialTheme.colorScheme.onSurfaceVariant
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

/** Korean label per lifecycle event type, rendered in [LifecycleTypeBadge]. */
private fun lifecycleTypeLabel(type: String): String = when (type) {
    "genesis" -> "생성"
    "evolved" -> "진화"
    "evolve_rejected" -> "기각"
    "evolve_rolled_back" -> "롤백"
    else -> "리뷰"
}

@Composable
private fun LifecycleTypeBadge(type: String) {
    // Two-accent + semantic mapping (2026-06 doctrine): genesis (AI authored a new
    // skill) = warm apricot insight; evolved (an applied/committed change) = cool
    // interactive primary; rejected/rolled back = semantic warning/error;
    // review/no-op = neutral mono.
    val (bg, fg) = when (type) {
        "genesis" -> denebInsightContainer() to denebInsight()
        "evolved" -> MaterialTheme.colorScheme.primaryContainer to MaterialTheme.colorScheme.onPrimaryContainer
        "evolve_rejected" -> MaterialTheme.colorScheme.errorContainer to MaterialTheme.colorScheme.onErrorContainer
        "evolve_rolled_back" -> MaterialTheme.colorScheme.tertiaryContainer to MaterialTheme.colorScheme.onTertiaryContainer
        else -> MaterialTheme.colorScheme.surfaceVariant to MaterialTheme.colorScheme.onSurfaceVariant
    }
    Text(
        lifecycleTypeLabel(type),
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
internal fun skillSourceLabel(source: String): String = when (source) {
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

/** Review verdict in Korean for the expanded timeline row; null for events
 *  without a route (genesis/evolved/rejected). Unknown future routes fall back
 *  to the raw value rather than hiding the verdict. */
internal fun lifecycleRouteLabel(route: String): String? = when (route) {
    "no-op" -> "판정: 변경 없음"
    "evolve" -> "판정: 기존 스킬 진화"
    "create", "genesis" -> "판정: 새 스킬 생성"
    else -> route.takeIf { it.isNotBlank() }?.let { "판정: $it" }
}

/** Absolute local timestamp ("2026-06-13 14:05") for the expanded timeline
 *  row — the collapsed header only carries the relative stamp. Blank when the
 *  event has no timestamp. */
internal fun lifecycleDateTime(epochMs: Long): String {
    if (epochMs <= 0L) return ""
    val dt = Instant.fromEpochMilliseconds(epochMs).toLocalDateTime(TimeZone.currentSystemDefault())
    val hh = dt.hour.toString().padStart(2, '0')
    val mm = dt.minute.toString().padStart(2, '0')
    return "${dt.date} $hh:$mm"
}

/** Short Korean relative time for timeline rows ("방금" / "N분 전" / "N시간 전" /
 *  "N일 전"). Blank for missing/future timestamps so the row omits the stamp.
 *  Shared with [DenebSkillScreen]'s usage/evolve meta lines. */
internal fun lifecycleTime(epochMs: Long): String {
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
