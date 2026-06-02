package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.DenebScreenScaffold
import com.inspiredandroid.kai.ui.DenebSectionLabel
import com.inspiredandroid.kai.ui.DenebType
import com.inspiredandroid.kai.ui.components.rememberHaptics
import com.inspiredandroid.kai.ui.denebHint
import kotlinx.coroutines.launch

/**
 * Calendar event detail (`miniapp.calendar.get`): when, location, a Meet join
 * button, organizer, attendees and description.
 *
 * Design split (see .claude/rules/native-design-system.md): the frame + type are
 * the Deneb typographic skin (DenebScreenScaffold + DenebType + DenebSectionLabel),
 * while the Meet join stays a Material button. The loaded-event presentation lives
 * in [CalendarEventContent] — a stateless body the render harness previews with
 * mock data; this composable is the stateful shell (load + loading/error states).
 */
@Composable
fun DenebCalendarEventScreen(
    client: DenebGatewayClient,
    eventId: String,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var event by remember(eventId) { mutableStateOf<CalendarEventDetail?>(null) }
    var loadFailed by remember(eventId) { mutableStateOf(false) }
    val uriHandler = LocalUriHandler.current
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()

    suspend fun load() {
        loadFailed = false
        event = null
        val e = client.fetchCalendarEvent(eventId)
        event = e
        loadFailed = e == null
    }
    LaunchedEffect(eventId) { load() }

    DenebScreenScaffold(title = "일정", onBack = onBack, tabBar = navigationTabBar) {
        Column(
            Modifier
                .fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp),
        ) {
            val ev = event
            when {
                ev == null && loadFailed ->
                    DenebError("일정을 불러오지 못했습니다.", onRetry = { scope.launch { load() } })
                ev == null -> DenebLoading()
                else -> CalendarEventContent(
                    ev = ev,
                    onJoinMeet = { haptics.tap(); uriHandler.openUri(ev.meetUri) },
                )
            }
        }
    }
}

/**
 * Stateless presentation of a loaded event — extracted so [RenderPreview] can render
 * it with mock data. Pure Deneb type skin; the Meet join is a Material button.
 */
@Composable
internal fun CalendarEventContent(ev: CalendarEventDetail, onJoinMeet: () -> Unit) {
    Spacer(Modifier.height(8.dp))
    Text(
        ev.title.ifBlank { "(제목 없음)" },
        style = DenebType.subject,
        color = MaterialTheme.colorScheme.onBackground,
    )
    Spacer(Modifier.height(10.dp))
    Text(whenLabel(ev), style = DenebType.body, color = MaterialTheme.colorScheme.onBackground)
    statusLabel(ev.status)?.let {
        Spacer(Modifier.height(4.dp))
        Text(it, style = DenebType.meta, color = MaterialTheme.colorScheme.error)
    }

    if (ev.location.isNotBlank()) {
        Spacer(Modifier.height(8.dp))
        InfoRow("장소", ev.location)
    }
    if (ev.organizer.isNotBlank()) {
        Spacer(Modifier.height(4.dp))
        InfoRow("주최", ev.organizer)
    }

    if (ev.meetUri.isNotBlank()) {
        Spacer(Modifier.height(16.dp))
        FilledTonalButton(onClick = onJoinMeet) { Text("📹 Meet 참가") }
    }

    if (ev.attendees.isNotEmpty()) {
        DenebSectionLabel("참석자 ${ev.attendees.size}")
        ev.attendees.forEach { name ->
            Text("· $name", style = DenebType.body, color = MaterialTheme.colorScheme.onBackground)
        }
    }

    if (ev.description.isNotBlank()) {
        DenebSectionLabel("설명")
        Text(ev.description, style = DenebType.body, color = MaterialTheme.colorScheme.onBackground)
    }
    Spacer(Modifier.height(24.dp))
}

@Composable
private fun InfoRow(label: String, value: String) {
    Row(verticalAlignment = Alignment.Top) {
        Text(
            label,
            style = DenebType.hint,
            color = denebHint(),
            modifier = Modifier.width(56.dp),
        )
        Text(
            value,
            style = DenebType.body,
            color = MaterialTheme.colorScheme.onBackground,
            modifier = Modifier.weight(1f),
        )
    }
}

/** Event status -> a Korean label, or null when confirmed (no banner needed). */
private fun statusLabel(status: String): String? = when (status) {
    "tentative" -> "미확정"
    "cancelled" -> "취소됨"
    else -> null
}

/** "5월 31일 (토) 14:00 – 15:00" or "5월 31일 (토) · 종일". */
private fun whenLabel(ev: CalendarEventDetail): String {
    val start = stampOf(ev.start, ev.allDay)
    val end = stampOf(ev.end, ev.allDay)
    val day = start?.dayLabel ?: "날짜 미정"
    return when {
        ev.allDay -> "$day · 종일"
        end != null -> "$day  ${start?.time ?: ""} – ${end.time}"
        else -> "$day  ${start?.time ?: ""}"
    }
}
