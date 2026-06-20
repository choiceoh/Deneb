package ai.deneb.deneb

import ai.deneb.deneb.generated.DashboardItem
import ai.deneb.deneb.generated.DashboardOut
import ai.deneb.deneb.generated.LaneOut
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebRow
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
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
import kotlinx.datetime.DateTimeUnit
import kotlinx.datetime.TimeZone
import kotlinx.datetime.plus
import kotlinx.datetime.toLocalDateTime
import kotlinx.datetime.todayIn
import kotlin.time.Clock
import kotlin.time.Instant

/**
 * 파트별 업무 현황 — the work dashboard (`miniapp.dashboard.lanes`). Every live work
 * item (calendar events + work-feed cards) grouped into 파트(part) lanes so the
 * operator sees who is doing what across the org at a glance, rather than scanning
 * the flat feed. The five fixed 파트 lanes always show (an empty lane signals "this
 * part has nothing pending right now"); '미분류' appears last only when it has items
 * and is rendered muted as a triage bucket. Pull to refresh re-fetches.
 *
 * Design split (see .claude/rules/native-design-system.md): frame + type are the
 * Deneb skin (DenebScreenScaffold + DenebType + grouped DenebGroup cards); the
 * pull-to-refresh is Material. The lane list is a stateless body
 * ([DashboardLanesContent]) the render harness previews with mock data; this
 * composable is the stateful shell (fetch + loading/error/empty states).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DenebDashboardScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var lanes by remember { mutableStateOf<List<LaneOut>>(emptyList()) }
    // null = load in flight, true = ok, false = fetch failed (mirrors DenebTodoScreen).
    var loadOk by remember { mutableStateOf<Boolean?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    suspend fun load() {
        val fetched: DashboardOut? = client.fetchDashboardLanes()
        if (fetched == null) {
            loadOk = false
        } else {
            lanes = fetched.lanes
            loadOk = true
        }
    }
    LaunchedEffect(Unit) { load() }

    DenebScreenScaffold(title = "파트별 업무 현황", onBack = onBack, tabBar = navigationTabBar) {
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = {
                scope.launch {
                    refreshing = true
                    load()
                    refreshing = false
                }
            },
            modifier = Modifier.fillMaxWidth().weight(1f),
        ) {
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                when {
                    loadOk == null && lanes.isEmpty() -> DenebLoading()

                    loadOk == false && lanes.isEmpty() -> DenebError(
                        "업무 현황을 불러오지 못했습니다.",
                        onRetry = {
                            scope.launch {
                                loadOk = null
                                load()
                            }
                        },
                    )

                    // The gateway always returns the five fixed lanes, so an empty
                    // list means a degenerate/old response — guide rather than blank.
                    lanes.isEmpty() -> DenebEmpty("표시할 업무 현황이 없습니다.")

                    else -> DashboardLanesContent(lanes)
                }
                Spacer(Modifier.height(24.dp))
            }
        }
    }
}

// --- stateless body (previewable) ----------------------------------------

/**
 * The dashboard lanes: one grouped card per 파트, each headed by the part name and
 * its item count. Items list as title / subtitle / scheduled-time rows; an empty
 * lane shows a quiet "지금 할 일이 없습니다" line so the part still reads as present.
 * The '미분류' triage lane is rendered muted. Pure presentation — the shell owns
 * fetch + state.
 */
@Composable
internal fun DashboardLanesContent(lanes: List<LaneOut>) {
    val tz = remember { TimeZone.currentSystemDefault() }
    Column(Modifier.fillMaxWidth().padding(top = 4.dp)) {
        lanes.forEach { lane ->
            val muted = lane.key == "unclassified"
            DashboardLane(lane = lane, tz = tz, muted = muted)
            Spacer(Modifier.height(18.dp))
        }
    }
}

/** One 파트 lane: a [DenebGroup] (header + rows). When [muted] (the 미분류 triage
 *  bucket) the whole lane relaxes to hint color so the named parts read first. */
@Composable
private fun DashboardLane(lane: LaneOut, tz: TimeZone, muted: Boolean) {
    val titleColor = if (muted) denebHint() else MaterialTheme.colorScheme.onBackground
    DenebGroup {
        // Lane header: part name + count. The count is hint-colored so the name leads.
        Row(
            Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 14.dp, bottom = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = lane.name.ifBlank { "(이름 없음)" },
                style = DenebType.cardTitle,
                color = titleColor,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            if (lane.items.isNotEmpty()) {
                Text(
                    text = "${lane.items.size}건",
                    style = DenebType.meta,
                    color = denebHint(),
                    modifier = Modifier.padding(start = 8.dp),
                )
            }
        }
        if (lane.items.isEmpty()) {
            Text(
                text = "지금 할 일이 없습니다.",
                style = DenebType.rowSubtitle,
                color = denebHint(),
                modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 2.dp, bottom = 16.dp),
            )
        } else {
            lane.items.forEach { item ->
                DashboardItemRow(item = item, tz = tz, muted = muted)
            }
        }
    }
}

/** A single work item inside a lane: title / subtitle / scheduled-time. Uses a bare
 *  [DenebRow] (single hairline) so the rows read as a content list inside the card —
 *  the last row's hairline sits fine against the group's rounded edge (matches the
 *  feed list). Non-interactive for now (the dashboard is a read-only glance). */
@Composable
private fun DashboardItemRow(item: DashboardItem, tz: TimeZone, muted: Boolean) {
    val haptics = rememberHaptics()
    val titleColor = if (muted) denebHint() else MaterialTheme.colorScheme.onBackground
    DenebRow(
        // Whole-row haptic tap kept for parity with other lists, but no navigation
        // yet — the gateway hands back refType/refId for a future deep-link.
        onClick = { haptics.tap() },
        modifier = Modifier.padding(horizontal = 16.dp),
    ) {
        Row(verticalAlignment = Alignment.Top) {
            Column(Modifier.weight(1f)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = item.title.ifBlank { "(제목 없음)" },
                        style = DenebType.rowTitle,
                        color = titleColor,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                    val stamp = dashboardTimeLabel(item.whenMs, tz)
                    if (stamp.isNotEmpty()) {
                        Text(
                            text = stamp,
                            style = DenebType.meta,
                            color = denebHint(),
                            modifier = Modifier.padding(start = 8.dp),
                        )
                    }
                }
                if (item.subtitle.isNotBlank()) {
                    Text(
                        text = item.subtitle,
                        style = DenebType.snippet,
                        color = denebHint(),
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.fillMaxWidth().padding(top = 4.dp),
                    )
                }
            }
        }
    }
}

/**
 * Scheduled-time label for a dashboard item ("오늘 14:00" / "내일 09:30" /
 * "6월 20일 (금) 14:00"). Blank for a missing/zero timestamp so the row omits the
 * stamp. Unlike the feed's relative "N분 전" (items are things that *happened*), the
 * dashboard mixes upcoming calendar events with cards, so an absolute clock reads
 * better at a glance.
 */
internal fun dashboardTimeLabel(epochMs: Long, tz: TimeZone): String {
    if (epochMs <= 0L) return ""
    val local = runCatching { Instant.fromEpochMilliseconds(epochMs).toLocalDateTime(tz) }.getOrNull() ?: return ""
    val today = Clock.System.todayIn(tz)
    val hh = local.hour.toString().padStart(2, '0')
    val mm = local.minute.toString().padStart(2, '0')
    val clock = "$hh:$mm"
    return when (local.date) {
        today -> "오늘 $clock"

        today.plus(1, DateTimeUnit.DAY) -> "내일 $clock"

        today.plus(-1, DateTimeUnit.DAY) -> "어제 $clock"

        else -> {
            val dow = koreanDayOfWeek.getOrElse(local.dayOfWeek.ordinal) { "" }
            "${local.month.ordinal + 1}월 ${local.day}일 ($dow) $clock"
        }
    }
}
