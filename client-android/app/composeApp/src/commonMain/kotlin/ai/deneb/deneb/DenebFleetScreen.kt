package ai.deneb.deneb

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
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
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.deneb.Platform
import ai.deneb.currentPlatform
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.handCursor
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

/**
 * Fleet management as its own full screen (NOT a settings tab — the settings
 * hub stays configuration-only; running GPU nodes is an operational surface,
 * like mail or people). The frame deliberately mirrors [DenebConfigScreen]:
 * title row + pill tab bar + pager, with 노드 / 레시피 / 작업 as the tabs.
 *
 * Data flows through the gateway's authenticated SparkFleet passthrough
 * (DenebClientFleet); one poll loop at screen level feeds all three tabs, so a
 * launched recipe's health wait and a job's streamed log move live wherever
 * the user is looking. Reached from the desktop sidebar ("fleet") and the
 * 설정 게이트웨이 tab's 플릿 관리 entry (mobile).
 */
@Composable
fun DenebFleetScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val pagerState = rememberPagerState(pageCount = { FleetTab.entries.size })
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()

    var state by remember { mutableStateOf<FleetState?>(null) }
    var recipes by remember { mutableStateOf<List<FleetRecipe>?>(null) }
    var jobs by remember { mutableStateOf<List<FleetJob>?>(null) }
    var loaded by remember { mutableStateOf(false) }
    var stale by remember { mutableStateOf(false) }
    var notice by remember { mutableStateOf<String?>(null) }
    var confirm by remember { mutableStateOf<Pair<FleetRecipe, String>?>(null) }

    suspend fun refresh() {
        val st = client.fleetState()
        val rc = client.fleetRecipes()
        val jb = client.fleetJobs()
        st?.let { state = it }
        rc?.let { recipes = it }
        jb?.let { jobs = it }
        // Every fetch failing after a successful load means the fleet went away:
        // keep the last data on screen but flag it, instead of letting stale
        // green health pass for live (the retained values would otherwise look
        // current forever).
        stale = loaded && st == null && rc == null && jb == null
        loaded = true
    }
    // One poll loop for the whole screen: jobs stream their logs server-side,
    // so a short cadence keeps launch health-waits and transfers moving here.
    LaunchedEffect(Unit) {
        while (isActive) {
            refresh()
            delay(7_000)
        }
    }

    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(Modifier.fillMaxSize().statusBarsPadding()) {
            if (navigationTabBar != null) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) { navigationTabBar() }
            }
            Row(
                modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp, top = 12.dp, bottom = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text("플릿", style = DenebType.viewTitle, modifier = Modifier.weight(1f))
                if (currentPlatform !is Platform.Desktop) {
                    TextButton(onClick = onBack) { Text("닫기") }
                }
            }
            // Pill tab bar — same look as the settings hub so the two screens
            // read as siblings.
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .horizontalScroll(rememberScrollState())
                    .padding(horizontal = 12.dp, vertical = 4.dp),
                horizontalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                FleetTab.entries.forEachIndexed { idx, entry ->
                    val isSelected = pagerState.currentPage == idx
                    Surface(
                        modifier = Modifier
                            .handCursor()
                            .clip(RoundedCornerShape(50))
                            .selectable(
                                selected = isSelected,
                                role = Role.Tab,
                                onClick = { haptics.tap(); scope.launch { pagerState.animateScrollToPage(idx) } },
                            ),
                        shape = RoundedCornerShape(50),
                        color = if (isSelected) {
                            MaterialTheme.colorScheme.primary.copy(alpha = 0.2f)
                        } else {
                            Color.Transparent
                        },
                    ) {
                        Text(
                            text = entry.label,
                            modifier = Modifier.padding(horizontal = 16.dp, vertical = 10.dp),
                            color = if (isSelected) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
                            style = MaterialTheme.typography.labelLarge,
                            fontWeight = if (isSelected) FontWeight.SemiBold else FontWeight.Normal,
                            maxLines = 1,
                        )
                    }
                }
            }
            if (stale) {
                Text(
                    "⚠ 플릿 연결 끊김 — 마지막으로 받은 데이터를 표시 중입니다",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
                )
            }
            notice?.let { n ->
                Text(
                    n,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
                )
            }
            HorizontalPager(
                state = pagerState,
                modifier = Modifier.weight(1f).fillMaxWidth(),
            ) { page ->
                val unreachable = loaded && state?.nodes.isNullOrEmpty() && recipes.isNullOrEmpty()
                if (unreachable) {
                    Box(Modifier.fillMaxSize().padding(16.dp), contentAlignment = Alignment.Center) {
                        DenebError(
                            "플릿에 연결하지 못했습니다 — 게이트웨이의 DENEB_SPARKFLEET_URL 설정과 SparkFleet 동작 여부를 확인하세요.",
                            onRetry = { scope.launch { refresh() } },
                        )
                    }
                } else {
                    when (FleetTab.entries[page]) {
                        FleetTab.NODES -> FleetNodesPage(state?.nodes.orEmpty(), loaded)
                        FleetTab.RECIPES -> FleetRecipesPage(recipes.orEmpty(), loaded) { rc, action ->
                            haptics.tap(); confirm = rc to action
                        }
                        FleetTab.MODELS -> FleetModelsPage(client, state?.nodes.orEmpty()) { notice = it }
                        FleetTab.JOBS -> FleetJobsPage(client, jobs.orEmpty(), loaded) { notice = it }
                    }
                }
            }
        }
    }

    confirm?.let { (rc, action) ->
        val label = when (action) {
            "launch" -> "기동"
            "stop" -> "중지"
            "restart" -> "재시작"
            else -> action
        }
        // Per-launch memory overrides, prefilled from the recipe's vLLM block —
        // SparkFleet applies them to a clone, the recipe file never changes.
        var gmu by remember(rc.name) { mutableStateOf(rc.vllm?.gpuMemoryUtilization?.toString().orEmpty()) }
        var maxLen by remember(rc.name) { mutableStateOf(rc.vllm?.maxModelLen?.toString().orEmpty()) }
        var seqs by remember(rc.name) { mutableStateOf(rc.vllm?.maxNumSeqs?.toString().orEmpty()) }
        AlertDialog(
            onDismissRequest = { confirm = null },
            title = { Text("${rc.name} $label") },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("${rc.status.node.ifBlank { rc.node }} 노드에서 ${rc.name} 레시피를 $label 할까요?")
                    if (action == "launch" && rc.vllm != null) {
                        Text(
                            "이번 기동에만 적용되는 메모리 설정 (비우면 레시피 값)",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        OutlinedTextField(
                            value = gmu, onValueChange = { gmu = it },
                            label = { Text("GPU 메모리 사용률 (0–1)") }, singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                        OutlinedTextField(
                            value = maxLen, onValueChange = { maxLen = it },
                            label = { Text("최대 컨텍스트 (tokens)") }, singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                        OutlinedTextField(
                            value = seqs, onValueChange = { seqs = it },
                            label = { Text("동시 시퀀스") }, singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                }
            },
            confirmButton = {
                TextButton(onClick = {
                    confirm = null
                    val overrides = if (action == "launch") {
                        FleetVllm(
                            gpuMemoryUtilization = gmu.trim().toDoubleOrNull(),
                            maxModelLen = maxLen.trim().toIntOrNull(),
                            maxNumSeqs = seqs.trim().toIntOrNull(),
                        )
                    } else {
                        null
                    }
                    scope.launch {
                        val err = client.fleetRecipeAction(rc.name, action, overrides) { jobId ->
                            notice = "${rc.name} $label 시작됨 — 작업 $jobId 진행 상황은 작업 탭에서"
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

/** The fleet screen's tabs, in display order (same contract as ConfigTab). */
private enum class FleetTab(val label: String) {
    NODES("노드"),
    RECIPES("레시피"),
    MODELS("모델"),
    JOBS("작업"),
}

// --- pages -------------------------------------------------------------------

@Composable
private fun FleetNodesPage(nodes: List<FleetNode>, loaded: Boolean) {
    if (nodes.isEmpty()) {
        EmptyTab(if (loaded) "노드 정보가 없습니다." else "불러오는 중…")
        return
    }
    LazyColumn(Modifier.fillMaxSize()) {
        items(nodes, key = { it.name }) { node -> FleetNodeRow(node) }
    }
}

@Composable
private fun FleetRecipesPage(recipes: List<FleetRecipe>, loaded: Boolean, onAction: (FleetRecipe, String) -> Unit) {
    if (recipes.isEmpty()) {
        EmptyTab(if (loaded) "레시피가 없습니다." else "불러오는 중…")
        return
    }
    LazyColumn(Modifier.fillMaxSize()) {
        items(recipes, key = { it.name }) { rc ->
            FleetRecipeRow(rc = rc, onAction = { action -> onAction(rc, action) })
        }
    }
}

@Composable
private fun FleetJobsPage(client: DenebGatewayClient, jobs: List<FleetJob>, loaded: Boolean, onNotice: (String) -> Unit) {
    var openLogJob by remember { mutableStateOf<String?>(null) }
    var cancelTarget by remember { mutableStateOf<FleetJob?>(null) }
    val scope = rememberCoroutineScope()
    val recent = jobs.take(20)
    if (recent.isEmpty()) {
        EmptyTab(if (loaded) "진행 중인 작업이 없습니다." else "불러오는 중…")
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
        val downServices = node.metrics.services.filter { !it.ok }
        if (downServices.isNotEmpty()) {
            Text(
                "다운: " + downServices.joinToString(", ") { it.name },
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
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
                Text(label, style = MaterialTheme.typography.labelSmall, color = color, modifier = Modifier.padding(horizontal = 8.dp, vertical = 2.dp))
            }
            Text(job.title, style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
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
                    style = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
                    modifier = Modifier.padding(8.dp),
                )
            }
        }
    }
    HorizontalDivider(Modifier.padding(start = 16.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
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

/**
 * 모델 tab: HuggingFace search → size preview → download to a node (all through
 * the fleet passthrough; the gateway's stored HF token covers gated repos), plus
 * each node's on-disk model inventory so "where are the weights" has an answer
 * in the app.
 */
@Composable
private fun FleetModelsPage(client: DenebGatewayClient, nodes: List<FleetNode>, onNotice: (String) -> Unit) {
    val scope = rememberCoroutineScope()
    var query by remember { mutableStateOf("") }
    var searching by remember { mutableStateOf(false) }
    var results by remember { mutableStateOf<List<FleetHFModel>?>(null) }
    var dlTarget by remember { mutableStateOf<FleetHFModel?>(null) }

    LazyColumn(Modifier.fillMaxSize()) {
        item(key = "hf-search") {
            Column(
                Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                    OutlinedTextField(
                        value = query,
                        onValueChange = { query = it },
                        label = { Text("HuggingFace 모델 검색") },
                        singleLine = true,
                        modifier = Modifier.weight(1f),
                    )
                    TextButton(
                        enabled = !searching && query.isNotBlank(),
                        onClick = {
                            scope.launch {
                                searching = true
                                results = client.fleetHFSearch(query.trim()) ?: emptyList()
                                searching = false
                            }
                        },
                    ) { Text(if (searching) "검색 중…" else "검색") }
                }
            }
        }
        results?.let { rs ->
            if (rs.isEmpty()) {
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
                            style = MaterialTheme.typography.bodyMedium,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        Text(
                            "↓ ${m.downloads} · ♥ ${m.likes}",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    Surface(shape = RoundedCornerShape(50), color = MaterialTheme.colorScheme.primary.copy(alpha = 0.12f)) {
                        Text(
                            fmtParamsK(m.params),
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.primary,
                            modifier = Modifier.padding(horizontal = 8.dp, vertical = 2.dp),
                        )
                    }
                }
                HorizontalDivider(Modifier.padding(start = 16.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
            }
        }
        nodes.filter { it.models.isNotEmpty() }.forEach { node ->
            item(key = "mh-" + node.name) { FleetSectionHeader("${node.name} 보유 모델 · ${node.models.size}") }
            items(node.models, key = { "m-" + node.name + "-" + it.name }) { m ->
                Row(
                    Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(m.name, style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                    Text(fmtBytes(m.sizeBytes), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
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
                Text(model.id, style = MaterialTheme.typography.bodyMedium)
                Text(
                    info?.let { "총 ${fmtBytes(it.sizeBytes)} · ${it.files} files" + (if (it.gated) " · 🔒 gated" else "") }
                        ?: "크기 조회 중…",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text("다운로드할 노드", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
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
                                style = MaterialTheme.typography.labelMedium,
                                color = if (sel) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
                                modifier = Modifier.padding(horizontal = 12.dp, vertical = 6.dp),
                            )
                        }
                    }
                }
                Text(
                    "진행 상황은 작업 탭에서 실시간으로 보입니다 · 재시작하면 이어받습니다",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        },
        confirmButton = {
            TextButton(
                enabled = target.isNotBlank(),
                onClick = {
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
