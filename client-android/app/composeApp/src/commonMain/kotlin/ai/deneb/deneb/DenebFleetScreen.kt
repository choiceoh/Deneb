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
 * title row + pill tab bar + pager, with 노드 / 모델 / 작업 as the tabs. (레시피·벤치
 * surfaces were removed 2026-08-28: recipe control is AI-only via the gateway's
 * `fleet` chat tool; benchmarking left the product.)
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
    var jobs by remember { mutableStateOf<List<FleetJob>?>(null) }
    var loaded by remember { mutableStateOf(false) }
    var stale by remember { mutableStateOf(false) }
    var notice by remember { mutableStateOf<String?>(null) }

    suspend fun refresh() {
        val st = client.fleetState()
        val jb = client.fleetJobs()
        st?.let { state = it }
        jb?.let { jobs = it }
        // Every fetch failing after a successful load means the fleet went away:
        // keep the last data on screen but flag it, instead of letting stale
        // green health pass for live (the retained values would otherwise look
        // current forever).
        stale = loaded && st == null && jb == null
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
                val unreachable = loaded && state?.nodes.isNullOrEmpty() && jobs.isNullOrEmpty()
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
                        FleetTab.MODELS -> FleetModelsPage(client, state?.nodes.orEmpty()) { notice = it }
                        FleetTab.JOBS -> FleetJobsPage(client, jobs.orEmpty(), loaded) { notice = it }
                    }
                }
            }
        }
    }
}

/** The fleet screen's tabs, in display order (same contract as ConfigTab). */
private enum class FleetTab(val label: String) {
    NODES("노드"),
    MODELS("모델"),
    JOBS("작업"),
}
