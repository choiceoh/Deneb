package ai.deneb.deneb

import ai.deneb.deneb.generated.EngineDay
import ai.deneb.deneb.generated.EngineRoutingRow
import ai.deneb.deneb.generated.EngineStatusResult
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import androidx.compose.foundation.background
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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
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
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import kotlin.math.roundToInt

/**
 * 엔진 — the local serving engine, as the engine and the router describe
 * themselves (`miniapp.engine.status`).
 *
 * Leads with the two facts that are actually decided elsewhere and invisible
 * here: whether the engine is up right now, and **how much of the traffic it
 * actually took**. The second is not a detail. The local engine and its cloud
 * twin answer under the same model name, so a speed number alone reads as if
 * every turn ran locally — between 2026-09-06 and 09-13 the router substituted
 * 4,949 times and nothing in the replies said so.
 *
 * Design split (docs/agent-rules/native-design-system.md): pull-to-refresh is
 * Material, the frame and type are the Deneb skin (DenebScreenScaffold +
 * DenebType + grouped DenebGroup cards). [EngineStatusContent] is the stateless
 * body; this composable is the stateful shell (fetch + loading/error states).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DenebEngineScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var status by remember { mutableStateOf<EngineStatusResult?>(null) }
    // null = load in flight, true = ok, false = fetch failed (mirrors DenebUsageScreen).
    var loadOk by remember { mutableStateOf<Boolean?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    val haptics = rememberHaptics()
    val scope = rememberCoroutineScope()

    suspend fun load() {
        val fetched = client.fetchEngineStatus()
        if (fetched == null) {
            loadOk = false
        } else {
            status = fetched
            loadOk = true
        }
    }
    LaunchedEffect(Unit) {
        loadOk = null
        load()
    }

    DenebScreenScaffold(title = "엔진", onBack = onBack, tabBar = navigationTabBar) {
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
                        "엔진 상태를 불러오지 못했습니다.",
                        onRetry = {
                            scope.launch {
                                loadOk = null
                                load()
                            }
                        },
                    )

                    s == null -> DenebEmpty("엔진 상태가 비어 있습니다.")

                    !s.configured -> DenebEmpty("로컬 서빙 엔진이 설정되어 있지 않습니다.")

                    else -> EngineStatusContent(s)
                }
                Spacer(Modifier.height(24.dp))
            }
        }
    }
}

// --- stateless body (previewable) ----------------------------------------

/**
 * The engine page: a live state line, the share of traffic it actually served,
 * the days it measured itself, and the router's per-entry account. Pure
 * presentation — the shell owns the fetch.
 */
@Composable
internal fun EngineStatusContent(status: EngineStatusResult) {
    Column(Modifier.fillMaxWidth().padding(top = 4.dp)) {
        EngineStateLine(status)
        Spacer(Modifier.height(14.dp))
        EngineShareSection(status)
        Spacer(Modifier.height(18.dp))
        EngineSpeedSection(status.days)
        if (status.routing.isNotEmpty()) {
            Spacer(Modifier.height(18.dp))
            EngineRoutingSection(status.routing)
        }
    }
}

/** 가동 중 / 멈춤, with the served model and whatever is in flight right now. */
@Composable
private fun EngineStateLine(status: EngineStatusResult) {
    val live = status.reachable
    val dot = if (live) MaterialTheme.colorScheme.primary else denebHint()
    Row(
        Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.size(8.dp).background(dot, CircleShape))
        Spacer(Modifier.width(8.dp))
        Column(Modifier.weight(1f)) {
            Text(
                text = if (live) "가동 중" else "멈춤",
                style = DenebType.rowTitleStrong,
                color = MaterialTheme.colorScheme.onBackground,
            )
            val detail = when {
                live && status.model.isNotBlank() -> status.model
                live -> status.endpoint
                else -> "엔진에 닿지 않습니다"
            }
            Text(
                text = detail,
                style = DenebType.rowSubtitle,
                color = denebHint(),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        if (live && (status.runningRequests > 0 || status.waitingRequests > 0)) {
            Text(
                text = "실행 ${status.runningRequests} · 대기 ${status.waitingRequests}",
                style = DenebType.meta,
                color = denebHint(),
            )
        }
    }
}

/**
 * How much of the traffic the local engine actually took, from the router's own
 * meter. An unavailable meter says so rather than rendering 0% — in a
 * postmortem "nothing ran locally" and "we could not ask" look identical.
 */
@Composable
private fun EngineShareSection(status: EngineStatusResult) {
    DenebGroup(label = "점유율") {
        if (!status.routerAvailable) {
            Text(
                text = "라우터 계량기에 닿지 못했습니다 — 점유율을 알 수 없습니다.",
                style = DenebType.rowSubtitle,
                color = denebHint(),
                modifier = Modifier.fillMaxWidth().padding(16.dp),
            )
            return@DenebGroup
        }
        val total = status.localRequests + status.remoteRequests
        val pct = if (total > 0) (100.0 * status.localRequests / total) else 0.0
        Column(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 14.dp, bottom = 14.dp)) {
            Text(
                text = "로컬 ${formatPercent(pct)}",
                style = DenebType.rowTitleStrong,
                color = MaterialTheme.colorScheme.onBackground,
            )
            Spacer(Modifier.height(8.dp))
            ShareBar(fraction = if (total > 0) (status.localRequests.toFloat() / total.toFloat()) else 0f)
            Spacer(Modifier.height(8.dp))
            Text(
                text = "로컬 ${status.localRequests} · 그 외 ${status.remoteRequests}" +
                    if (status.routerWindow.isNotBlank()) " · ${status.routerWindow}" else "",
                style = DenebType.meta,
                color = denebHint(),
            )
            Text(
                text = "로컬이 죽으면 라우터가 같은 모델의 클라우드로 조용히 넘깁니다. 응답만 봐서는 구분되지 않습니다.",
                style = DenebType.meta,
                color = denebHint(),
                modifier = Modifier.padding(top = 6.dp),
            )
        }
    }
}

/** The local share as a single proportional bar — the interactive accent for
 *  the local part, a hairline-weight track for the rest. */
@Composable
private fun ShareBar(fraction: Float) {
    val f = fraction.coerceIn(0f, 1f)
    Box(
        Modifier.fillMaxWidth().height(6.dp)
            .background(denebHint().copy(alpha = 0.25f), RoundedCornerShape(3.dp)),
    ) {
        if (f > 0f) {
            Box(
                Modifier.fillMaxWidth(f).height(6.dp)
                    .background(MaterialTheme.colorScheme.primary, RoundedCornerShape(3.dp)),
            )
        }
    }
}

/** One row per measured day. Every number is the engine's own counter — the
 *  gateway's clock cannot see prefill at all (the provider withholds response
 *  headers until the first token, so it reads 0 ms). */
@Composable
private fun EngineSpeedSection(days: List<EngineDay>) {
    DenebGroup(label = "속도") {
        if (days.isEmpty()) {
            Text(
                text = "아직 측정된 날이 없습니다 — 표본은 엔진이 살아 있는 동안에만 쌓입니다.",
                style = DenebType.rowSubtitle,
                color = denebHint(),
                modifier = Modifier.fillMaxWidth().padding(16.dp),
            )
            return@DenebGroup
        }
        days.forEach { EngineDayRow(it) }
        Spacer(Modifier.height(12.dp))
    }
}

@Composable
private fun EngineDayRow(day: EngineDay) {
    Column(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 10.dp, bottom = 2.dp)) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = day.day,
                style = DenebType.rowTitle,
                color = MaterialTheme.colorScheme.onBackground,
                modifier = Modifier.weight(1f),
            )
            Text(
                text = if (day.measured) {
                    "디코드 ${formatRate(day.decodeTokensPerSec)} · 프리필 ${formatRate(day.prefillTokensPerSec)}"
                } else {
                    "측정 없음"
                },
                style = DenebType.meta,
                color = denebHint(),
            )
        }
        if (day.measured) {
            Text(
                text = "동시성 ${formatConcurrency(day.concurrencyWhileBusy)} · 최대 ${day.peakConcurrency} · 요청 ${day.requests}",
                style = DenebType.meta,
                color = denebHint(),
                modifier = Modifier.padding(top = 2.dp),
            )
        }
        if (day.restarts > 0) {
            Text(
                text = "재시작 ${day.restarts}회 — 그 구간은 위 수치에 없습니다",
                style = DenebType.meta,
                color = denebHint(),
                modifier = Modifier.padding(top = 2.dp),
            )
        }
    }
}

/** The router's per-entry account: who answered, and how much. */
@Composable
private fun EngineRoutingSection(rows: List<EngineRoutingRow>) {
    DenebGroup(label = "실제로 답한 곳") {
        rows.forEach { row ->
            Row(
                Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 10.dp, bottom = 2.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(Modifier.weight(1f)) {
                    Text(
                        text = row.model,
                        style = if (row.local) DenebType.rowTitleStrong else DenebType.rowTitle,
                        color = MaterialTheme.colorScheme.onBackground,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Text(
                        text = if (row.local) "로컬 엔진" else "원격",
                        style = DenebType.meta,
                        color = if (row.local) MaterialTheme.colorScheme.primary else denebHint(),
                    )
                }
                Text(
                    text = "${row.requests}회 · 입력 ${formatTokenCount(row.inputTokens)}",
                    style = DenebType.meta,
                    color = denebHint(),
                )
            }
        }
        Spacer(Modifier.height(12.dp))
    }
}

// --- formatting ----------------------------------------------------------

/** One decimal below 10%, none above — a share is read, not audited. */
internal fun formatPercent(pct: Double): String = if (pct >= 10.0) "${pct.roundToInt()}%" else "${(pct * 10).roundToInt() / 10.0}%"

/** Token rates round to whole tokens: the sampling interval is coarser than a
 *  decimal place would suggest. */
internal fun formatRate(perSec: Double): String = if (perSec <= 0.0) "—" else "${perSec.roundToInt()} tok/s"

internal fun formatConcurrency(c: Double): String = if (c <= 0.0) "—" else "${(c * 10).roundToInt() / 10.0}"
