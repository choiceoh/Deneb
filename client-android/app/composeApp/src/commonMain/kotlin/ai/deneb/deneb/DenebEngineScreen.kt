package ai.deneb.deneb

import ai.deneb.deneb.generated.EngineOutage
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.datetime.TimeZone

/**
 * 엔진 — the local serving engine, as the gateway routes to it
 * (`miniapp.engine.status`).
 *
 * Leads with the gateway's own routing verdict, not a probe made for this
 * screen: the liveness watcher decides where a turn goes, and on 2026-09-16 it
 * moved turns to the cloud 22 times for eight hours in total while a scrape
 * taken at the moment of opening showed a green dot. Then how much of the last
 * day the engine was gone, how much of today's traffic it actually took, and
 * only then how fast it is.
 *
 * Design split (docs/agent-rules/native-design-system.md): pull-to-refresh is
 * Material, the frame and type are the Deneb skin (DenebScreenScaffold +
 * DenebType + grouped DenebGroup cards). [EngineStatusContent] is the stateless
 * body; this composable is the stateful shell (fetch + loading/error states),
 * which also refetches on every engine readiness push.
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
    // Whose statistics to show; null follows the model the engine serves.
    var selectedModel by remember { mutableStateOf<String?>(null) }
    val haptics = rememberHaptics()
    val scope = rememberCoroutineScope()
    val loadMutex = remember { Mutex() }
    val lifecycle = LocalLifecycleOwner.current.lifecycle

    suspend fun load() {
        loadMutex.withLock {
            val asked = selectedModel
            val fetched = client.fetchEngineStatus(asked)
            if (fetched == null) {
                loadOk = false
            } else {
                // The window no longer holds the model asked for (it aged out):
                // follow the current model again instead of asking for it forever.
                if (asked != null && fetched.selectedModel != asked && selectedModel == asked) selectedModel = null
                status = fetched
                loadOk = true
            }
        }
    }
    // The gateway pushes every readiness change; the screen follows it so the
    // state line flips the moment routing does, not on the next pull.
    LaunchedEffect(client, lifecycle) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            coroutineScope {
                launch {
                    while (isActive) {
                        load()
                        delay(15_000)
                    }
                }
                launch { client.engineEvents.collect { load() } }
            }
        }
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

                    else -> {
                        if (loadOk == false) {
                            Text(
                                "갱신 실패 · 이전에 받은 통계입니다.",
                                style = DenebType.meta,
                                color = MaterialTheme.colorScheme.error,
                                modifier = Modifier.padding(16.dp),
                            )
                        }
                        EngineStatusContent(
                            s,
                            onSelectModel = { model ->
                                // Back to the current model means "follow it": a later
                                // switch then moves the page with the engine.
                                selectedModel = model.takeUnless { m -> s.models.firstOrNull { it.current }?.model == m }
                                scope.launch { load() }
                            },
                        )
                    }
                }
                Spacer(Modifier.height(24.dp))
            }
        }
    }
}

// --- stateless body (previewable) ----------------------------------------

/**
 * The engine page: the routing verdict, the last 24 hours, today's share, the
 * window's totals, the measured days (each expandable to its full detail), the
 * engine's internals and the router's per-entry account. Pure presentation —
 * the shell owns the fetch. [zone] renders wall-clock times; the preview pins
 * it so the golden is the same on every machine.
 */
@Composable
internal fun EngineStatusContent(
    status: EngineStatusResult,
    zone: TimeZone = TimeZone.currentSystemDefault(),
    onSelectModel: (String) -> Unit = {},
) {
    Column(Modifier.fillMaxWidth().padding(top = 4.dp)) {
        EngineStateLine(status)
        EngineModelTabs(status, onSelectModel)
        Spacer(Modifier.height(14.dp))
        EngineDiagnosticsSection(status, zone)
        Spacer(Modifier.height(14.dp))
        EngineAvailabilitySection(status, zone)
        Spacer(Modifier.height(18.dp))
        EngineShareSection(status)
        if (status.total.requests > 0 || status.total.outages > 0) {
            Spacer(Modifier.height(18.dp))
            EngineSummarySection(status.total)
        }
        Spacer(Modifier.height(18.dp))
        EngineDaysSection(status.days)
        // The live process's gauges describe the model it serves, not a past one.
        if (status.reachable && status.viewingCurrentModel() && (status.internals.published || status.internals.fleetKnown)) {
            Spacer(Modifier.height(18.dp))
            EngineInternalsSection(status)
        }
        if (status.routingToday.isNotEmpty()) {
            Spacer(Modifier.height(18.dp))
            EngineRoutingSection("오늘 실제로 답한 곳", status.routingToday)
        }
        if (status.routing.isNotEmpty()) {
            Spacer(Modifier.height(18.dp))
            EngineRoutingSection("이번 달 실제로 답한 곳" + (if (status.routerWindow.isNotBlank()) " · ${status.routerWindow}" else ""), status.routing)
        }
    }
}

/**
 * The verdict line. While the watcher runs, its decision is the state: a turn
 * for the engine's models goes to the fallback chain the whole time it says
 * down, whatever a scrape says. Without a watcher the live probe is all there
 * is, and the line says so.
 */
@Composable
private fun EngineStateLine(status: EngineStatusResult) {
    val tracked = status.livenessTracked
    val live = if (tracked) !status.engineDown else status.reachable
    val dot = if (live) MaterialTheme.colorScheme.primary else denebHint()
    val title = when {
        tracked && status.engineDown -> "클라우드로 우회 중"
        tracked -> "가동 중"
        status.reachable -> "가동 중 (프로브)"
        else -> "멈춤"
    }
    val since = when {
        tracked && status.engineDown && status.downSinceMs > 0L -> formatSinceFor(status.downSinceMs, status.nowMs)
        tracked && !status.engineDown && status.upSinceMs > 0L -> "복구 ${formatAgo(status.upSinceMs, status.nowMs)}"
        else -> ""
    }
    Column(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 4.dp)) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.size(8.dp).background(dot, CircleShape))
            Spacer(Modifier.width(8.dp))
            Column(Modifier.weight(1f)) {
                Text(
                    text = if (since.isNotBlank()) "$title · $since" else title,
                    style = DenebType.rowTitleStrong,
                    color = MaterialTheme.colorScheme.onBackground,
                )
                val detail = when {
                    tracked && status.engineDown -> downReasonLabel(status.downReason).ifBlank { "엔진이 요청을 거부합니다" }
                    status.model.isNotBlank() -> status.model
                    live -> status.endpoint
                    else -> "엔진에 닿지 않습니다"
                }
                Text(
                    text = detail,
                    style = DenebType.rowSubtitle,
                    color = denebHint(),
                    maxLines = 2,
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
        if (tracked && status.engineDown && status.downModels.isNotEmpty()) {
            Text(
                text = "폴백으로 가는 모델: ${status.downModels.joinToString(", ")}",
                style = DenebType.meta,
                color = denebHint(),
                modifier = Modifier.padding(start = 16.dp, top = 6.dp),
            )
        }
        if (!tracked) {
            Text(
                text = "게이트웨이의 엔진 감시가 꺼져 있어 지금 이 순간의 프로브만 보입니다.",
                style = DenebType.meta,
                color = denebHint(),
                modifier = Modifier.padding(start = 16.dp, top = 6.dp),
            )
        }
    }
}

/**
 * The last 24 hours as one strip — up in the accent, down in the hint, and
 * the time before tracking began left blank — with the outages listed under
 * it. This is the card the 2026-09-16 flapping was invisible without.
 */
@Composable
private fun EngineAvailabilitySection(status: EngineStatusResult, zone: TimeZone) {
    DenebGroup(label = "최근 24시간") {
        Column(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 14.dp, bottom = 14.dp)) {
            if (!status.livenessTracked) {
                Text(
                    text = "엔진 감시가 꺼져 있어 가용성을 알 수 없습니다.",
                    style = DenebType.rowSubtitle,
                    color = denebHint(),
                )
                return@DenebGroup
            }
            val segments = availabilitySegments(status.outages, status.trackedSinceMs, status.nowMs)
            val recent = outagesWithin(status.outages, status.nowMs)
            val downSec = downSecondsIn(segments)
            Text(
                text = if (recent.isEmpty()) "끊김 없음" else "끊김 ${recent.size}회 · 총 ${formatDuration(downSec)}",
                style = DenebType.rowTitleStrong,
                color = MaterialTheme.colorScheme.onBackground,
            )
            Spacer(Modifier.height(8.dp))
            AvailabilityStrip(segments)
            Row(Modifier.fillMaxWidth().padding(top = 4.dp)) {
                Text(text = formatClockOrDay(status.nowMs - AVAILABILITY_WINDOW_MS, status.nowMs, zone), style = DenebType.meta, color = denebHint(), modifier = Modifier.weight(1f))
                Text(text = "지금 ${formatClock(status.nowMs, zone)}", style = DenebType.meta, color = denebHint())
            }
            if (segments.any { it.state == AvailabilityState.UNKNOWN }) {
                Text(
                    text = "빈 구간은 감시 기록이 없는 시간입니다.",
                    style = DenebType.meta,
                    color = denebHint(),
                    modifier = Modifier.padding(top = 6.dp),
                )
            }
            recent.take(MAX_OUTAGE_ROWS).forEach { OutageRow(it, status.nowMs, zone) }
            if (recent.size > MAX_OUTAGE_ROWS) {
                Text(
                    text = "외 ${recent.size - MAX_OUTAGE_ROWS}건",
                    style = DenebType.meta,
                    color = denebHint(),
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
        }
    }
}

private const val MAX_OUTAGE_ROWS = 8

@Composable
private fun AvailabilityStrip(segments: List<AvailabilitySegment>) {
    val up = MaterialTheme.colorScheme.primary
    val down = denebHint()
    val unknown = denebHint().copy(alpha = 0.12f)
    Row(Modifier.fillMaxWidth().height(10.dp).clip(RoundedCornerShape(3.dp))) {
        if (segments.isEmpty()) {
            Box(Modifier.weight(1f).fillMaxSize().background(unknown))
            return@Row
        }
        segments.forEach { seg ->
            // Row weights must be positive; a sub-pixel segment still gets a sliver.
            val w = (seg.end - seg.start).coerceAtLeast(0.0005f)
            val color = when (seg.state) {
                AvailabilityState.UP -> up
                AvailabilityState.DOWN -> down
                AvailabilityState.UNKNOWN -> unknown
            }
            Box(Modifier.weight(w).fillMaxSize().background(color))
        }
    }
}

@Composable
private fun OutageRow(outage: EngineOutage, nowMs: Long, zone: TimeZone) {
    val ongoing = outage.untilMs <= 0L
    val span = if (ongoing) {
        "${formatClockOrDay(outage.sinceMs, nowMs, zone)} ~ 진행 중"
    } else {
        "${formatClockOrDay(outage.sinceMs, nowMs, zone)} ~ ${formatClockOrDay(outage.untilMs, nowMs, zone)}"
    }
    val duration = formatDuration(if (ongoing) (nowMs - outage.sinceMs) / 1000.0 else outage.durationSec)
    Row(Modifier.fillMaxWidth().padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(text = span, style = DenebType.rowSubtitle, color = MaterialTheme.colorScheme.onBackground)
            val reason = downReasonLabel(outage.reason)
            if (reason.isNotBlank()) {
                Text(text = reason, style = DenebType.meta, color = denebHint(), maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        Text(text = duration, style = DenebType.meta, color = denebHint())
    }
}

/**
 * How much of the traffic the local engine actually took — today first, from
 * the day meter, then the month. The month meter straddles routing changes
 * (the 2026-09-14 rename left its "local" total mostly cloud traffic), so it
 * is the secondary line, and entries the router config no longer lists are
 * counted apart from remote rather than folded into it. An unavailable meter
 * says so rather than rendering 0%.
 */
@Composable
private fun EngineShareSection(status: EngineStatusResult) {
    DenebGroup(label = "점유율") {
        Column(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 14.dp, bottom = 14.dp)) {
            val todayPct = sharePercent(status.todayLocalRequests, status.todayRemoteRequests + status.todayUnknownRequests)
            if (status.routerDayMetered && todayPct != null) {
                Text(
                    text = "오늘 로컬 ${formatPercent(todayPct)}",
                    style = DenebType.rowTitleStrong,
                    color = MaterialTheme.colorScheme.onBackground,
                )
                Spacer(Modifier.height(8.dp))
                ShareBar(fraction = (todayPct / 100.0).toFloat())
                Spacer(Modifier.height(8.dp))
                Text(
                    text = "로컬 ${status.todayLocalRequests} · 원격 ${status.todayRemoteRequests}" +
                        (if (status.todayUnknownRequests > 0) " · 설정에 없음 ${status.todayUnknownRequests}" else ""),
                    style = DenebType.meta,
                    color = denebHint(),
                )
            } else {
                Text(
                    text = "오늘 로컬 —",
                    style = DenebType.rowTitleStrong,
                    color = MaterialTheme.colorScheme.onBackground,
                )
                Text(
                    text = "오늘은 아직 계량된 요청이 없습니다.",
                    style = DenebType.meta,
                    color = denebHint(),
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
            val monthPct = sharePercent(status.localRequests, status.remoteRequests + status.unknownRequests)
            Text(
                text = when {
                    !status.routerAvailable -> "이번 달: 라우터 계량기에 닿지 못했습니다"

                    monthPct == null -> "이번 달: 계량된 요청 없음"

                    else -> "이번 달 ${status.routerWindow}: 로컬 ${formatPercent(monthPct)} · 로컬 ${status.localRequests} · 원격 ${status.remoteRequests}" +
                        (if (status.unknownRequests > 0) " · 설정에 없음 ${status.unknownRequests}" else "")
                },
                style = DenebType.meta,
                color = denebHint(),
                modifier = Modifier.padding(top = 8.dp),
            )
            Text(
                text = "엔진이 끊기면 게이트웨이가 폴백 모델로 넘기고 답변에 표시합니다. 월 합계는 라우팅 설정이 바뀌면 그 전후가 섞이므로 오늘 수치를 먼저 보세요.",
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
internal fun ShareBar(fraction: Float) {
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
