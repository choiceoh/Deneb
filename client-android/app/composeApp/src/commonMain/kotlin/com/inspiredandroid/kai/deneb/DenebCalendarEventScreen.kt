package com.inspiredandroid.kai.deneb

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
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp

/**
 * Calendar event detail (`miniapp.calendar.get`): when, location, a Meet join
 * button, organizer, attendees and description. Surface-wrapped so all text is
 * visible in dark mode.
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

    LaunchedEffect(eventId) {
        val e = client.fetchCalendarEvent(eventId)
        event = e
        loadFailed = e == null
    }

    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(
            modifier = Modifier
                .statusBarsPadding()
                .padding(16.dp)
                .verticalScroll(rememberScrollState()),
        ) {
            if (navigationTabBar != null) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) { navigationTabBar() }
                Spacer(Modifier.height(12.dp))
            }
            TextButton(onClick = onBack) { Text("← 뒤로") }
            Spacer(Modifier.height(4.dp))

            val ev = event
            if (ev == null) {
                if (loadFailed) DenebError("일정을 불러오지 못했습니다.") else DenebLoading()
            } else {
                Text(
                    ev.title.ifBlank { "(제목 없음)" },
                    style = MaterialTheme.typography.titleLarge,
                    fontWeight = FontWeight.SemiBold,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Spacer(Modifier.height(10.dp))
                Text(
                    whenLabel(ev),
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurface,
                )

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
                    FilledTonalButton(onClick = { uriHandler.openUri(ev.meetUri) }) {
                        Text("📹 Meet 참가")
                    }
                }

                if (ev.attendees.isNotEmpty()) {
                    Spacer(Modifier.height(16.dp))
                    Text(
                        "참석자 ${ev.attendees.size}",
                        style = MaterialTheme.typography.titleSmall,
                        color = MaterialTheme.colorScheme.primary,
                    )
                    Spacer(Modifier.height(4.dp))
                    ev.attendees.forEach { name ->
                        Text(
                            "· $name",
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurface,
                        )
                    }
                }

                if (ev.description.isNotBlank()) {
                    Spacer(Modifier.height(16.dp))
                    HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                    Spacer(Modifier.height(12.dp))
                    Text(
                        ev.description,
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                }
                Spacer(Modifier.height(24.dp))
            }
        }
    }
}

@Composable
private fun InfoRow(label: String, value: String) {
    Row(verticalAlignment = Alignment.Top) {
        Text(
            label,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.width(48.dp),
        )
        Text(
            value,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.weight(1f),
        )
    }
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
