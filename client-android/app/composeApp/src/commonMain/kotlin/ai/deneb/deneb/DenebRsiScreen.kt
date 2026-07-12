package ai.deneb.deneb

import ai.deneb.deneb.generated.RSILayerView
import ai.deneb.deneb.generated.RSILoopStatusResponse
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import ai.deneb.ui.denebInsightContainer
import ai.deneb.ui.denebOnSuccessContainer
import ai.deneb.ui.denebOnWarningContainer
import ai.deneb.ui.denebSuccessContainer
import ai.deneb.ui.denebWarningContainer
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/**
 * 재귀적 자가개선 — the recursive-self-improvement loop status
 * (`miniapp.rsi.status`). The four loop layers (L1 스킬 진화, L2 메타 진화,
 * L3 판정자 공진화, L4 소스 자가편집) each shown with an honest state badge
 * (LIVE/DATA-GATED/STARVED/FROZEN/IDLE), a one-line diagnosis, and key metrics —
 * the window onto how far the agent's self-improvement is actually turning.
 *
 * The DATA-GATED vs STARVED distinction is deliberate: DATA-GATED is a young
 * loop waiting for data (normal), STARVED is an input gap (actionable). The
 * badge colors keep them apart at a glance.
 *
 * Design split (docs/agent-rules/native-design-system.md): frame + type are the
 * Deneb skin (DenebScreenScaffold + DenebType + grouped DenebGroup cards); the
 * pull-to-refresh is Material. The layer list is a stateless body
 * ([RsiStatusContent]) the render harness previews with mock data; this
 * composable is the stateful shell (fetch + loading/error/empty states).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DenebRsiScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var status by remember { mutableStateOf<RSILoopStatusResponse?>(null) }
    // null = load in flight, true = ok, false = fetch failed (mirrors DenebDashboardScreen).
    var loadOk by remember { mutableStateOf<Boolean?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    val haptics = rememberHaptics()
    val scope = rememberCoroutineScope()

    suspend fun load() {
        val fetched: RSILoopStatusResponse? = client.fetchRsiStatus()
        if (fetched == null) {
            loadOk = false
        } else {
            status = fetched
            loadOk = true
        }
    }
    LaunchedEffect(Unit) { load() }

    DenebScreenScaffold(title = "재귀적 자가개선", onBack = onBack, tabBar = navigationTabBar) {
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = {
                haptics.refresh()
                scope.launch {
                    refreshing = true
                    load()
                    refreshing = false
                }
            },
            modifier = Modifier.fillMaxWidth().weight(1f),
        ) {
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                val s = status
                when {
                    loadOk == null && s == null -> DenebLoading()

                    loadOk == false && s == null -> DenebError(
                        "자가개선 상태를 불러오지 못했습니다.",
                        onRetry = {
                            scope.launch {
                                loadOk = null
                                load()
                            }
                        },
                    )

                    s == null || s.layers.isEmpty() -> DenebEmpty("표시할 자가개선 상태가 없습니다.")

                    else -> RsiStatusContent(s)
                }
                Spacer(Modifier.height(24.dp))
            }
        }
    }
}

// --- stateless body (previewable) ----------------------------------------

/**
 * The four RSI loop layers: a summary line ("N/4개 루프 가동 중") then one grouped
 * card per layer — header (key · title + state badge), diagnosis line, and a
 * wrapped row of value/label metrics. Pure presentation — the shell owns
 * fetch + state.
 */
@Composable
internal fun RsiStatusContent(status: RSILoopStatusResponse) {
    Column(Modifier.fillMaxWidth().padding(top = 4.dp)) {
        Text(
            text = "${status.turning}/${status.layers.size}개 루프 가동 중",
            style = DenebType.sectionLabel,
            color = denebHint(),
            modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 12.dp),
        )
        status.layers.forEach { layer ->
            RsiLayerCard(layer)
            Spacer(Modifier.height(18.dp))
        }
    }
}

/** One loop layer: header (key · title + state badge), diagnosis, metric chips. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun RsiLayerCard(layer: RSILayerView) {
    DenebGroup {
        Row(
            Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 14.dp, bottom = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = "${layer.key} · ${layer.title}",
                style = DenebType.cardTitle,
                color = MaterialTheme.colorScheme.onBackground,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            RsiStateBadge(layer.state)
        }
        if (layer.diagnosis.isNotBlank()) {
            Text(
                text = layer.diagnosis,
                style = DenebType.rowSubtitle,
                color = denebHint(),
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 16.dp, end = 16.dp, top = 2.dp, bottom = if (layer.metrics.isEmpty()) 16.dp else 10.dp),
            )
        }
        if (layer.metrics.isNotEmpty()) {
            FlowRow(
                Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 2.dp, bottom = 16.dp),
                horizontalArrangement = Arrangement.spacedBy(20.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                layer.metrics.forEach { m ->
                    Column {
                        Text(
                            text = m.value.ifBlank { "—" },
                            style = DenebType.rowTitleStrong,
                            color = MaterialTheme.colorScheme.onBackground,
                        )
                        Text(text = m.label, style = DenebType.meta, color = denebHint())
                    }
                }
            }
        }
    }
}

/** A colored pill for the layer state — the LIVE/DATA-GATED/STARVED/FROZEN/IDLE
 *  taxonomy, colored so "turning" (LIVE), "waiting" (DATA-GATED), "needs wiring"
 *  (STARVED), "self-braked" (FROZEN), and "dormant" (IDLE) separate at a glance. */
@Composable
private fun RsiStateBadge(state: String) {
    val bg: Color
    val fg: Color
    when (state) {
        "LIVE" -> {
            bg = denebSuccessContainer()
            fg = denebOnSuccessContainer()
        }

        "DATA-GATED" -> {
            bg = denebWarningContainer()
            fg = denebOnWarningContainer()
        }

        "STARVED" -> {
            bg = MaterialTheme.colorScheme.errorContainer
            fg = MaterialTheme.colorScheme.onErrorContainer
        }

        "FROZEN" -> {
            bg = denebInsightContainer()
            fg = denebInsight()
        }

        else -> { // IDLE / unknown
            bg = MaterialTheme.colorScheme.surfaceVariant
            fg = denebHint()
        }
    }
    Text(
        text = state,
        style = DenebType.meta,
        color = fg,
        modifier = Modifier
            .padding(start = 8.dp)
            .background(bg, RoundedCornerShape(6.dp))
            .padding(horizontal = 8.dp, vertical = 3.dp),
    )
}
