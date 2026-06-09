package ai.deneb.deneb

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

// Settings hub "관찰" (Observe) tab: the gateway's own behavior + recent
// warn/error logs via miniapp.observe.*. The native adapter over the observe
// plane (CLI and chat tool are the other two). Read-only — an operator
// dashboard, not controls. Hosted by [DenebConfigScreen]'s pager.
@Composable
internal fun ObserveTab(client: DenebGatewayClient) {
    var behavior by remember { mutableStateOf<ObserveBehavior?>(null) }
    var logs by remember { mutableStateOf<List<ObserveLogLine>>(emptyList()) }
    var loading by remember { mutableStateOf(true) }
    var failed by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    suspend fun load() {
        loading = true
        failed = false
        val b = client.observeBehavior(7)
        val l = client.observeLogs("warn", 40)
        behavior = b
        logs = l?.lines ?: emptyList()
        failed = b == null && l == null
        loading = false
    }
    LaunchedEffect(Unit) { load() }
    when {
        loading -> DenebLoading()
        failed -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            DenebError("관찰 데이터를 불러오지 못했습니다.", onRetry = { scope.launch { load() } })
        }
        else -> LazyColumn(Modifier.fillMaxSize()) {
            behavior?.let { b ->
                item {
                    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 14.dp)) {
                        Text("최근 7일 동작", style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold, color = MaterialTheme.colorScheme.onSurface)
                        Text(
                            "실행 ${b.runs}회 · 능동 ${b.proactiveRuns} · 압축 ${b.compactedRuns}",
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    HorizontalDivider(Modifier.padding(start = 16.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
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
                        HorizontalDivider(Modifier.padding(start = 16.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
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
                    HorizontalDivider(Modifier.padding(start = 16.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
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
