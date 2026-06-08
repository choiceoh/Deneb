package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TimePicker
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.material3.rememberTimePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.DenebScreenScaffold
import com.inspiredandroid.kai.ui.DenebSectionLabel
import com.inspiredandroid.kai.ui.DenebType
import com.inspiredandroid.kai.ui.components.rememberHaptics
import kotlinx.coroutines.launch
import kotlinx.datetime.DateTimeUnit
import kotlinx.datetime.LocalDate
import kotlinx.datetime.LocalDateTime
import kotlinx.datetime.LocalTime
import kotlinx.datetime.TimeZone
import kotlinx.datetime.minus
import kotlinx.datetime.plus
import kotlinx.datetime.toInstant
import kotlinx.datetime.toLocalDateTime
import kotlin.time.Clock
import kotlin.time.Instant

/**
 * Manual event entry (`miniapp.calendar.create`). Title + date + start/end times
 * (hidden when all-day) + location + description; saving posts to the gateway and
 * pops back on success. A write-scope error from the gateway is shown inline.
 *
 * Design split (see .claude/rules/native-design-system.md): frame + type are the
 * Deneb skin; the inputs are Material (OutlinedTextField/Switch/DatePicker/
 * TimePicker/Button). Body rendering lives in [CalendarAddContent] so the render
 * harness can preview it; this composable is the stateful shell (pickers + save).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DenebCalendarAddScreen(
    client: DenebGatewayClient,
    initialDateIso: String,
    onBack: () -> Unit,
    onSaved: () -> Unit,
    editEventId: String? = null,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val isEdit = editEventId != null
    val tz = remember { TimeZone.currentSystemDefault() }
    val now = remember { Clock.System.now().toLocalDateTime(tz) }
    val startHour = if (now.hour in 0..22) now.hour else 9

    var title by remember { mutableStateOf("") }
    var location by remember { mutableStateOf("") }
    var description by remember { mutableStateOf("") }
    var allDay by remember { mutableStateOf(false) }
    var startDate by remember { mutableStateOf(parseDateOr(initialDateIso, now.date)) }
    var endDate by remember { mutableStateOf(parseDateOr(initialDateIso, now.date)) }
    var startTime by remember { mutableStateOf(LocalTime(startHour, 0)) }
    var endTime by remember { mutableStateOf(LocalTime(startHour + 1, 0)) }

    var showStartDatePicker by remember { mutableStateOf(false) }
    var showEndDatePicker by remember { mutableStateOf(false) }
    var showStartPicker by remember { mutableStateOf(false) }
    var showEndPicker by remember { mutableStateOf(false) }
    var saving by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    // For edit mode, fields are populated from the fetched event before the form shows.
    var prefilling by remember { mutableStateOf(isEdit) }
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()

    LaunchedEffect(editEventId) {
        if (editEventId == null) return@LaunchedEffect
        val ev = client.fetchCalendarEvent(editEventId)
        if (ev != null) {
            title = ev.title
            location = ev.location
            description = ev.description
            allDay = ev.allDay
            rfcToLocal(ev.start, tz)?.let { (d, t) ->
                startDate = d
                endDate = d
                if (!ev.allDay) startTime = t
            }
            rfcToLocal(ev.end, tz)?.let { (d, t) ->
                if (ev.allDay) {
                    endDate = allDayEndToInclusive(d, t, startDate)
                } else {
                    endDate = d
                    endTime = t
                }
            }
        } else {
            error = "일정을 불러오지 못했습니다."
        }
        prefilling = false
    }

    fun save() {
        if (title.isBlank()) {
            error = "제목을 입력해 주세요."
            return
        }
        if (allDay) {
            if (endDate < startDate) {
                error = "종료 날짜는 시작 날짜와 같거나 뒤여야 합니다."
                return
            }
        } else if (LocalDateTime(endDate, endTime).toInstant(tz) <= LocalDateTime(startDate, startTime).toInstant(tz)) {
            error = "종료가 시작보다 뒤여야 합니다."
            return
        }
        scope.launch {
            saving = true
            error = null
            val (startIso, endIso) = if (allDay) {
                // All-day end is exclusive: midnight of the day after the last day.
                LocalDateTime(startDate, LocalTime(0, 0)).toInstant(tz).toString() to
                    LocalDateTime(endDate.plus(1, DateTimeUnit.DAY), LocalTime(0, 0)).toInstant(tz).toString()
            } else {
                LocalDateTime(startDate, startTime).toInstant(tz).toString() to
                    LocalDateTime(endDate, endTime).toInstant(tz).toString()
            }
            val err = if (editEventId != null) {
                client.updateCalendarEvent(
                    id = editEventId,
                    summary = title.trim(),
                    description = description.trim(),
                    location = location.trim(),
                    allDay = allDay,
                    startIso = startIso,
                    endIso = endIso,
                    timeZone = tz.id,
                )
            } else {
                client.createCalendarEvent(
                    summary = title.trim(),
                    description = description.trim(),
                    location = location.trim(),
                    allDay = allDay,
                    startIso = startIso,
                    endIso = endIso,
                    timeZone = tz.id,
                )
            }
            saving = false
            if (err == null) onSaved() else error = err
        }
    }

    DenebScreenScaffold(title = if (isEdit) "일정 편집" else "일정 추가", onBack = onBack, tabBar = navigationTabBar) {
        Column(
            Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(horizontal = 24.dp),
        ) {
            if (prefilling) {
                DenebLoading()
            } else {
                CalendarAddContent(
                    title = title,
                    onTitle = { title = it },
                    allDay = allDay,
                    onAllDay = { allDay = it },
                    startDateLabel = dateLabel(startDate),
                    onPickStartDate = { showStartDatePicker = true },
                    endDateLabel = dateLabel(endDate),
                    onPickEndDate = { showEndDatePicker = true },
                    startLabel = timeLabel(startTime),
                    onPickStart = { showStartPicker = true },
                    endLabel = timeLabel(endTime),
                    onPickEnd = { showEndPicker = true },
                    location = location,
                    onLocation = { location = it },
                    description = description,
                    onDescription = { description = it },
                    error = error,
                    saving = saving,
                    saveLabel = if (isEdit) "저장" else "추가",
                    onSave = { save() },
                )
            }
        }
    }

    if (showStartDatePicker) {
        val state = rememberDatePickerState(initialSelectedDateMillis = localDateToUtcMillis(startDate))
        DatePickerDialog(
            onDismissRequest = { showStartDatePicker = false },
            confirmButton = {
                TextButton(onClick = {
                    haptics.tap()
                    state.selectedDateMillis?.let {
                        val picked = utcMillisToLocalDate(it)
                        startDate = picked
                        if (endDate < picked) endDate = picked // keep end on/after start
                    }
                    showStartDatePicker = false
                }) { Text("확인") }
            },
            dismissButton = { TextButton(onClick = { showStartDatePicker = false }) { Text("취소") } },
        ) { DatePicker(state = state) }
    }
    if (showEndDatePicker) {
        val state = rememberDatePickerState(initialSelectedDateMillis = localDateToUtcMillis(endDate))
        DatePickerDialog(
            onDismissRequest = { showEndDatePicker = false },
            confirmButton = {
                TextButton(onClick = {
                    haptics.tap()
                    state.selectedDateMillis?.let {
                        val picked = utcMillisToLocalDate(it)
                        endDate = picked
                        if (startDate > picked) startDate = picked // keep start on/before end
                    }
                    showEndDatePicker = false
                }) { Text("확인") }
            },
            dismissButton = { TextButton(onClick = { showEndDatePicker = false }) { Text("취소") } },
        ) { DatePicker(state = state) }
    }
    if (showStartPicker) {
        TimePickerDialog(
            initial = startTime,
            onConfirm = {
                startTime = it
                // Same-day event: nudge end to +1h if it now precedes start.
                if (startDate == endDate && !timeAfter(endTime, startTime)) {
                    endTime = LocalTime((it.hour + 1) % 24, it.minute)
                }
                showStartPicker = false
            },
            onDismiss = { showStartPicker = false },
        )
    }
    if (showEndPicker) {
        TimePickerDialog(
            initial = endTime,
            onConfirm = { endTime = it; showEndPicker = false },
            onDismiss = { showEndPicker = false },
        )
    }
}

/**
 * Stateless add-event form — extracted so [RenderPreview] can render it with mock
 * values. Pure presentation: Material inputs under Deneb section labels.
 */
@Composable
internal fun CalendarAddContent(
    title: String,
    onTitle: (String) -> Unit,
    allDay: Boolean,
    onAllDay: (Boolean) -> Unit,
    startDateLabel: String,
    onPickStartDate: () -> Unit,
    endDateLabel: String,
    onPickEndDate: () -> Unit,
    startLabel: String,
    onPickStart: () -> Unit,
    endLabel: String,
    onPickEnd: () -> Unit,
    location: String,
    onLocation: (String) -> Unit,
    description: String,
    onDescription: (String) -> Unit,
    error: String?,
    saving: Boolean,
    saveLabel: String,
    onSave: () -> Unit,
) {
    val haptics = rememberHaptics()
    Spacer(Modifier.height(8.dp))
    OutlinedTextField(
        value = title,
        onValueChange = onTitle,
        label = { Text("제목") },
        singleLine = true,
        modifier = Modifier.fillMaxWidth(),
    )

    DenebSectionLabel("일시")
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text("종일", style = DenebType.body, color = MaterialTheme.colorScheme.onBackground, modifier = Modifier.weight(1f))
        Switch(checked = allDay, onCheckedChange = onAllDay)
    }
    Spacer(Modifier.height(8.dp))
    OutlinedButton(onClick = { haptics.tap(); onPickStartDate() }, modifier = Modifier.fillMaxWidth()) { Text("시작 $startDateLabel") }
    Spacer(Modifier.height(8.dp))
    OutlinedButton(onClick = { haptics.tap(); onPickEndDate() }, modifier = Modifier.fillMaxWidth()) { Text("종료 $endDateLabel") }
    if (!allDay) {
        Spacer(Modifier.height(8.dp))
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedButton(onClick = { haptics.tap(); onPickStart() }, modifier = Modifier.weight(1f)) { Text("시작 $startLabel") }
            OutlinedButton(onClick = { haptics.tap(); onPickEnd() }, modifier = Modifier.weight(1f)) { Text("종료 $endLabel") }
        }
    }

    DenebSectionLabel("장소")
    OutlinedTextField(
        value = location,
        onValueChange = onLocation,
        label = { Text("장소 (선택)") },
        singleLine = true,
        modifier = Modifier.fillMaxWidth(),
    )

    DenebSectionLabel("설명")
    OutlinedTextField(
        value = description,
        onValueChange = onDescription,
        label = { Text("설명 (선택)") },
        minLines = 3,
        modifier = Modifier.fillMaxWidth(),
        keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(imeAction = ImeAction.Default),
    )

    if (error != null) {
        Spacer(Modifier.height(12.dp))
        Text(error, style = DenebType.body, color = MaterialTheme.colorScheme.error)
    }

    Spacer(Modifier.height(20.dp))
    Button(onClick = { haptics.confirm(); onSave() }, enabled = !saving, modifier = Modifier.fillMaxWidth()) {
        Text(if (saving) "$saveLabel 중…" else saveLabel)
    }
    Spacer(Modifier.height(24.dp))
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun TimePickerDialog(
    initial: LocalTime,
    onConfirm: (LocalTime) -> Unit,
    onDismiss: () -> Unit,
) {
    val state = rememberTimePickerState(initialHour = initial.hour, initialMinute = initial.minute, is24Hour = true)
    val haptics = rememberHaptics()
    AlertDialog(
        onDismissRequest = onDismiss,
        confirmButton = { TextButton(onClick = { haptics.tap(); onConfirm(LocalTime(state.hour, state.minute)) }) { Text("확인") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("취소") } },
        text = { TimePicker(state = state) },
    )
}

// --- helpers --------------------------------------------------------------

// allDayEndToInclusive converts a stored all-day end (exclusive — midnight after
// the last day) back to the last visible day, for pre-filling the edit form.
private fun allDayEndToInclusive(d: LocalDate, t: LocalTime, start: LocalDate): LocalDate =
    if (t == LocalTime(0, 0) && d > start) d.minus(1, DateTimeUnit.DAY) else d

private fun parseDateOr(iso: String, fallback: LocalDate): LocalDate {
    if (iso.isBlank()) return fallback
    return runCatching { LocalDate.parse(iso) }.getOrDefault(fallback)
}

// rfcToLocal splits an RFC3339 instant into its local date + time-of-day, used to
// pre-fill the edit form from a fetched event.
private fun rfcToLocal(iso: String, tz: TimeZone): Pair<LocalDate, LocalTime>? {
    if (iso.isBlank()) return null
    return runCatching {
        val ldt = Instant.parse(iso).toLocalDateTime(tz)
        ldt.date to LocalTime(ldt.hour, ldt.minute)
    }.getOrNull()
}

private fun dateLabel(d: LocalDate): String {
    val dow = koreanDayOfWeek.getOrElse(d.dayOfWeek.ordinal) { "" }
    return "${d.year}년 ${d.month.ordinal + 1}월 ${d.day}일 ($dow)"
}

private fun timeLabel(t: LocalTime): String =
    "${t.hour.toString().padStart(2, '0')}:${t.minute.toString().padStart(2, '0')}"

private fun timeAfter(a: LocalTime, b: LocalTime): Boolean =
    a.hour * 60 + a.minute > b.hour * 60 + b.minute

private fun localDateToUtcMillis(d: LocalDate): Long =
    LocalDateTime(d, LocalTime(0, 0)).toInstant(TimeZone.UTC).toEpochMilliseconds()

private fun utcMillisToLocalDate(ms: Long): LocalDate =
    Instant.fromEpochMilliseconds(ms).toLocalDateTime(TimeZone.UTC).date
