package ai.deneb.deneb

import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.handCursor
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

// Settings hub "관찰" (Observe) tab: the gateway's own behavior + recent
// warn/error logs via miniapp.observe.*. The native adapter over the observe
// plane (CLI and chat tool are the other two). Read-only — an operator
// dashboard, not controls. A flat 1일/7일 switcher scopes the window (behavior +
// logs re-query for the span). Hosted by [DenebConfigScreen]'s pager.
@Composable
internal fun ObserveTab(client: DenebGatewayClient) {
    var selectedDays by remember { mutableStateOf(7) }
    var behavior by remember { mutableStateOf<ObserveBehavior?>(null) }
    var logs by remember { mutableStateOf<List<ObserveLogLine>>(emptyList()) }
    var loading by remember { mutableStateOf(true) }
    var failed by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    suspend fun load() {
        loading = true
        failed = false
        val b = client.observeBehavior(selectedDays)
        val l = client.observeLogs("warn", 40, selectedDays)
        behavior = b
        logs = l?.lines ?: emptyList()
        failed = b == null && l == null
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
                    behavior?.let { b ->
                        item {
                            Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 14.dp)) {
                                Text("최근 ${selectedDays}일 동작", style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold, color = MaterialTheme.colorScheme.onSurface)
                                Text(
                                    "실행 ${b.runs}회 · 능동 ${b.proactiveRuns} · 압축 ${b.compactedRuns}",
                                    style = MaterialTheme.typography.bodyMedium,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                            HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
                        }
                        if (b.tools.isNotEmpty()) {
                            item { ObserveSectionHeader("도구 사용") }
                            items(b.tools, key = { it.name }) { t ->
                                Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp)) {
                                    Text(t.name, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
                                    Text(
                                        if (t.errors > 0) "${t.calls}회 · ${t.errors} 오류 · 평균 ${t.avgMs}ms" else "${t.calls}회 · 평균 ${t.avgMs}ms",
                                        style = MaterialTheme.typography.bodySmall,
                                        color = if (t.errors > 0) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant,
                                    )
                                }
                                HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
                            }
                        }
                    }
                    if (logs.isNotEmpty()) {
                        item { ObserveSectionHeader("최근 경고 / 오류") }
                        items(logs.size) { i ->
                            val l = logs[i]
                            Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp)) {
                                Text(
                                    l.level,
                                    style = MaterialTheme.typography.labelSmall,
                                    fontWeight = FontWeight.SemiBold,
                                    color = if (l.level == "ERROR") MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.tertiary,
                                )
                                Text(l.msg, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurface, maxLines = 3, overflow = TextOverflow.Ellipsis)
                            }
                            HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
                        }
                    }
                    if ((behavior?.runs ?: 0) == 0 && logs.isEmpty()) {
                        item {
                            Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) {
                                Text("아직 관찰된 동작이 없습니다.", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                        }
                    }
                }
            }
        }
    }
}

// Flat period switcher in the Deneb idiom (mirrors SkillsViewSwitcher): ink-vs-
// hint text over a shared hairline, no capsule or fill. Selecting a window
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
                    style = MaterialTheme.typography.bodyMedium,
                    fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal,
                    color = if (selected) MaterialTheme.colorScheme.onSurface else denebHint(),
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

@Composable
private fun ObserveSectionHeader(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.labelMedium,
        fontWeight = FontWeight.SemiBold,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 18.dp, bottom = 6.dp),
    )
}
