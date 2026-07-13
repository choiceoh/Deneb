package ai.deneb.deneb

import ai.deneb.deneb.generated.RSIHealthView
import ai.deneb.deneb.generated.RSILayerView
import ai.deneb.deneb.generated.RSILoopStatusResponse
import ai.deneb.deneb.generated.SelfCorrectionCandidate
import ai.deneb.deneb.generated.SkillLifecycleEvent
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import ai.deneb.ui.denebInsightContainer
import ai.deneb.ui.denebOnSuccessContainer
import ai.deneb.ui.denebOnWarningContainer
import ai.deneb.ui.denebSuccessContainer
import ai.deneb.ui.denebWarningContainer
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import kotlin.math.roundToInt

/**
 * 재귀적 자가개선 — the recursive-self-improvement loop status
 * (`miniapp.rsi.status`). The four loop layers (L1 스킬 진화, L2 메타 진화,
 * L3 판정자 공진화, L4 소스 자가편집) each shown with an honest state badge
 * (LIVE/DATA-GATED/STARVED/FROZEN/IDLE), a one-line diagnosis, and key metrics —
 * the window onto how far the agent's self-improvement is actually turning.
 *
 * The DATA-GATED vs STARVED distinction is deliberate: DATA-GATED is a young
 * loop waiting for data (normal), STARVED is an input gap (actionable). The
 * badge colors keep them apart at a glance.
 *
 * Design split (docs/agent-rules/native-design-system.md): frame + type are the
 * Deneb skin (DenebScreenScaffold + DenebType + grouped DenebGroup cards); the
 * pull-to-refresh is Material. The layer list is a stateless body
 * ([RsiStatusContent]) the render harness previews with mock data; this
 * composable is the stateful shell (fetch + loading/error/empty states).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DenebRsiScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var status by remember { mutableStateOf<RSILoopStatusResponse?>(null) }
    // Drill-down detail: the L1 card expands into the Propus lifecycle feed, the
    // L4 card into the coding-candidate queue — so this screen is the single
    // self-improvement hub (overview + drill-in), not a fourth scattered surface.
    var lifecycle by remember { mutableStateOf<List<SkillLifecycleEvent>>(emptyList()) }
    var candidates by remember { mutableStateOf<List<SelfCorrectionCandidate>>(emptyList()) }
    // null = load in flight, true = ok, false = fetch failed (mirrors DenebDashboardScreen).
    var loadOk by remember { mutableStateOf<Boolean?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    val haptics = rememberHaptics()
    val scope = rememberCoroutineScope()

    suspend fun load() {
        val fetched: RSILoopStatusResponse? = client.fetchRsiStatus()
        if (fetched == null) {
            loadOk = false
        } else {
            status = fetched
            loadOk = true
            // Best-effort drill data — a failure here leaves the overview intact.
            lifecycle = client.fetchSkillLifecycle(limit = 12) ?: emptyList()
            // Pending dispatch = proposed + accepted (coding-dispatch picks both).
            // proposed-only hid the entire accepted L4 backlog (2026-07-13: 7
            // accepted health-finding candidates, drill empty while L4 LIVE).
            candidates = client.fetchSelfImprovementCodingCandidates(limit = 24, status = "all")
                ?.filter { it.status == "proposed" || it.status == "accepted" }
                ?.filter { it.scope.isBlank() || it.scope == "code" }
                ?.take(8)
                ?: emptyList()
        }
    }
    LaunchedEffect(Unit) { load() }

    DenebScreenScaffold(title = "재귀적 자가개선", onBack = onBack, tabBar = navigationTabBar) {
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = {
                haptics.refresh()
                scope.launch {
                    refreshing = true
                    load()
                    refreshing = false
                }
            },
            modifier = Modifier.fillMaxWidth().weight(1f),
        ) {
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                val s = status
                when {
                    loadOk == null && s == null -> DenebLoading()

                    loadOk == false && s == null -> DenebError(
                        "자가개선 상태를 불러오지 못했습니다.",
                        onRetry = {
                            scope.launch {
                                loadOk = null
                                load()
                            }
                        },
                    )

                    s == null || s.layers.isEmpty() -> DenebEmpty("표시할 자가개선 상태가 없습니다.")

                    else -> RsiStatusContent(s, lifecycle, candidates)
                }
                Spacer(Modifier.height(24.dp))
            }
        }
    }
}

// --- stateless body (previewable) ----------------------------------------

/**
 * The four RSI loop layers: a summary line ("N/4개 루프 가동 중") then one grouped
 * card per layer — header (key · title + state badge), diagnosis line, and a
 * wrapped row of value/label metrics. Pure presentation — the shell owns
 * fetch + state.
 */
@Composable
internal fun RsiStatusContent(
    status: RSILoopStatusResponse,
    lifecycle: List<SkillLifecycleEvent> = emptyList(),
    candidates: List<SelfCorrectionCandidate> = emptyList(),
) {
    Column(Modifier.fillMaxWidth().padding(top = 4.dp)) {
        Text(
            text = "${status.turning}/${status.layers.size}개 루프 가동 중",
            style = DenebType.sectionLabel,
            color = denebHint(),
            modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 12.dp),
        )
        // Same 18.dp gap the layer cards get — else the health card butts against L1.
        if (status.health.hasActivity) {
            RsiHealthCard(status.health)
            Spacer(Modifier.height(18.dp))
        }
        status.layers.forEach { layer ->
            RsiLayerCard(layer, lifecycle, candidates)
            Spacer(Modifier.height(18.dp))
        }
    }
}

/** The 7-day scoreboard renders only when something happened — an all-zero board
 *  would just echo the layer cards' IDLE state. Hoisted so the caller can guard the
 *  card *and* its trailing spacer with the same predicate. */
private val RSIHealthView.hasActivity: Boolean
    get() = evolves7d > 0 || genesis7d > 0 || metaRevisions7d > 0 ||
        confirmed7d > 0 || rolledBack7d > 0 || resolvedEvolves7d > 0 ||
        thrash || autoAdoptFrozen

/** Evolution-health scoreboard (7-day) from `rsi.status.health`: the numeric
 *  fields the layer diagnoses only render as prose. Skipped entirely when
 *  nothing has happened — the layer cards already say IDLE, so an all-zero
 *  scoreboard would be noise. Self-brake flags (thrash / auto-adopt frozen)
 *  surface as warm insight chips so a paused loop is visible at a glance. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun RsiHealthCard(health: RSIHealthView) {
    if (!health.hasActivity) return
    fun pct(v: Double) = "${(v * 100).roundToInt()}%"
    // Rates are undefined with no resolved sample — show "—", not a misleading 0%
    // (andromeda HealthCard parity).
    fun rate(v: Double) = if (health.resolvedEvolves7d > 0) pct(v) else "—"
    DenebGroup {
        Text(
            text = "진화 건강 (7일)",
            style = DenebType.sectionLabel,
            color = denebHint(),
            modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 14.dp, bottom = 8.dp),
        )
        FlowRow(
            Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, bottom = 12.dp),
            horizontalArrangement = Arrangement.spacedBy(20.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
            // 7 tiles: cap at 4/row so they wrap balanced (4+3) instead of orphaning the 7th.
            maxItemsInEachRow = 4,
        ) {
            RsiStat("확정률", rate(health.confirmRate))
            RsiStat("오수용률", rate(health.falseAcceptRate), sub = "n=${health.resolvedEvolves7d}")
            RsiStat("진화", health.evolves7d.toString())
            RsiStat("확정", health.confirmed7d.toString())
            RsiStat("롤백", health.rolledBack7d.toString())
            RsiStat("신규 스킬", health.genesis7d.toString())
            RsiStat("메타 개정", health.metaRevisions7d.toString())
        }
        if (health.thrash || health.autoAdoptFrozen) {
            FlowRow(
                Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, bottom = 14.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                if (health.autoAdoptFrozen) RsiFlagChip("메타 자동채택 동결")
                if (health.thrash) RsiFlagChip("진화 쓰래싱")
            }
        }
    }
}

/** One scoreboard tile: value over label, optional small sub (e.g. sample n). */
@Composable
private fun RsiStat(label: String, value: String, sub: String? = null) {
    Column {
        Text(text = value, style = DenebType.rowTitleStrong, color = MaterialTheme.colorScheme.onBackground)
        Text(text = label, style = DenebType.meta, color = denebHint())
        if (sub != null) Text(text = sub, style = DenebType.meta, color = denebHint())
    }
}

/** Warm-insight self-brake flag chip (auto-adopt frozen / thrash) — a paused or
 *  churning loop must be visible without reading the L2 diagnosis. */
@Composable
private fun RsiFlagChip(text: String) {
    Text(
        text = text,
        style = DenebType.meta,
        color = denebInsight(),
        modifier = Modifier
            .background(denebInsightContainer(), RoundedCornerShape(6.dp))
            .padding(horizontal = 8.dp, vertical = 3.dp),
    )
}

/** One loop layer: header (key · title + state badge), diagnosis, metric chips.
 *  Tapping the card (when it carries a detail) expands a Korean explanation of
 *  what the loop does — the "눌러서 상세" affordance the card header hints with a
 *  chevron. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun RsiLayerCard(
    layer: RSILayerView,
    lifecycle: List<SkillLifecycleEvent> = emptyList(),
    candidates: List<SelfCorrectionCandidate> = emptyList(),
) {
    var expanded by remember { mutableStateOf(false) }
    val haptics = rememberHaptics()
    val hasDetail = layer.detail.isNotBlank()
    DenebGroup {
        Row(
            Modifier
                .fillMaxWidth()
                .clickable(enabled = hasDetail) {
                    haptics.tap()
                    expanded = !expanded
                }
                .padding(start = 16.dp, end = 16.dp, top = 14.dp, bottom = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = "${layer.key} · ${layer.title}",
                style = DenebType.cardTitle,
                color = MaterialTheme.colorScheme.onBackground,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            RsiStateBadge(layer.state)
            if (hasDetail) {
                Text(
                    text = if (expanded) "⌃" else "⌄",
                    style = DenebType.meta,
                    color = denebHint(),
                    modifier = Modifier.padding(start = 8.dp),
                )
            }
        }
        if (layer.diagnosis.isNotBlank()) {
            Text(
                text = layer.diagnosis,
                style = DenebType.rowSubtitle,
                color = denebHint(),
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 16.dp, end = 16.dp, top = 2.dp, bottom = if (layer.metrics.isEmpty()) 16.dp else 10.dp),
            )
        }
        if (layer.metrics.isNotEmpty()) {
            FlowRow(
                Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 2.dp, bottom = if (expanded && hasDetail) 10.dp else 16.dp),
                horizontalArrangement = Arrangement.spacedBy(20.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                layer.metrics.forEach { m ->
                    Column {
                        Text(
                            text = m.value.ifBlank { "—" },
                            style = DenebType.rowTitleStrong,
                            color = MaterialTheme.colorScheme.onBackground,
                        )
                        Text(text = m.label, style = DenebType.meta, color = denebHint())
                    }
                }
            }
        }
        if (expanded && hasDetail) {
            Text(
                text = layer.detail,
                style = DenebType.rowSubtitle,
                color = MaterialTheme.colorScheme.onBackground,
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 16.dp, end = 16.dp, top = 2.dp, bottom = if (layer.key == "L1" || layer.key == "L4") 8.dp else 16.dp),
            )
            // Drill-in: L1 → recent Propus lifecycle, L4 → coding-candidate queue.
            when (layer.key) {
                "L1" -> RsiDrillSection(
                    "최근 스킬 생애",
                    lifecycle.take(6).map { rsiEventTypeLabel(it.type) to rsiEventText(it) },
                    "최근 스킬 진화 이벤트 없음",
                )

                "L4" -> RsiDrillSection(
                    "대기 중 코딩 후보",
                    candidates.take(6).map { rsiCandidateStatusLabel(it.status) to it.title.ifBlank { it.candidate } },
                    "대기 중인 코딩 후보 없음",
                )

                else -> {}
            }
        }
    }
}

/** A compact "recent detail" list inside an expanded layer card — the drill-in
 *  that folds the Propus feed / coding queue into the hub. Each row is a short
 *  label chip + a one-line text. */
@Composable
private fun RsiDrillSection(header: String, rows: List<Pair<String, String>>, emptyText: String) {
    Column(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, bottom = 16.dp)) {
        Text(header, style = DenebType.sectionLabel, color = denebHint(), modifier = Modifier.padding(bottom = 4.dp))
        if (rows.isEmpty()) {
            Text(emptyText, style = DenebType.meta, color = denebHint())
        } else {
            rows.forEach { (label, text) ->
                Row(Modifier.fillMaxWidth().padding(vertical = 3.dp), verticalAlignment = Alignment.Top) {
                    Text(
                        text = label,
                        style = DenebType.meta,
                        color = denebHint(),
                        maxLines = 1,
                        modifier = Modifier.width(52.dp).padding(end = 8.dp),
                    )
                    Text(
                        text = text.ifBlank { "—" },
                        style = DenebType.rowSubtitle,
                        color = MaterialTheme.colorScheme.onBackground,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                }
            }
        }
    }
}

private fun rsiEventTypeLabel(type: String): String = when (type) {
    "evolved" -> "진화"
    "genesis" -> "생성"
    "evolve_rejected" -> "기각"
    "evolution_proposal" -> "제안"
    "confirmed" -> "확정"
    "rolled_back" -> "롤백"
    else -> type
}

private fun rsiEventText(e: SkillLifecycleEvent): String {
    val name = e.skillName.ifBlank { "(전역)" }
    return if (e.detail.isNotBlank()) "$name · ${e.detail}" else name
}

private fun rsiCandidateStatusLabel(status: String): String = when (status) {
    "proposed" -> "제안"
    "accepted" -> "채택"
    "applied" -> "적용"
    "rejected" -> "기각"
    "superseded" -> "대체"
    else -> status.ifBlank { "제안" }
}

/** A colored pill for the layer state — the LIVE/DATA-GATED/STARVED/FROZEN/IDLE
 *  taxonomy, colored so "turning" (LIVE), "waiting" (DATA-GATED), "needs wiring"
 *  (STARVED), "self-braked" (FROZEN), and "dormant" (IDLE) separate at a glance. */
@Composable
private fun RsiStateBadge(state: String) {
    val bg: Color
    val fg: Color
    when (state) {
        "LIVE" -> {
            bg = denebSuccessContainer()
            fg = denebOnSuccessContainer()
        }

        "DATA-GATED" -> {
            bg = denebWarningContainer()
            fg = denebOnWarningContainer()
        }

        "STARVED" -> {
            bg = MaterialTheme.colorScheme.errorContainer
            fg = MaterialTheme.colorScheme.onErrorContainer
        }

        "FROZEN" -> {
            bg = denebInsightContainer()
            fg = denebInsight()
        }

        else -> { // IDLE / unknown
            bg = MaterialTheme.colorScheme.surfaceVariant
            fg = denebHint()
        }
    }
    val label = when (state) {
        "LIVE" -> "가동 중"
        "DATA-GATED" -> "데이터 대기"
        "STARVED" -> "연료 부족"
        "FROZEN" -> "동결"
        else -> "휴면"
    }
    Text(
        text = label,
        style = DenebType.meta,
        color = fg,
        modifier = Modifier
            .padding(start = 8.dp)
            .background(bg, RoundedCornerShape(6.dp))
            .padding(horizontal = 8.dp, vertical = 3.dp),
    )
}
