package ai.deneb.deneb

import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
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
import kotlinx.coroutines.launch
import kotlinx.datetime.LocalDate
import kotlinx.datetime.LocalDateTime
import kotlinx.datetime.LocalTime
import kotlinx.datetime.TimeZone
import kotlinx.datetime.toInstant
import kotlinx.datetime.toLocalDateTime
import kotlin.time.Clock
import kotlin.time.Instant

/**
 * To-do entry/editing (`miniapp.todo.create` / `update`). Title + optional note +
 * an optional due date (whole-day or a specific time). Saving posts to the gateway
 * and pops back; a gateway error is shown inline.
 *
 * Design split (see .claude/rules/native-design-system.md): frame + type are the
 * Deneb skin; the inputs are Material (OutlinedTextField/Switch/DatePicker/
 * TimePicker/Button). Body rendering lives in [TodoAddContent] so the render
 * harness can preview it; this composable is the stateful shell (pickers + save).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DenebTodoAddScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onSaved: () -> Unit,
    editTodoId: String? = null,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val isEdit = editTodoId != null
    val tz = remember { TimeZone.currentSystemDefault() }
    val now = remember { Clock.System.now().toLocalDateTime(tz) }
    val startHour = if (now.hour in 0..22) now.hour else 9

    var title by remember { mutableStateOf("") }
    var note by remember { mutableStateOf("") }
    var hasDue by remember { mutableStateOf(false) }
    var allDay by remember { mutableStateOf(true) }
    var dueDate by remember { mutableStateOf(now.date) }
    var dueTime by remember { mutableStateOf(LocalTime(startHour, 0)) }

    var showDatePicker by remember { mutableStateOf(false) }
    var showTimePicker by remember { mutableStateOf(false) }
    var saving by remember { mutableStateOf(false) }
    var deleting by remember { mutableStateOf(false) }
    var confirmDelete by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var prefilling by remember { mutableStateOf(isEdit) }
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()

    LaunchedEffect(editTodoId) {
        if (editTodoId == null) return@LaunchedEffect
        val td = client.fetchTodos()?.find { it.id == editTodoId }
        if (td != null) {
            title = td.title
            note = td.note
            if (td.due.isNotBlank()) {
                hasDue = true
                allDay = td.dueAllDay
                runCatching { Instant.parse(td.due).toLocalDateTime(tz) }.getOrNull()?.let { ldt ->
                    dueDate = ldt.date
                    if (!td.dueAllDay) dueTime = LocalTime(ldt.hour, ldt.minute)
                }
            }
        } else {
            error = "할 일을 불러오지 못했습니다."
        }
        prefilling = false
    }

    fun save() {
        if (title.isBlank()) {
            error = "제목을 입력해 주세요."
            return
        }
        scope.launch {
            saving = true
            error = null
            val dueIso = when {
                !hasDue -> ""
                allDay -> LocalDateTime(dueDate, LocalTime(0, 0)).toInstant(tz).toString()
                else -> LocalDateTime(dueDate, dueTime).toInstant(tz).toString()
            }
            val err = if (editTodoId != null) {
                client.updateTodo(editTodoId, title.trim(), note.trim(), dueIso, allDay)
            } else {
                client.createTodo(title.trim(), note.trim(), dueIso, allDay)
            }
            saving = false
            if (err == null) onSaved() else error = err
        }
    }

    fun delete() {
        val id = editTodoId ?: return
        scope.launch {
            deleting = true
            error = null
            val err = client.deleteTodo(id)
            deleting = false
            if (err == null) onSaved() else error = err
        }
    }

    // Guard against losing an in-progress todo to a stray back: snapshot the fields
    // once they're ready (after edit-prefill, or immediately for a new todo) and
    // confirm before leaving if they've since changed.
    val snapshot = listOf<Any?>(title, note, hasDue, allDay, dueDate, dueTime)
    val baseline = remember(prefilling) { if (!prefilling) snapshot else null }
    val requestBack = rememberDiscardGuard(baseline != null && snapshot != baseline, onBack)

    DenebScreenScaffold(title = if (isEdit) "할 일 편집" else "할 일 추가", onBack = requestBack, tabBar = navigationTabBar) {
        Column(
            Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(horizontal = 24.dp),
        ) {
            if (prefilling) {
                DenebLoading()
            } else {
                TodoAddContent(
                    title = title,
                    onTitle = { title = it },
                    note = note,
                    onNote = { note = it },
                    hasDue = hasDue,
                    onHasDue = { hasDue = it },
                    allDay = allDay,
                    onAllDay = { allDay = it },
                    dueDateLabel = todoDateLabel(dueDate),
                    onPickDate = { showDatePicker = true },
                    dueTimeLabel = todoTimeLabel(dueTime),
                    onPickTime = { showTimePicker = true },
                    error = error,
                    saving = saving || deleting,
                    saveLabel = if (isEdit) "저장" else "추가",
                    onSave = { save() },
                    onDelete = if (isEdit) {
                        { confirmDelete = true }
                    } else {
                        null
                    },
                )
            }
        }
    }

    if (confirmDelete) {
        AlertDialog(
            onDismissRequest = { if (!deleting) confirmDelete = false },
            title = { Text("할 일 삭제") },
            text = { Text("이 할 일을 삭제할까요? 되돌릴 수 없습니다.") },
            confirmButton = {
                TextButton(
                    enabled = !deleting,
                    onClick = {
                        confirmDelete = false
                        delete()
                    },
                ) {
                    Text("삭제", color = MaterialTheme.colorScheme.error)
                }
            },
            dismissButton = {
                TextButton(enabled = !deleting, onClick = { confirmDelete = false }) { Text("취소") }
            },
        )
    }

    if (showDatePicker) {
        val state = rememberDatePickerState(initialSelectedDateMillis = todoDateToUtcMillis(dueDate))
        DatePickerDialog(
            onDismissRequest = { showDatePicker = false },
            confirmButton = {
                TextButton(onClick = {
                    haptics.tap()
                    state.selectedDateMillis?.let { dueDate = todoUtcMillisToDate(it) }
                    showDatePicker = false
                }) { Text("확인") }
            },
            dismissButton = { TextButton(onClick = { showDatePicker = false }) { Text("취소") } },
        ) { DatePicker(state = state) }
    }
    if (showTimePicker) {
        val state = rememberTimePickerState(initialHour = dueTime.hour, initialMinute = dueTime.minute, is24Hour = true)
        AlertDialog(
            onDismissRequest = { showTimePicker = false },
            confirmButton = {
                TextButton(onClick = {
                    haptics.tap()
                    dueTime = LocalTime(state.hour, state.minute)
                    showTimePicker = false
                }) { Text("확인") }
            },
            dismissButton = { TextButton(onClick = { showTimePicker = false }) { Text("취소") } },
            text = { TimePicker(state = state) },
        )
    }
}

/**
 * Stateless add/edit-to-do form — extracted so [RenderPreview] can render it with
 * mock values. Pure presentation: Material inputs under Deneb section labels.
 */
@Composable
internal fun TodoAddContent(
    title: String,
    onTitle: (String) -> Unit,
    note: String,
    onNote: (String) -> Unit,
    hasDue: Boolean,
    onHasDue: (Boolean) -> Unit,
    allDay: Boolean,
    onAllDay: (Boolean) -> Unit,
    dueDateLabel: String,
    onPickDate: () -> Unit,
    dueTimeLabel: String,
    onPickTime: () -> Unit,
    error: String?,
    saving: Boolean,
    saveLabel: String,
    onSave: () -> Unit,
    onDelete: (() -> Unit)? = null,
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

    DenebSectionLabel("메모")
    OutlinedTextField(
        value = note,
        onValueChange = onNote,
        label = { Text("메모 (선택)") },
        minLines = 2,
        modifier = Modifier.fillMaxWidth(),
        keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(imeAction = ImeAction.Default),
    )

    DenebSectionLabel("마감일")
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text("마감일 설정", style = DenebType.body, color = MaterialTheme.colorScheme.onBackground, modifier = Modifier.weight(1f))
        Switch(checked = hasDue, onCheckedChange = {
            haptics.toggle(it)
            onHasDue(it)
        })
    }
    if (hasDue) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text("종일", style = DenebType.body, color = MaterialTheme.colorScheme.onBackground, modifier = Modifier.weight(1f))
            Switch(checked = allDay, onCheckedChange = {
                haptics.toggle(it)
                onAllDay(it)
            })
        }
        Spacer(Modifier.height(8.dp))
        if (allDay) {
            OutlinedButton(onClick = {
                haptics.tap()
                onPickDate()
            }, modifier = Modifier.fillMaxWidth()) { Text(dueDateLabel) }
        } else {
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(onClick = {
                    haptics.tap()
                    onPickDate()
                }, modifier = Modifier.weight(1f)) { Text(dueDateLabel) }
                OutlinedButton(onClick = {
                    haptics.tap()
                    onPickTime()
                }, modifier = Modifier.weight(1f)) { Text(dueTimeLabel) }
            }
        }
    }

    if (error != null) {
        Spacer(Modifier.height(12.dp))
        Text(error, style = DenebType.body, color = MaterialTheme.colorScheme.error)
    }

    Spacer(Modifier.height(20.dp))
    Button(onClick = {
        haptics.confirm()
        onSave()
    }, enabled = !saving, modifier = Modifier.fillMaxWidth()) {
        Text(if (saving) "$saveLabel 중…" else saveLabel)
    }
    if (onDelete != null) {
        Spacer(Modifier.height(8.dp))
        TextButton(
            onClick = {
                haptics.longPress()
                onDelete()
            },
            enabled = !saving,
            modifier = Modifier.fillMaxWidth(),
        ) {
            Text("삭제", color = MaterialTheme.colorScheme.error)
        }
    }
    Spacer(Modifier.height(24.dp))
}

// --- helpers --------------------------------------------------------------

private fun todoDateLabel(d: LocalDate): String {
    val dow = koreanDayOfWeek.getOrElse(d.dayOfWeek.ordinal) { "" }
    return "${d.year}년 ${d.month.ordinal + 1}월 ${d.day}일 ($dow)"
}

private fun todoTimeLabel(t: LocalTime): String = "${t.hour.toString().padStart(2, '0')}:${t.minute.toString().padStart(2, '0')}"

private fun todoDateToUtcMillis(d: LocalDate): Long = LocalDateTime(d, LocalTime(0, 0)).toInstant(TimeZone.UTC).toEpochMilliseconds()

private fun todoUtcMillisToDate(ms: Long): LocalDate = Instant.fromEpochMilliseconds(ms).toLocalDateTime(TimeZone.UTC).date
