package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import kotlinx.datetime.DateTimeUnit
import kotlinx.datetime.TimeZone
import kotlinx.datetime.plus
import kotlinx.datetime.todayIn
import kotlinx.datetime.toLocalDateTime
import kotlin.time.Clock
import kotlin.time.Instant

/**
 * Upcoming-calendar view (`miniapp.calendar.list_upcoming`). Pull to refresh;
 * events group by local day; tapping one opens the event detail. Surface-wrapped
 * so unstyled text inherits the right content color in dark mode.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DenebCalendarScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onOpenEvent: (String) -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val events by client.denebCalendar.collectAsState()
    val scope = rememberCoroutineScope()
    var refreshing by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { client.refreshCalendar() }

    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = { scope.launch { refreshing = true; client.refreshCalendar(); refreshing = false } },
            modifier = Modifier.fillMaxSize(),
        ) {
            Column(
                modifier = Modifier.statusBarsPadding().padding(16.dp).verticalScroll(rememberScrollState()),
            ) {
                if (navigationTabBar != null) {
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) { navigationTabBar() }
                    Spacer(Modifier.height(16.dp))
                }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        "일정",
                        style = MaterialTheme.typography.headlineMedium,
                        fontWeight = FontWeight.SemiBold,
                        modifier = Modifier.weight(1f),
                    )
                    TextButton(onClick = onBack) { Text("닫기") }
                }
                Spacer(Modifier.height(8.dp))

                if (events.isEmpty()) {
                    Text(
                        "불러오는 중…",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                } else {
                    var lastDayKey: String? = null
                    events.sortedBy { it.start }.forEach { event ->
                        val stamp = stampOf(event.start, event.allDay)
                        if (stamp?.dayKey != lastDayKey) {
                            Spacer(Modifier.height(12.dp))
                            Text(
                                stamp?.dayLabel ?: "날짜 미정",
                                style = MaterialTheme.typography.titleSmall,
                                color = MaterialTheme.colorScheme.primary,
                            )
                            Spacer(Modifier.height(4.dp))
                            HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                            lastDayKey = stamp?.dayKey
                        }
                        Row(
                            modifier = Modifier.fillMaxWidth().clickable { onOpenEvent(event.id) }.padding(vertical = 12.dp),
                            verticalAlignment = Alignment.Top,
                        ) {
                            Text(
                                stamp?.time ?: "—",
                                style = MaterialTheme.typography.bodyMedium,
                                fontWeight = FontWeight.SemiBold,
                                color = MaterialTheme.colorScheme.onSurface,
                                modifier = Modifier.width(56.dp),
                            )
                            Column(Modifier.weight(1f)) {
                                Text(
                                    event.title.ifBlank { "(제목 없음)" },
                                    style = MaterialTheme.typography.bodyLarge,
                                    color = MaterialTheme.colorScheme.onSurface,
                                    maxLines = 2,
                                )
                                val sub = buildList {
                                    if (event.location.isNotBlank()) add(event.location)
                                    if (event.hasMeet) add("📹 Meet")
                                }.joinToString("  ·  ")
                                if (sub.isNotEmpty()) {
                                    Text(
                                        sub,
                                        style = MaterialTheme.typography.bodySmall,
                                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                                        maxLines = 1,
                                    )
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

internal val koreanDayOfWeek = listOf("월", "화", "수", "목", "금", "토", "일")

internal data class CalStamp(val dayKey: String, val dayLabel: String, val time: String)

/** Parse an RFC3339 UTC instant into a local day-grouping key + label + HH:mm (or 종일). */
internal fun stampOf(rfc3339: String, allDay: Boolean): CalStamp? {
    if (rfc3339.isBlank()) return null
    val tz = TimeZone.currentSystemDefault()
    val local = runCatching {
        Instant.parse(rfc3339).toLocalDateTime(tz)
    }.getOrNull() ?: return null
    val month = local.month.ordinal + 1
    val dow = koreanDayOfWeek.getOrElse(local.dayOfWeek.ordinal) { "" }
    val today = Clock.System.todayIn(tz)
    val dayLabel = when (local.date) {
        today -> "오늘"
        today.plus(1, DateTimeUnit.DAY) -> "내일"
        else -> "${month}월 ${local.day}일 ($dow)"
    }
    return CalStamp(
        dayKey = "${local.year}-$month-${local.day}",
        dayLabel = dayLabel,
        time = if (allDay) {
            "종일"
        } else {
            "${local.hour.toString().padStart(2, '0')}:${local.minute.toString().padStart(2, '0')}"
        },
    )
}
