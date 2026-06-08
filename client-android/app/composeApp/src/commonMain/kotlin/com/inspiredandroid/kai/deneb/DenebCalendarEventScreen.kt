package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
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
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.DenebScreenScaffold
import com.inspiredandroid.kai.ui.DenebSectionLabel
import com.inspiredandroid.kai.ui.DenebType
import com.inspiredandroid.kai.ui.components.rememberHaptics
import com.inspiredandroid.kai.ui.denebHint
import kotlinx.coroutines.launch
import kotlinx.datetime.LocalDate
import kotlinx.datetime.TimeZone

/**
 * Calendar event detail (`miniapp.calendar.get`): when, location, organizer,
 * attendees and description. Locally-stored events (created in this app) also get
 * 편집 / 삭제 actions; read-only Google events don't.
 *
 * Design split (see .claude/rules/native-design-system.md): the frame + type are
 * the Deneb typographic skin (DenebScreenScaffold + DenebType + DenebSectionLabel).
 * The loaded-event presentation lives in [CalendarEventContent] — a stateless body
 * the render harness previews with mock data; this composable is the stateful shell
 * (load + loading/error states + delete confirm).
 */
@Composable
fun DenebCalendarEventScreen(
    client: DenebGatewayClient,
    eventId: String,
    onBack: () -> Unit,
    onEdit: (String) -> Unit = {},
    onDeleted: () -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var event by remember(eventId) { mutableStateOf<CalendarEventDetail?>(null) }
    var loadFailed by remember(eventId) { mutableStateOf(false) }
    var showDelete by remember(eventId) { mutableStateOf(false) }
    var deleting by remember(eventId) { mutableStateOf(false) }
    var actionError by remember(eventId) { mutableStateOf<String?>(null) }
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
                    isLocal = ev.local,
                    actionError = actionError,
                    onEdit = { onEdit(eventId) },
                    onDelete = { showDelete = true },
                )
            }
        }
    }

    if (showDelete) {
        AlertDialog(
            onDismissRequest = { if (!deleting) showDelete = false },
            title = { Text("일정 삭제") },
            text = { Text("이 일정을 삭제할까요? 되돌릴 수 없습니다.") },
            confirmButton = {
                TextButton(
                    enabled = !deleting,
                    onClick = {
                        haptics.reject()
                        scope.launch {
                            deleting = true
                            actionError = null
                            val err = client.deleteCalendarEvent(eventId)
                            deleting = false
                            showDelete = false
                            if (err == null) onDeleted() else actionError = err
                        }
                    },
                ) { Text("삭제") }
            },
            dismissButton = {
                TextButton(enabled = !deleting, onClick = { showDelete = false }) { Text("취소") }
            },
        )
    }
}

/**
 * Stateless presentation of a loaded event — extracted so [RenderPreview] can render
 * it with mock data. Pure Deneb type skin, with Material action buttons for local
 * events (controls = Material per the design rules).
 */
@Composable
internal fun CalendarEventContent(
    ev: CalendarEventDetail,
    isLocal: Boolean = false,
    actionError: String? = null,
    onEdit: () -> Unit = {},
    onDelete: () -> Unit = {},
) {
    val haptics = rememberHaptics()
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

    if (isLocal) {
        Spacer(Modifier.height(20.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedButton(onClick = { haptics.tap(); onEdit() }) { Text("편집") }
            OutlinedButton(onClick = { haptics.reject(); onDelete() }) { Text("삭제") }
        }
        if (actionError != null) {
            Spacer(Modifier.height(8.dp))
            Text(actionError, style = DenebType.meta, color = MaterialTheme.colorScheme.error)
        }
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

/** "5월 31일 (토) 14:00 – 15:00", "5월 31일 (토) · 종일", or for a multi-day span
 *  "5월 31일 (토) ~ 6월 2일 (월) · 종일" / "5월 31일 (토) 22:00 – 6월 1일 (일) 02:00".
 *  Uses eventDays so the all-day exclusive end resolves to the real last day. */
private fun whenLabel(ev: CalendarEventDetail): String {
    val tz = TimeZone.currentSystemDefault()
    val days = eventDays(ev.start, ev.end, ev.allDay, tz)
    val startDate = days.firstOrNull()
    val lastDate = days.lastOrNull()
    val multiDay = startDate != null && lastDate != null && lastDate != startDate

    val start = stampOf(ev.start, ev.allDay)
    val day = start?.dayLabel ?: "날짜 미정"

    if (ev.allDay) {
        return if (multiDay && lastDate != null) "$day ~ ${dayLabelOf(lastDate)} · 종일" else "$day · 종일"
    }
    val end = stampOf(ev.end, ev.allDay)
    val startTime = start?.time ?: ""
    return when {
        multiDay && lastDate != null && end != null -> "$day  $startTime – ${dayLabelOf(lastDate)} ${end.time}"
        end != null -> "$day  $startTime – ${end.time}"
        else -> "$day  $startTime"
    }
}

/** "6월 11일 (수)" — an explicit endpoint label (no 오늘/내일) for a span's end day. */
private fun dayLabelOf(d: LocalDate): String {
    val month = d.month.ordinal + 1
    val dow = koreanDayOfWeek.getOrElse(d.dayOfWeek.ordinal) { "" }
    return "${month}월 ${d.day}일 ($dow)"
}
