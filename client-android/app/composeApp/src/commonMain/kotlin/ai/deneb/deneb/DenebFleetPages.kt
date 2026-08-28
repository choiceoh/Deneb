package ai.deneb.deneb

import ai.deneb.ui.DenebType
import ai.deneb.ui.JetBrainsMonoFamily
import ai.deneb.ui.denebHairline
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

// Fleet tab pages (노드·레시피·작업) and their rows — split from
// DenebFleetScreen.kt (pure move; cross-file symbols widened private→internal).

// --- pages -------------------------------------------------------------------

@Composable
internal fun FleetNodesPage(nodes: List<FleetNode>, loaded: Boolean) {
    if (!loaded) {
        DenebLoading()
        return
    }
    if (nodes.isEmpty()) {
        EmptyTab("노드 정보가 없습니다.")
        return
    }
    LazyColumn(Modifier.fillMaxSize()) {
        items(nodes, key = { it.name }) { node -> FleetNodeRow(node) }
    }
}

@Composable
internal fun FleetJobsPage(client: DenebGatewayClient, jobs: List<FleetJob>, loaded: Boolean, onNotice: (String) -> Unit) {
    var openLogJob by remember { mutableStateOf<String?>(null) }
    var cancelTarget by remember { mutableStateOf<FleetJob?>(null) }
    val scope = rememberCoroutineScope()
    val recent = jobs.take(20)
    if (!loaded) {
        DenebLoading()
        return
    }
    if (recent.isEmpty()) {
        EmptyTab("진행 중인 작업이 없습니다.")
        return
    }
    LazyColumn(Modifier.fillMaxSize()) {
        items(recent, key = { it.id }) { job ->
            FleetJobRow(
                job = job,
                expanded = openLogJob == job.id,
                onToggle = { openLogJob = if (openLogJob == job.id) null else job.id },
                onCancel = { cancelTarget = job },
            )
        }
    }
    cancelTarget?.let { job ->
        AlertDialog(
            onDismissRequest = { cancelTarget = null },
            title = { Text("작업 취소") },
            text = { Text("\"${job.title}\" 작업을 취소할까요?\n전송류 작업은 재시도하면 끊긴 지점부터 이어받습니다.") },
            confirmButton = {
                TextButton(onClick = {
                    cancelTarget = null
                    scope.launch {
                        val err = client.fleetCancelJob(job.id)
                        onNotice(err ?: "작업 취소됨: ${job.id}")
                    }
                }) { Text("취소 실행", color = MaterialTheme.colorScheme.error) }
            },
            dismissButton = { TextButton(onClick = { cancelTarget = null }) { Text("닫기") } },
        )
    }
}

// --- rows ---------------------------------------------------------------------

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
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FleetDot(node.reachable)
            Text(node.name, style = DenebType.rowTitleStrong)
            if (node.role.isNotBlank()) {
                Text(node.role, style = DenebType.rowSubtitle, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Spacer(Modifier.weight(1f))
            node.metrics.gpus.firstOrNull()?.let { g ->
                Text(
                    "GPU ${g.utilPct ?: "—"}% · ${g.tempC ?: "—"}℃",
                    style = DenebType.meta,
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
                style = DenebType.meta,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        val downServices = node.metrics.services.filter { !it.ok }
        if (downServices.isNotEmpty()) {
            Text(
                "다운: " + downServices.joinToString(", ") { it.name },
                style = DenebType.meta,
                color = MaterialTheme.colorScheme.error,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        node.error?.takeIf { it.isNotBlank() }?.let {
            Text(it, style = DenebType.meta, color = MaterialTheme.colorScheme.error, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
    HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
}

@Composable
private fun FleetJobRow(job: FleetJob, expanded: Boolean, onToggle: () -> Unit, onCancel: () -> Unit = {}) {
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
                Text(label, style = DenebType.meta, color = color, modifier = Modifier.padding(horizontal = 8.dp, vertical = 2.dp))
            }
            Text(job.title, style = DenebType.rowTitle, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            if (job.state == "running") {
                TextButton(onClick = onCancel) { Text("취소", color = MaterialTheme.colorScheme.error) }
            }
        }
        if (expanded && job.log.isNotBlank()) {
            Surface(
                color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f),
                shape = RoundedCornerShape(8.dp),
                modifier = Modifier.fillMaxWidth().padding(top = 6.dp),
            ) {
                Text(
                    job.log.takeLast(2000),
                    style = DenebType.snippet.copy(fontFamily = JetBrainsMonoFamily()),
                    modifier = Modifier.padding(8.dp),
                )
            }
        }
    }
    HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
}

@Composable
internal fun FleetSectionHeader(title: String) {
    Text(
        title,
        style = DenebType.sectionLabel,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 18.dp, bottom = 6.dp),
    )
}

@Composable
internal fun FleetMuted(text: String) {
    Text(
        text,
        style = DenebType.meta,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
    )
}
