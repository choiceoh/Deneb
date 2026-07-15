package ai.deneb.deneb

import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.handCursor
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.selection.selectable
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
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch

// Settings hub "관찰" (Observe) tab: the gateway's own behavior + health + recent
// warn/error logs via miniapp.observe.*. The native adapter over the observe
// plane (CLI and chat tool are the other two). Read-only — an operator
// dashboard, not controls. A flat 1일/7일 switcher scopes the window (behavior +
// logs re-query for the span; health is always a live glance). Hosted by
// [DenebConfigScreen]'s pager.
@Composable
internal fun ObserveTab(client: DenebGatewayClient) {
    var selectedDays by remember { mutableStateOf(7) }
    var health by remember { mutableStateOf<ObserveHealth?>(null) }
    var behavior by remember { mutableStateOf<ObserveBehavior?>(null) }
    var logs by remember { mutableStateOf<List<ObserveLogLine>>(emptyList()) }
    var loading by remember { mutableStateOf(true) }
    var failed by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    suspend fun load() {
        loading = true
        failed = false
        coroutineScope {
            val h = async { client.observeHealth() }
            val b = async { client.observeBehavior(selectedDays) }
            val l = async { client.observeLogs("warn", 40, selectedDays) }
            health = h.await()
            behavior = b.await()
            val logPayload = l.await()
            logs = logPayload?.lines ?: emptyList()
            failed = health == null && behavior == null && logPayload == null
        }
        loading = false
    }
    LaunchedEffect(selectedDays) { load() }
    Column(Modifier.fillMaxSize()) {
        ObservePeriodSwitcher(selectedDays) { selectedDays = it }
        Box(Modifier.fillMaxWidth().weight(1f)) {
            when {
                loading -> DenebLoading()

                failed -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    DenebError("관찰 데이터를 불러오지 못했습니다.", onRetry = { scope.launch { load() } })
                }

                else -> LazyColumn(Modifier.fillMaxSize()) {
                    health?.let { h ->
                        item {
                            ObserveSectionHeader("상태")
                            Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp)) {
                                Text(
                                    buildString {
                                        append(if (h.captureEnabled) "캡처 on" else "캡처 off")
                                        append(" · ")
                                        append(if (h.agentLogEnabled) "에이전트로그 on" else "에이전트로그 off")
                                        if (h.ringCapacity > 0) {
                                            append(" · 링 ${h.ringUsed}/${h.ringCapacity}")
                                        }
                                        if (h.recentErrors > 0) {
                                            append(" · ERROR ${h.recentErrors}")
                                        }
                                    },
                                    style = DenebType.rowTitle,
                                    color = MaterialTheme.colorScheme.onBackground,
                                )
                                Text(
                                    "24h 실행 ${h.runs24h} · 능동 ${h.proactiveRuns24h} · 압축 ${h.compactedRuns24h}" +
                                        if (h.backgroundErrors24h > 0) " · 백그라운드 오류 ${h.backgroundErrors24h}" else "",
                                    style = DenebType.rowSubtitle,
                                    color = if (h.backgroundErrors24h > 0 || h.recentErrors > 0) {
                                        MaterialTheme.colorScheme.error
                                    } else {
                                        denebHint()
                                    },
                                )
                                if (h.vllmPrefixCache.isNotEmpty()) {
                                    Text(
                                        h.vllmPrefixCache.joinToString(" · ") { c ->
                                            val label = c.model.ifBlank { "vLLM" }
                                            "$label 캐시 ${formatPct(c.hitRatePct)}%"
                                        },
                                        style = DenebType.snippet,
                                        color = denebHint(),
                                        modifier = Modifier.padding(top = 4.dp),
                                    )
                                }
                            }
                            HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
                        }
                    }
                    behavior?.let { b ->
                        item {
                            Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 14.dp)) {
                                Text("최근 ${selectedDays}일 동작", style = DenebType.rowTitleStrong, color = MaterialTheme.colorScheme.onBackground)
                                Text(
                                    "실행 ${b.runs}회 · 능동 ${b.proactiveRuns} · 압축 ${b.compactedRuns}",
                                    style = DenebType.rowSubtitle,
                                    color = denebHint(),
                                )
                                if (b.totalInputTokens > 0 || b.totalOutputTokens > 0 || b.cacheReadTokens > 0) {
                                    Text(
                                        "입력 ${formatCount(b.totalInputTokens)} · 출력 ${formatCount(b.totalOutputTokens)} · 캐시 ${formatCount(b.cacheReadTokens)}",
                                        style = DenebType.snippet,
                                        color = denebHint(),
                                        modifier = Modifier.padding(top = 4.dp),
                                    )
                                }
                            }
                            HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
                        }
                        if (b.proactiveDecisions.isNotEmpty()) {
                            item { ObserveSectionHeader("능동 전달") }
                            items(b.proactiveDecisions.entries.sortedByDescending { it.value }.toList(), key = { "pd-${it.key}" }) { (key, count) ->
                                ObserveKeyCountRow(key, count, emphasize = key.startsWith("suppressed"))
                            }
                        }
                        val bgNames = (b.backgroundJobs.keys + b.backgroundErrors.keys).toSortedSet()
                        if (bgNames.isNotEmpty()) {
                            item { ObserveSectionHeader("백그라운드") }
                            items(bgNames.toList(), key = { "bg-$it" }) { name ->
                                val jobs = b.backgroundJobs[name] ?: 0
                                val errors = b.backgroundErrors[name] ?: 0
                                Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp)) {
                                    Text(name, style = DenebType.rowTitle, color = MaterialTheme.colorScheme.onBackground)
                                    Text(
                                        if (errors > 0) "${jobs}회 · $errors 오류" else "${jobs}회",
                                        style = DenebType.snippet,
                                        color = if (errors > 0) MaterialTheme.colorScheme.error else denebHint(),
                                    )
                                }
                                HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
                            }
                        }
                        if (b.tools.isNotEmpty()) {
                            item { ObserveSectionHeader("도구 사용") }
                            items(b.tools, key = { it.name }) { t ->
                                Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp)) {
                                    Text(t.name, style = DenebType.rowTitle, color = MaterialTheme.colorScheme.onBackground)
                                    Text(
                                        if (t.errors > 0) {
                                            "${t.calls}회 · ${t.errors} 오류 · 평균 ${t.avgMs}ms"
                                        } else {
                                            "${t.calls}회 · 평균 ${t.avgMs}ms"
                                        },
                                        style = DenebType.snippet,
                                        color = if (t.errors > 0) MaterialTheme.colorScheme.error else denebHint(),
                                    )
                                    toolAnomalySnippet(t)?.let { snippet ->
                                        Text(snippet, style = DenebType.snippet, color = denebHint(), modifier = Modifier.padding(top = 2.dp))
                                    }
                                }
                                HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
                            }
                        }
                    }
                    if (logs.isNotEmpty()) {
                        item { ObserveSectionHeader("최근 경고 / 오류") }
                        items(logs.size) { i ->
                            ObserveLogRow(logs[i])
                        }
                    }
                    if ((behavior?.runs ?: 0) == 0 && logs.isEmpty() && health == null) {
                        item {
                            Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) {
                                Text("아직 관찰된 동작이 없습니다.", style = DenebType.body, color = denebHint())
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun ObserveKeyCountRow(key: String, count: Int, emphasize: Boolean) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp)) {
        Text(key, style = DenebType.rowTitle, color = MaterialTheme.colorScheme.onBackground)
        Text(
            "${count}회",
            style = DenebType.snippet,
            color = if (emphasize) MaterialTheme.colorScheme.error else denebHint(),
        )
    }
    HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
}

@Composable
private fun ObserveLogRow(line: ObserveLogLine) {
    var expanded by remember(line.ts, line.msg, line.runId) { mutableStateOf(false) }
    val haptics = rememberHaptics()
    Column(
        Modifier
            .fillMaxWidth()
            .handCursor()
            .clickable {
                haptics.tap()
                expanded = !expanded
            }
            .padding(horizontal = 16.dp, vertical = 10.dp),
    ) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(
                line.level,
                style = DenebType.sectionLabel,
                color = if (line.level.equals("ERROR", ignoreCase = true)) {
                    MaterialTheme.colorScheme.error
                } else {
                    denebHint()
                },
            )
            if (line.runId.isNotBlank()) {
                Text(
                    shortRunId(line.runId),
                    style = DenebType.meta,
                    color = denebHint(),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        Text(
            line.msg,
            style = DenebType.body,
            color = MaterialTheme.colorScheme.onBackground,
            maxLines = if (expanded) Int.MAX_VALUE else 3,
            overflow = TextOverflow.Ellipsis,
        )
    }
    HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
}

// Flat period switcher in the Deneb idiom (mirrors SkillsViewSwitcher): a flat
// text switcher over a shared hairline, no capsule or fill. The active span is
// the one interactive accent (primary), the rest muted hint. Selecting a window
// re-queries behavior + logs for that span. View navigation (not a form input),
// so presentation is Deneb while each label keeps Material selectable + Role.Tab.
@Composable
private fun ObservePeriodSwitcher(days: Int, onSelect: (Int) -> Unit) {
    val haptics = rememberHaptics()
    Column(Modifier.fillMaxWidth()) {
        Row(
            Modifier.padding(horizontal = 16.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            listOf("1일" to 1, "7일" to 7).forEach { (label, d) ->
                val selected = days == d
                Text(
                    label,
                    style = if (selected) DenebType.rowTitleStrong else DenebType.rowTitle,
                    color = if (selected) MaterialTheme.colorScheme.primary else denebHint(),
                    modifier = Modifier
                        .handCursor()
                        .selectable(
                            selected = selected,
                            role = Role.Tab,
                            onClick = {
                                if (!selected) {
                                    haptics.tap()
                                    onSelect(d)
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

// Tracked-caps section header in the Deneb idiom (mirrors [ai.deneb.ui.DenebSectionLabel]),
// but laid out inside a LazyColumn item so it keeps its own horizontal inset.
@Composable
private fun ObserveSectionHeader(text: String) {
    Text(
        text.uppercase(),
        style = DenebType.sectionLabel,
        color = denebHint(),
        modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 18.dp, bottom = 6.dp),
    )
}

private fun toolAnomalySnippet(t: ObserveToolStat): String? {
    val parts = buildList {
        if (t.repaired > 0) add("수리 ${t.repaired}")
        if (t.blocked > 0) add("차단 ${t.blocked}")
        if (t.unknown > 0) add("미지 ${t.unknown}")
        if (t.cacheHits > 0) add("캐시 ${t.cacheHits}")
        if (t.truncated > 0) add("절단 ${t.truncated}")
    }
    return parts.takeIf { it.isNotEmpty() }?.joinToString(" · ")
}

private fun shortRunId(runId: String): String = if (runId.length <= 12) runId else runId.take(8) + "…"

private fun formatCount(n: Long): String = when {
    n >= 1_000_000 -> "${n / 1_000_000}M"
    n >= 1_000 -> "${n / 1_000}k"
    else -> n.toString()
}

private fun formatPct(pct: Double): String {
    val rounded = (pct * 10.0).toInt() / 10.0
    return if (rounded == rounded.toLong().toDouble()) rounded.toLong().toString() else rounded.toString()
}
