package ai.deneb.deneb

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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.deneb.ui.components.rememberHaptics
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

/**
 * Settings hub "플릿" tab: manage the SparkFleet GPU control plane from the app
 * — node health (GPU/unified-memory/disk), recipe lifecycle (launch / stop /
 * restart, behind a confirm dialog), and background jobs with their streamed
 * logs. Data flows through the gateway's authenticated fleet passthrough; the
 * tab polls while visible so a launch's health-wait and job progress update
 * live. Hosted by [DenebConfigScreen]'s pager.
 */
@Composable
internal fun FleetTab(client: DenebGatewayClient) {
    var state by remember { mutableStateOf<FleetState?>(null) }
    var recipes by remember { mutableStateOf<List<FleetRecipe>?>(null) }
    var jobs by remember { mutableStateOf<List<FleetJob>?>(null) }
    var loaded by remember { mutableStateOf(false) }
    var notice by remember { mutableStateOf<String?>(null) }
    var confirm by remember { mutableStateOf<Pair<FleetRecipe, String>?>(null) }
    var openLogJob by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()

    suspend fun refresh() {
        state = client.fleetState() ?: state
        recipes = client.fleetRecipes() ?: recipes
        jobs = client.fleetJobs() ?: jobs
        loaded = true
    }
    // Poll while the tab is composed: a launched recipe's health-wait and job
    // logs stream server-side, so a short cadence keeps them moving here.
    LaunchedEffect(Unit) {
        while (isActive) {
            refresh()
            delay(7_000)
        }
    }

    val nodes = state?.nodes.orEmpty()
    if (loaded && nodes.isEmpty() && recipes.isNullOrEmpty()) {
        Box(Modifier.fillMaxSize().padding(16.dp), contentAlignment = Alignment.Center) {
            DenebError(
                "플릿에 연결하지 못했습니다 — 게이트웨이의 DENEB_SPARKFLEET_URL 설정과 SparkFleet 동작 여부를 확인하세요.",
                onRetry = { scope.launch { refresh() } },
            )
        }
        return
    }

    LazyColumn(Modifier.fillMaxSize()) {
        notice?.let { n ->
            item(key = "notice") {
                Text(
                    n,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp),
                )
            }
        }

        item(key = "h-nodes") { FleetSectionHeader("노드") }
        items(nodes, key = { "n-" + it.name }) { node -> FleetNodeRow(node) }

        item(key = "h-recipes") { FleetSectionHeader("레시피") }
        val rcs = recipes.orEmpty()
        if (rcs.isEmpty()) {
            item(key = "rc-empty") { FleetMuted(if (loaded) "레시피가 없습니다." else "불러오는 중…") }
        }
        items(rcs, key = { "r-" + it.name }) { rc ->
            FleetRecipeRow(
                rc = rc,
                onAction = { action -> haptics.tap(); confirm = rc to action },
            )
        }

        item(key = "h-jobs") { FleetSectionHeader("작업") }
        val js = jobs.orEmpty().take(10)
        if (js.isEmpty()) {
            item(key = "j-empty") { FleetMuted("진행 중인 작업이 없습니다.") }
        }
        items(js, key = { "j-" + it.id }) { job ->
            FleetJobRow(
                job = job,
                expanded = openLogJob == job.id,
                onToggle = { openLogJob = if (openLogJob == job.id) null else job.id },
            )
        }
        item(key = "tail-pad") { Spacer(Modifier.height(24.dp)) }
    }

    confirm?.let { (rc, action) ->
        val label = when (action) {
            "launch" -> "기동"
            "stop" -> "중지"
            "restart" -> "재시작"
            else -> action
        }
        AlertDialog(
            onDismissRequest = { confirm = null },
            title = { Text("${rc.name} $label") },
            text = { Text("${rc.status.node.ifBlank { rc.node }} 노드에서 ${rc.name} 레시피를 $label 할까요?") },
            confirmButton = {
                TextButton(onClick = {
                    confirm = null
                    scope.launch {
                        val err = client.fleetRecipeAction(rc.name, action) { jobId ->
                            notice = "${rc.name} $label 시작됨 — 작업 $jobId 진행 상황은 아래 작업 목록에서"
                        }
                        notice = err ?: notice ?: "${rc.name} $label 완료"
                        refresh()
                    }
                }) { Text(label) }
            },
            dismissButton = { TextButton(onClick = { confirm = null }) { Text("취소") } },
        )
    }
}

@Composable
private fun FleetSectionHeader(title: String) {
    Text(
        title,
        style = MaterialTheme.typography.labelLarge,
        fontWeight = FontWeight.SemiBold,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 18.dp, bottom = 6.dp),
    )
}

@Composable
private fun FleetMuted(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
    )
}

@Composable
private fun FleetDot(up: Boolean) {
    Box(
        Modifier.size(8.dp).clip(CircleShape).background(
            if (up) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.error,
        ),
    )
}

private fun gib(kb: Long): String {
    val g = kb / 1024.0 / 1024.0
    return if (g >= 100) "${g.toInt()}" else "${(g * 10).toInt() / 10.0}"
}

@Composable
private fun FleetNodeRow(node: FleetNode) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FleetDot(node.reachable)
            Text(node.name, style = MaterialTheme.typography.bodyLarge, fontWeight = FontWeight.Medium)
            if (node.role.isNotBlank()) {
                Text(node.role, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Spacer(Modifier.weight(1f))
            node.metrics.gpus.firstOrNull()?.let { g ->
                Text(
                    "GPU ${g.utilPct ?: "—"}% · ${g.tempC ?: "—"}℃",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        node.metrics.memory?.takeIf { it.totalKB > 0 }?.let { m ->
            val used = m.totalKB - m.availableKB
            Spacer(Modifier.height(6.dp))
            LinearProgressIndicator(
                progress = { (used.toFloat() / m.totalKB.toFloat()).coerceIn(0f, 1f) },
                modifier = Modifier.fillMaxWidth().height(4.dp).clip(RoundedCornerShape(2.dp)),
            )
            Text(
                "통합 메모리 ${gib(used)} / ${gib(m.totalKB)} GiB" +
                    (node.metrics.disks.firstOrNull()?.let { "  ·  디스크 ${it.usePct}%" } ?: ""),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        node.error?.takeIf { it.isNotBlank() }?.let {
            Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
    HorizontalDivider(Modifier.padding(start = 16.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
}

@Composable
private fun FleetRecipeRow(rc: FleetRecipe, onAction: (String) -> Unit) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FleetDot(rc.status.running)
            Column(Modifier.weight(1f)) {
                Text(rc.name, style = MaterialTheme.typography.bodyLarge, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(
                    listOfNotNull(
                        rc.status.node.ifBlank { rc.node }.takeIf { it.isNotBlank() },
                        rc.port.takeIf { it > 0 }?.let { ":$it" },
                        if (!rc.status.running && !rc.status.weightsPresent) "가중치 없음" else null,
                    ).joinToString(" · "),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            if (rc.status.running) {
                OutlinedButton(onClick = { onAction("restart") }) { Text("재시작") }
                OutlinedButton(onClick = { onAction("stop") }) { Text("중지", color = MaterialTheme.colorScheme.error) }
            } else {
                OutlinedButton(onClick = { onAction("launch") }) { Text("▶ 기동") }
            }
        }
    }
    HorizontalDivider(Modifier.padding(start = 16.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
}

@Composable
private fun FleetJobRow(job: FleetJob, expanded: Boolean, onToggle: () -> Unit) {
    Column(
        Modifier.fillMaxWidth().clickable(onClick = onToggle).padding(horizontal = 16.dp, vertical = 8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            val (label, color) = when (job.state) {
                "running" -> "진행" to MaterialTheme.colorScheme.tertiary
                "done" -> "완료" to MaterialTheme.colorScheme.primary
                else -> "실패" to MaterialTheme.colorScheme.error
            }
            Surface(shape = RoundedCornerShape(50), color = color.copy(alpha = 0.15f)) {
                Text(label, style = MaterialTheme.typography.labelSmall, color = color, modifier = Modifier.padding(horizontal = 8.dp, vertical = 2.dp))
            }
            Text(job.title, style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
        }
        if (expanded && job.log.isNotBlank()) {
            Spacer(Modifier.width(8.dp))
            Surface(
                color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f),
                shape = RoundedCornerShape(8.dp),
                modifier = Modifier.fillMaxWidth().padding(top = 6.dp),
            ) {
                Text(
                    job.log.takeLast(2000),
                    style = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
                    modifier = Modifier.padding(8.dp),
                )
            }
        }
    }
    HorizontalDivider(Modifier.padding(start = 16.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
}
