package ai.deneb.deneb

import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.handCursor
import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
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
 * the user is looking. Reached from the 설정 게이트웨이 tab's 플릿 관리 entry
 * (the retired desktop sidebar's "fleet" entry was the old desktop route).
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
    // Read-only diagnostics open directly (no confirm): container logs / crash triage.
    var logsTarget by remember { mutableStateOf<FleetRecipe?>(null) }
    var diagnoseTarget by remember { mutableStateOf<FleetRecipe?>(null) }

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
                TextButton(onClick = onBack) { Text("닫기") }
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
                                onClick = {
                                    haptics.tap()
                                    scope.launch { pagerState.animateScrollToPage(idx) }
                                },
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
                            style = DenebType.rowTitle,
                            fontWeight = if (isSelected) FontWeight.SemiBold else FontWeight.Normal,
                            maxLines = 1,
                        )
                    }
                }
            }
            if (stale) {
                Text(
                    "⚠ 플릿 연결 끊김 — 마지막으로 받은 데이터를 표시 중입니다",
                    style = DenebType.meta,
                    color = MaterialTheme.colorScheme.error,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
                )
            }
            notice?.let { n ->
                Text(
                    n,
                    style = DenebType.meta,
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
                            haptics.tap()
                            when (action) {
                                "logs" -> logsTarget = rc
                                "diagnose" -> diagnoseTarget = rc
                                else -> confirm = rc to action
                            }
                        }

                        FleetTab.MODELS -> FleetModelsPage(client, state?.nodes.orEmpty()) { notice = it }

                        FleetTab.BENCH -> FleetBenchPage(client, recipes.orEmpty(), jobs.orEmpty(), loaded) { notice = it }

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
                            style = DenebType.hint,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        OutlinedTextField(
                            value = gmu,
                            onValueChange = { gmu = it },
                            label = { Text("GPU 메모리 사용률 (0–1)") },
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                        OutlinedTextField(
                            value = maxLen,
                            onValueChange = { maxLen = it },
                            label = { Text("최대 컨텍스트 (tokens)") },
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                        OutlinedTextField(
                            value = seqs,
                            onValueChange = { seqs = it },
                            label = { Text("동시 시퀀스") },
                            singleLine = true,
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

    logsTarget?.let { rc -> FleetLogsDialog(client, rc) { logsTarget = null } }
    diagnoseTarget?.let { rc -> FleetDiagnoseDialog(client, rc) { diagnoseTarget = null } }
}

/** The fleet screen's tabs, in display order (same contract as ConfigTab). */
private enum class FleetTab(val label: String) {
    NODES("노드"),
    RECIPES("레시피"),
    MODELS("모델"),
    BENCH("벤치"),
    JOBS("작업"),
}
