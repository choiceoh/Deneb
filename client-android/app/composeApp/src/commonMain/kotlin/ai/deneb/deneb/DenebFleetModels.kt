package ai.deneb.deneb

import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebUnderlineSearchField
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.datetime.Instant
import kotlin.time.Clock

// 모델 tab: HF model browse/download onto fleet nodes — split from
// DenebFleetScreen.kt (pure move; cross-file symbols widened private→internal).

// --- 모델 tab -------------------------------------------------------------------

private fun fmtParamsK(p: Long): String = when {
    p <= 0 -> "?"
    p >= 1_000_000_000_000 -> "${(p / 100_000_000_000) / 10.0}T"
    p >= 10_000_000_000 -> "${p / 1_000_000_000}B"
    p >= 1_000_000_000 -> "${(p / 100_000_000) / 10.0}B"
    else -> "${p / 1_000_000}M"
}

private fun fmtBytes(b: Long): String {
    if (b <= 0) return "0 B"
    val units = listOf("B", "KiB", "MiB", "GiB", "TiB")
    var v = b.toDouble()
    var i = 0
    while (v >= 1024 && i < units.lastIndex) {
        v /= 1024
        i++
    }
    return if (v >= 100 || i <= 1) "${v.toInt()} ${units[i]}" else "${(v * 10).toInt() / 10.0} ${units[i]}"
}

// HF list sort keys SparkFleet whitelists, in the order the chips show them.
private val hfSortOptions = listOf(
    "" to "트렌딩",
    "downloads" to "다운로드",
    "likes" to "좋아요",
    "lastModified" to "최신",
)

// hfMeta is the second line of a search result — counts, task, and freshness:
// "↓ 1.2M · ♥ 340 · text-generation · 3일 전".
private fun hfMeta(m: FleetHFModel): String {
    val parts = mutableListOf("↓ ${fmtCount(m.downloads)}", "♥ ${fmtCount(m.likes)}")
    if (m.pipelineTag.isNotBlank()) parts += m.pipelineTag
    hfAge(m.lastModified).takeIf { it.isNotBlank() }?.let { parts += it }
    return parts.joinToString(" · ")
}

// fmtCount renders large download/like counts compactly (1.2M, 12.3K, 999).
private fun fmtCount(n: Long): String = when {
    n >= 1_000_000 -> "${(n / 100_000) / 10.0}M"
    n >= 1_000 -> "${(n / 100) / 10.0}K"
    else -> n.toString()
}

// hfAge turns an RFC3339 lastModified into a coarse Korean relative age; "" when
// the hub omitted it or the timestamp can't be parsed.
private fun hfAge(iso: String): String {
    if (iso.isBlank()) return ""
    val inst = runCatching { Instant.parse(iso) }.getOrNull() ?: return ""
    val days = (Clock.System.now() - inst).inWholeDays
    return when {
        days < 0 -> ""
        days == 0L -> "오늘"
        days < 30 -> "${days}일 전"
        days < 365 -> "${days / 30}개월 전"
        else -> "${days / 365}년 전"
    }
}

// nodeFreeBytes is the free space on a node's roomiest disk (where new weights
// most plausibly land), or -1 when the node/metrics are unknown.
private fun nodeFreeBytes(node: FleetNode?): Long {
    val disk = node?.metrics?.disks?.maxByOrNull { it.totalKB - it.usedKB } ?: return -1
    return (disk.totalKB - disk.usedKB) * 1024
}

/**
 * 모델 tab: HuggingFace search → size preview → download to a node (all through
 * the fleet passthrough; the gateway's stored HF token covers gated repos), plus
 * each node's on-disk model inventory so "where are the weights" has an answer
 * in the app.
 */
@Composable
internal fun FleetModelsPage(client: DenebGatewayClient, nodes: List<FleetNode>, onNotice: (String) -> Unit) {
    var query by remember { mutableStateOf("") }
    var sort by remember { mutableStateOf("") } // "" = trending (hub default)
    var searching by remember { mutableStateOf(false) }
    var results by remember { mutableStateOf<List<FleetHFModel>?>(null) }
    var dlTarget by remember { mutableStateOf<FleetHFModel?>(null) }

    // Search as you type, debounced. Sort is a *server* param (the hub caps at 50
    // results), so a sort change re-fetches rather than reordering one page.
    // LaunchedEffect cancels the prior run on each keystroke, so delay() debounces.
    LaunchedEffect(query, sort) {
        val q = query.trim()
        if (q.isBlank()) {
            results = null
            searching = false
            return@LaunchedEffect
        }
        searching = true
        delay(450)
        results = client.fleetHFSearch(q, sort) ?: emptyList()
        searching = false
    }

    LazyColumn(Modifier.fillMaxSize()) {
        item(key = "hf-search") {
            Column(
                Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                DenebUnderlineSearchField(
                    query = query,
                    onQueryChange = { query = it },
                    placeholder = "HuggingFace 모델 검색",
                    // Dense admin screen — smaller than the search screen's airy default.
                    textStyle = DenebType.body,
                    // Live, debounced as-you-type (see LaunchedEffect above) — no submit.
                    trailing = {
                        when {
                            searching -> CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                            query.isNotBlank() -> TextButton(onClick = { query = "" }) { Text("✕") }
                        }
                    },
                )
                // Server-side sort (trending / downloads / likes / recent) — only
                // meaningful once there's a query to sort.
                if (query.isNotBlank()) {
                    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        hfSortOptions.forEach { (key, label) ->
                            val sel = sort == key
                            Surface(
                                shape = RoundedCornerShape(50),
                                color = if (sel) MaterialTheme.colorScheme.primary.copy(alpha = 0.2f) else MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.4f),
                                modifier = Modifier.clip(RoundedCornerShape(50)).clickable { sort = key },
                            ) {
                                Text(
                                    label,
                                    style = DenebType.meta,
                                    color = if (sel) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
                                    modifier = Modifier.padding(horizontal = 12.dp, vertical = 6.dp),
                                )
                            }
                        }
                    }
                }
            }
        }
        results?.let { rs ->
            if (rs.isEmpty() && !searching) {
                item(key = "hf-none") { FleetMuted("결과 없음") }
            }
            items(rs, key = { "hf-" + it.id }) { m ->
                Row(
                    Modifier.fillMaxWidth().clickable { dlTarget = m }.padding(horizontal = 16.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Column(Modifier.weight(1f)) {
                        Text(
                            m.id + if (m.gated) " 🔒" else "",
                            style = DenebType.rowTitle,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        Text(
                            hfMeta(m),
                            style = DenebType.rowSubtitle,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                    Surface(shape = RoundedCornerShape(50), color = MaterialTheme.colorScheme.primary.copy(alpha = 0.12f)) {
                        Text(
                            fmtParamsK(m.params),
                            style = DenebType.meta,
                            color = MaterialTheme.colorScheme.primary,
                            modifier = Modifier.padding(horizontal = 8.dp, vertical = 2.dp),
                        )
                    }
                }
                HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
            }
        }
        nodes.filter { it.models.isNotEmpty() }.forEach { node ->
            item(key = "mh-" + node.name) { FleetSectionHeader("${node.name} 보유 모델 · ${node.models.size}") }
            items(node.models, key = { "m-" + node.name + "-" + it.name }) { m ->
                Row(
                    Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(m.name, style = DenebType.rowTitle, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                    Text(fmtBytes(m.sizeBytes), style = DenebType.meta, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
        item(key = "models-pad") { Spacer(Modifier.height(24.dp)) }
    }

    dlTarget?.let { m ->
        FleetDownloadDialog(client, m, nodes, onDismiss = { dlTarget = null }, onNotice = onNotice)
    }
}

@Composable
private fun FleetDownloadDialog(
    client: DenebGatewayClient,
    model: FleetHFModel,
    nodes: List<FleetNode>,
    onDismiss: () -> Unit,
    onNotice: (String) -> Unit,
) {
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()
    var info by remember(model.id) { mutableStateOf<FleetHFInfo?>(null) }
    // 운영 규칙: 새 가중치는 우선 저장 노드(마스터 저장소)로.
    var target by remember(model.id) {
        mutableStateOf(nodes.firstOrNull { it.role == "storage" }?.name ?: nodes.firstOrNull()?.name.orEmpty())
    }
    LaunchedEffect(model.id) { info = client.fleetHFInfo(model.id) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("모델 다운로드") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(model.id, style = DenebType.rowTitle)
                Text(
                    info?.let { "총 ${fmtBytes(it.sizeBytes)} · ${it.files} files" + (if (it.gated) " · 🔒 gated" else "") }
                        ?: "크기 조회 중…",
                    style = DenebType.meta,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text("다운로드할 노드", style = DenebType.hint, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    nodes.forEach { n ->
                        val sel = target == n.name
                        Surface(
                            shape = RoundedCornerShape(50),
                            color = if (sel) MaterialTheme.colorScheme.primary.copy(alpha = 0.2f) else MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.4f),
                            modifier = Modifier.clip(RoundedCornerShape(50)).clickable { target = n.name },
                        ) {
                            Text(
                                n.name,
                                style = DenebType.meta,
                                color = if (sel) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
                                modifier = Modifier.padding(horizontal = 12.dp, vertical = 6.dp),
                            )
                        }
                    }
                }
                // Disk-fit check: weights are big and the nodes are tight, so warn
                // before a download that won't fit the target node's freest disk.
                val free = nodeFreeBytes(nodes.firstOrNull { it.name == target })
                val size = info?.sizeBytes ?: 0L
                val fits = free < 0 || size <= 0 || size <= free
                Text(
                    buildString {
                        append("노드 여유 ")
                        append(if (free < 0) "조회 불가" else fmtBytes(free))
                        if (!fits) append(" · ⚠ 공간 부족")
                    },
                    style = DenebType.meta,
                    color = if (!fits) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text(
                    "진행 상황은 작업 탭에서 실시간으로 보입니다 · 재시작하면 이어받습니다",
                    style = DenebType.hint,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        },
        confirmButton = {
            TextButton(
                enabled = target.isNotBlank(),
                onClick = {
                    haptics.confirm()
                    onDismiss()
                    scope.launch {
                        val err = client.fleetDownloadModel(target, model.id) { jobId ->
                            onNotice("다운로드 시작: ${model.id} → $target (작업 $jobId)")
                        }
                        if (err != null) onNotice(err)
                    }
                },
            ) { Text("⬇ 다운로드") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("취소") } },
    )
}
