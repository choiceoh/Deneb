package ai.deneb.deneb

import ai.deneb.ui.DenebType
import ai.deneb.ui.denebHairline
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.HorizontalDivider
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
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

// 진단 (logs/triage/drift dialogs) + 벤치 tab — split from
// DenebFleetScreen.kt (pure move; cross-file symbols widened private→internal).

// --- 진단 (로그 / triage / drift) ------------------------------------------------

/** Raw `docker logs --tail` for a running recipe's container, in a scrollable
 *  monospace pane with a refresh. Read-only — opens without a confirm. */
@Composable
internal fun FleetLogsDialog(client: DenebGatewayClient, rc: FleetRecipe, onDismiss: () -> Unit) {
    val scope = rememberCoroutineScope()
    val node = rc.status.node.ifBlank { rc.node }
    var logs by remember(rc.name) { mutableStateOf<String?>(null) }
    var loading by remember(rc.name) { mutableStateOf(true) }
    suspend fun load() {
        loading = true
        logs = client.fleetContainerLogs(node, rc.container)
        loading = false
    }
    LaunchedEffect(rc.name) { load() }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("${rc.container} 로그") },
        text = {
            Surface(
                color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f),
                shape = RoundedCornerShape(8.dp),
                modifier = Modifier.fillMaxWidth().height(360.dp),
            ) {
                val scroll = rememberScrollState()
                // Pin to the newest line whenever the log (re)loads.
                LaunchedEffect(logs) { scroll.scrollTo(scroll.maxValue) }
                // null = fetch failed (vs "" = genuinely empty) — keep that signal so a
                // failing gateway/SparkFleet path doesn't read as "container has no logs".
                val text = logs
                Text(
                    when {
                        loading && text == null -> "불러오는 중…"
                        text == null -> "로그를 불러오지 못했습니다 — 새로고침으로 재시도하세요."
                        text.isBlank() -> "로그가 없습니다."
                        else -> text.takeLast(8000)
                    },
                    style = DenebType.snippet.copy(fontFamily = FontFamily.Monospace),
                    modifier = Modifier.verticalScroll(scroll).padding(8.dp),
                )
            }
        },
        confirmButton = { TextButton(onClick = { scope.launch { load() } }) { Text("새로고침") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("닫기") } },
    )
}

/** On-demand crash triage of a running container: docker state + matched failure
 *  patterns (cause/fix) + a fleet-LLM root-cause note, plus recipe-vs-container
 *  config drift. All read-only via the passthrough. */
@Composable
internal fun FleetDiagnoseDialog(client: DenebGatewayClient, rc: FleetRecipe, onDismiss: () -> Unit) {
    val scope = rememberCoroutineScope()
    val node = rc.status.node.ifBlank { rc.node }
    var diag by remember(rc.name) { mutableStateOf<FleetDiagnosis?>(null) }
    var drift by remember(rc.name) { mutableStateOf<FleetDrift?>(null) }
    var loading by remember(rc.name) { mutableStateOf(true) }
    suspend fun load() {
        loading = true
        diag = client.fleetDiagnose(node, rc.container)
        drift = client.fleetDrift(rc.name, node)
        loading = false
    }
    LaunchedEffect(rc.name) { load() }
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("${rc.name} 진단") },
        text = {
            Column(
                Modifier.fillMaxWidth().heightIn(max = 420.dp).verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                val d = diag
                val dr = drift
                if (loading && d == null && dr == null) {
                    Text("진단 중… (로컬 LLM 분석은 몇 초 걸릴 수 있습니다)", style = DenebType.meta, color = muted)
                } else {
                    // Crash triage (assist/logs). null after loading = THIS fetch failed —
                    // report it independently so a successful drift check below can't make
                    // the triage section read as clean ("설정 일치 ✓" with no findings).
                    if (!loading && d == null) {
                        Text("크래시 진단을 불러오지 못했습니다 — '다시 진단'으로 재시도하세요.", style = DenebType.meta, color = MaterialTheme.colorScheme.error)
                    }
                    d?.let {
                        if (it.state.isNotBlank()) {
                            Text("상태: ${it.state}", style = DenebType.meta, color = muted)
                        }
                        it.findings.forEach { f ->
                            Column {
                                Text("• ${f.cause}", style = DenebType.rowTitle)
                                if (f.fix.isNotBlank()) {
                                    Text("  → ${f.fix}", style = DenebType.rowSubtitle, color = muted)
                                }
                            }
                        }
                        if (it.llm.isNotBlank()) {
                            Text("AI 분석", style = DenebType.sectionLabel, color = muted)
                            Text(it.llm, style = DenebType.body)
                        }
                        if (it.findings.isEmpty() && it.llm.isBlank()) {
                            Text("알려진 실패 패턴이 없습니다.", style = DenebType.meta, color = muted)
                        }
                    }
                    // Config drift (supplementary).
                    if (!loading && dr == null) {
                        HorizontalDivider(color = denebHairline())
                        Text("설정 드리프트 확인을 불러오지 못했습니다.", style = DenebType.meta, color = muted)
                    }
                    dr?.let {
                        HorizontalDivider(color = denebHairline())
                        if (!it.inSync && it.diffs.isNotEmpty()) {
                            Text("⚠ 레시피 드리프트 — 컨테이너가 레시피와 다릅니다", style = DenebType.meta, color = MaterialTheme.colorScheme.error)
                            it.diffs.forEach { diff -> Text("• $diff", style = DenebType.meta, color = muted) }
                        } else if (it.inSync) {
                            Text("레시피와 컨테이너 설정 일치 ✓", style = DenebType.meta, color = muted)
                        }
                    }
                }
            }
        },
        confirmButton = { TextButton(onClick = { scope.launch { load() } }) { Text("다시 진단") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("닫기") } },
    )
}

// --- 벤치 tab -------------------------------------------------------------------

/**
 * 벤치 tab: each recipe's latest benchmark — overall score + the four headline
 * metrics (도구·언어·논리·안정) + a speed probe — with a 측정 button that starts a
 * run (it streams into the 작업 tab). Mirrors SparkFleet's own benchmark surface.
 */
@Composable
internal fun FleetBenchPage(
    client: DenebGatewayClient,
    recipes: List<FleetRecipe>,
    jobs: List<FleetJob>,
    loaded: Boolean,
    onNotice: (String) -> Unit,
) {
    val scope = rememberCoroutineScope()
    var evals by remember { mutableStateOf<FleetEvals?>(null) }
    // True when the last /api/evals attempt failed with nothing cached — so the tab can
    // say "data failed to load" instead of rendering every row as 측정 안 됨 (which would
    // hide a gateway/SparkFleet regression behind a normal-looking empty state).
    var evalsFailed by remember { mutableStateOf(false) }
    // Recipes with a benchmark in flight (name -> job id; "" = POST not yet acked).
    // Disables 측정 so a quick double-tap can't fire duplicate, expensive eval jobs.
    var launching by remember { mutableStateOf<Map<String, String>>(emptyMap()) }
    suspend fun load() {
        val r = client.fleetEvals()
        if (r != null) evals = r // keep the last good data on a transient poll failure
        evalsFailed = r == null
    }
    // Poll like the screen loop: a 측정 run returns a job id immediately and writes
    // its result only when the background job finishes, so a one-shot load would
    // leave the row stale. Re-fetching on the same cadence picks the new score up
    // within a few seconds (and stops when the tab leaves composition).
    LaunchedEffect(Unit) {
        while (isActive) {
            load()
            delay(7_000)
        }
    }
    // Re-enable 측정 once its job reaches a terminal state in the polled job list.
    LaunchedEffect(jobs) {
        if (launching.isEmpty()) return@LaunchedEffect
        val terminal = jobs.filter { it.state == "done" || it.state == "failed" }.map { it.id }.toSet()
        val finished = launching.filterValues { it.isNotEmpty() && it in terminal }.keys
        if (finished.isNotEmpty()) launching = launching - finished
    }
    // Busy state is also DERIVED from the polled jobs, not just this composition's
    // launches — so a benchmark started from the dashboard, before this page
    // existed, or across an app recreate still disables 측정. SparkFleet titles eval
    // jobs "benchmark <recipe> on <node>" (and rejects a concurrent eval server-side).
    val runningBench = remember(jobs) {
        jobs.filter { it.state == "running" }.mapNotNull { benchRecipeFromTitle(it.title) }.toSet()
    }
    if (recipes.isEmpty()) {
        EmptyTab(if (loaded) "레시피가 없습니다." else "불러오는 중…")
        return
    }
    LazyColumn(Modifier.fillMaxSize()) {
        if (evals == null && evalsFailed) {
            item(key = "evals-error") {
                Box(Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
                    DenebError("벤치마크 데이터를 불러오지 못했습니다.", onRetry = { scope.launch { load() } })
                }
            }
        }
        items(recipes, key = { it.name }) { rc ->
            val busy = rc.name in launching || rc.name in runningBench
            FleetBenchRow(rc, evals?.runs?.get(rc.name), busy = busy) {
                if (busy) return@FleetBenchRow
                launching = launching + (rc.name to "")
                scope.launch {
                    val err = client.fleetRunBench(rc.name, rc.status.node.ifBlank { rc.node }) { jobId ->
                        launching = launching + (rc.name to jobId)
                        onNotice("${rc.name} 벤치마크 시작 — 작업 $jobId (작업 탭). 결과는 끝나면 갱신됩니다")
                    }
                    if (err != null) {
                        onNotice(err)
                        launching = launching - rc.name
                    }
                }
            }
        }
        item(key = "bench-pad") { Spacer(Modifier.height(24.dp)) }
    }
}

private val benchCategoryLabels = listOf("tool" to "도구", "language" to "언어", "reasoning" to "논리", "stability" to "안정")

// SparkFleet titles a benchmark job "benchmark <recipe> on <node>" (RunEval).
// Recover the recipe so a running eval job disables that recipe's 측정 button.
private fun benchRecipeFromTitle(title: String): String? {
    val rest = title.removePrefix("benchmark ")
    if (rest == title) return null // not a benchmark job
    val onIdx = rest.lastIndexOf(" on ")
    val name = if (onIdx > 0) rest.substring(0, onIdx) else rest
    return name.takeIf { it.isNotBlank() }
}

@Composable
private fun FleetBenchRow(rc: FleetRecipe, run: FleetEvalRun?, busy: Boolean, onRun: () -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Column(Modifier.weight(1f)) {
                Text(rc.name, style = DenebType.rowTitleStrong, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(
                    if (run != null) "${run.pass}/${run.total} 통과" else "측정 안 됨",
                    style = DenebType.meta,
                    color = muted,
                )
            }
            run?.let {
                Text(
                    "${it.score.toInt()}",
                    style = DenebType.subject,
                    fontWeight = FontWeight.SemiBold,
                    color = MaterialTheme.colorScheme.primary,
                )
            }
            OutlinedButton(onClick = onRun, enabled = rc.status.running && !busy) {
                Text(if (busy) "측정 중…" else "측정")
            }
        }
        run?.categories?.takeIf { it.isNotEmpty() }?.let { cats ->
            Text(
                benchCategoryLabels.mapNotNull { (k, lab) -> cats[k]?.let { "$lab ${it.toInt()}" } }.joinToString("  ·  "),
                style = DenebType.meta,
                color = muted,
            )
        }
        run?.speed?.let { sp ->
            Text(
                "${sp.decodeTokPerSec.toInt()} tok/s · TTFT ${sp.ttftMs.toInt()}ms",
                style = DenebType.meta,
                color = muted,
            )
        }
    }
    HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
}
