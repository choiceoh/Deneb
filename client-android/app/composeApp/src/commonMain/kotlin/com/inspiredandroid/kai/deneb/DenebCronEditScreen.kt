@file:OptIn(ExperimentalMaterial3Api::class)

package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.DenebScreenScaffold
import com.inspiredandroid.kai.ui.DenebSectionLabel
import com.inspiredandroid.kai.ui.DenebType
import com.inspiredandroid.kai.ui.denebHairline
import com.inspiredandroid.kai.ui.denebHint
import com.inspiredandroid.kai.ui.components.rememberHaptics
import kotlinx.coroutines.launch
import kotlinx.datetime.LocalDate
import kotlinx.datetime.LocalDateTime
import kotlinx.datetime.LocalTime
import kotlinx.datetime.TimeZone
import kotlinx.datetime.toInstant
import kotlinx.datetime.toLocalDateTime
import kotlin.time.Clock
import kotlin.time.Instant

// How a job's schedule is being edited. The first four are friendly pickers that
// build a cron/interval/ISO spec for you; ADVANCED is the raw escape hatch for
// expressions the pickers can't represent (e.g. "*/5 8-22 * * 1-6").
enum class SchedMode { DAILY, WEEKLY, INTERVAL, ONCE, ADVANCED }

enum class IntervalUnit { MIN, HOUR }

/**
 * The schedule editor's working state. One source of truth that the picker controls
 * mutate; [buildScheduleSpec] turns it back into the gateway's smart-schedule string
 * on save, and [parseScheduleDraft] seeds it from the job being edited.
 */
data class ScheduleDraft(
    val mode: SchedMode,
    val timeText: String, // "HH:MM" for DAILY/WEEKLY/ONCE
    val weekdays: Set<Int>, // 0=Sun … 6=Sat, for WEEKLY
    val intervalText: String, // the number for INTERVAL
    val intervalUnit: IntervalUnit,
    val onceDate: LocalDate, // for ONCE
    val rawSpec: String, // for ADVANCED (verbatim spec)
)

/**
 * Cron edit form (`miniapp.crons.update`): rename a job, change when it runs, what it
 * does (the prompt), and which model runs it. The schedule is a friendly picker —
 * 매일/매주/주기/한 번 with directly-editable time/weekday/interval inputs — and falls
 * back to a raw "직접 입력" field for expressions the pickers can't model. Only fields
 * the user actually changed are sent, so editing one never blanks another; the gateway
 * validates the resulting spec and its rejection reason is surfaced inline.
 *
 * Design (.claude/rules/native-design-system.md): the Deneb skin is flat — typography on
 * the AMOLED surface, fields underlined by a single hairline (no Material fills/cards),
 * so this reads like the mail/calendar detail screens. Genuine controls stay Material
 * (SegmentedButton, FilterChip, DatePicker). Body rendering lives in [CronEditContent]
 * so the render harness can preview it; this is the stateful shell (fetch + prefill + save).
 */
@Composable
fun DenebCronEditScreen(
    client: DenebGatewayClient,
    cronId: String,
    onBack: () -> Unit,
    onSaved: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val today = remember { Clock.System.now().toLocalDateTime(TimeZone.currentSystemDefault()).date }
    // original is the fetched job we diff against on save; null means "still loading"
    // (prefilling true) or "load failed" (prefilling done, original still null).
    var original by remember(cronId) { mutableStateOf<CronDetail?>(null) }
    var name by remember(cronId) { mutableStateOf("") }
    var draft by remember(cronId) { mutableStateOf(emptyDraft(today)) }
    var tz by remember(cronId) { mutableStateOf("") }
    var prompt by remember(cronId) { mutableStateOf("") }
    var model by remember(cronId) { mutableStateOf("") }
    var prefilling by remember(cronId) { mutableStateOf(true) }
    var saving by remember(cronId) { mutableStateOf(false) }
    var error by remember(cronId) { mutableStateOf<String?>(null) }
    var showDatePicker by remember(cronId) { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    suspend fun load() {
        prefilling = true
        error = null
        val c = client.fetchCron(cronId)
        if (c != null) {
            original = c
            name = c.name
            draft = parseScheduleDraft(c.scheduleKind, c.scheduleSpec, today)
            tz = c.timezone
            prompt = c.prompt
            model = c.model
        }
        prefilling = false
    }
    LaunchedEffect(cronId) { load() }

    fun save() {
        val orig = original ?: return
        // Build (and validate) the schedule first so a bad time/empty-weekday is caught
        // before the network call; the gateway re-validates the spec as a backstop.
        val spec = buildScheduleSpec(draft, TimeZone.currentSystemDefault())
            .getOrElse { error = it.message ?: "일정을 확인해 주세요."; return }
        if (prompt.isBlank()) {
            error = "작업 내용을 입력해 주세요."
            return
        }
        scope.launch {
            saving = true
            error = null
            // Diff against the fetched job: send only what changed. takeIf maps an
            // unchanged field to null, which updateCron omits from the patch — so
            // editing one field can't clear another, and the timezone is left alone
            // for interval/once jobs where its input isn't even shown.
            val err = client.updateCron(
                id = orig.id,
                name = name.trim().takeIf { it != orig.name },
                schedule = spec.takeIf { it != orig.scheduleSpec },
                tz = tz.trim().takeIf { it != orig.timezone },
                prompt = prompt.trim().takeIf { it != orig.prompt },
                model = model.trim().takeIf { it != orig.model },
            )
            saving = false
            if (err == null) onSaved() else error = err
        }
    }

    DenebScreenScaffold(title = "크론 편집", onBack = onBack, tabBar = navigationTabBar) {
        Column(
            Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(horizontal = 24.dp),
        ) {
            when {
                prefilling -> DenebLoading()
                original == null -> DenebError("크론을 불러오지 못했습니다.", onRetry = { scope.launch { load() } })
                else -> CronEditContent(
                    name = name,
                    onName = { name = it },
                    draft = draft,
                    onDraft = { draft = it },
                    onceDateLabel = dateLabelKo(draft.onceDate),
                    onPickOnceDate = { showDatePicker = true },
                    tz = tz,
                    onTz = { tz = it },
                    prompt = prompt,
                    onPrompt = { prompt = it },
                    model = model,
                    onModel = { model = it },
                    error = error,
                    saving = saving,
                    onSave = { save() },
                )
            }
        }
    }

    if (showDatePicker) {
        val state = rememberDatePickerState(initialSelectedDateMillis = localDateToUtcMillis(draft.onceDate))
        DatePickerDialog(
            onDismissRequest = { showDatePicker = false },
            confirmButton = {
                TextButton(onClick = {
                    state.selectedDateMillis?.let { draft = draft.copy(onceDate = utcMillisToLocalDate(it)) }
                    showDatePicker = false
                }) { Text("확인") }
            },
            dismissButton = { TextButton(onClick = { showDatePicker = false }) { Text("취소") } },
        ) { DatePicker(state = state) }
    }
}

/**
 * Stateless cron edit form — extracted so [RenderPreview] can render it with mock
 * values. Pure presentation: flat hairline fields and a schedule picker on the AMOLED
 * surface.
 */
@Composable
internal fun CronEditContent(
    name: String,
    onName: (String) -> Unit,
    draft: ScheduleDraft,
    onDraft: (ScheduleDraft) -> Unit,
    onceDateLabel: String,
    onPickOnceDate: () -> Unit,
    tz: String,
    onTz: (String) -> Unit,
    prompt: String,
    onPrompt: (String) -> Unit,
    model: String,
    onModel: (String) -> Unit,
    error: String?,
    saving: Boolean,
    onSave: () -> Unit,
) {
    val haptics = rememberHaptics()
    Spacer(Modifier.height(12.dp))
    DenebField(value = name, onValueChange = onName, label = "이름", modifier = Modifier.fillMaxWidth())

    DenebSectionLabel("일정")
    ScheduleEditor(
        draft = draft,
        onDraft = onDraft,
        tz = tz,
        onTz = onTz,
        onceDateLabel = onceDateLabel,
        onPickOnceDate = onPickOnceDate,
    )

    DenebSectionLabel("작업")
    DenebField(
        value = prompt,
        onValueChange = onPrompt,
        label = "내용",
        singleLine = false,
        minLines = 3,
        modifier = Modifier.fillMaxWidth(),
    )
    Spacer(Modifier.height(20.dp))
    DenebField(
        value = model,
        onValueChange = onModel,
        label = "모델 (선택)",
        placeholder = "비워두면 기본 모델",
        modifier = Modifier.fillMaxWidth(),
    )

    if (error != null) {
        Spacer(Modifier.height(16.dp))
        Text(error, style = DenebType.body, color = MaterialTheme.colorScheme.error)
    }

    Spacer(Modifier.height(28.dp))
    Button(onClick = { haptics.confirm(); onSave() }, enabled = !saving, modifier = Modifier.fillMaxWidth()) {
        Text(if (saving) "저장 중…" else "저장")
    }
    Spacer(Modifier.height(28.dp))
}

// ScheduleEditor: the frequency segmented control, the inputs for the chosen mode, the
// timezone (cron-based modes only), and the "직접 입력 (cron)" escape hatch. Everything
// here mutates the single [ScheduleDraft]; the spec string is only built on save.
@Composable
private fun ScheduleEditor(
    draft: ScheduleDraft,
    onDraft: (ScheduleDraft) -> Unit,
    tz: String,
    onTz: (String) -> Unit,
    onceDateLabel: String,
    onPickOnceDate: () -> Unit,
) {
    val haptics = rememberHaptics()
    val segments = listOf(
        SchedMode.DAILY to "매일",
        SchedMode.WEEKLY to "매주",
        SchedMode.INTERVAL to "주기",
        SchedMode.ONCE to "한 번",
    )
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
        segments.forEachIndexed { i, (m, label) ->
            SegmentedButton(
                selected = draft.mode == m,
                onClick = { haptics.tap(); onDraft(draft.copy(mode = m)) },
                shape = SegmentedButtonDefaults.itemShape(i, segments.size),
            ) { Text(label) }
        }
    }

    Spacer(Modifier.height(20.dp))
    when (draft.mode) {
        SchedMode.DAILY -> TimeField(draft, onDraft)
        SchedMode.WEEKLY -> {
            WeekdayChips(draft, onDraft)
            Spacer(Modifier.height(20.dp))
            TimeField(draft, onDraft)
        }
        SchedMode.INTERVAL -> IntervalField(draft, onDraft)
        SchedMode.ONCE -> {
            DenebPickerRow(label = "날짜", value = onceDateLabel, onClick = onPickOnceDate)
            Spacer(Modifier.height(20.dp))
            TimeField(draft, onDraft)
        }
        SchedMode.ADVANCED -> {
            DenebField(
                value = draft.rawSpec,
                onValueChange = { onDraft(draft.copy(rawSpec = it)) },
                label = "스케줄 (cron · 간격 · 시각)",
                placeholder = "0 9 * * 1-5",
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(8.dp))
            Text(
                "예: 0 9 * * * (매일 09시) · 15m (15분마다) · 2026-06-10T09:00:00+09:00 (1회)",
                style = DenebType.meta,
                color = denebHint(),
            )
        }
    }

    if (draft.mode == SchedMode.DAILY || draft.mode == SchedMode.WEEKLY || draft.mode == SchedMode.ADVANCED) {
        Spacer(Modifier.height(20.dp))
        DenebField(value = tz, onValueChange = onTz, label = "시간대 (선택)", placeholder = "Asia/Seoul", modifier = Modifier.fillMaxWidth())
    }

    Spacer(Modifier.height(10.dp))
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
        TextButton(onClick = {
            haptics.tap()
            onDraft(draft.copy(mode = if (draft.mode == SchedMode.ADVANCED) SchedMode.DAILY else SchedMode.ADVANCED))
        }) {
            Text(if (draft.mode == SchedMode.ADVANCED) "간편 입력" else "직접 입력 (cron)")
        }
    }
}

@Composable
private fun TimeField(draft: ScheduleDraft, onDraft: (ScheduleDraft) -> Unit) {
    DenebField(
        value = draft.timeText,
        onValueChange = { onDraft(draft.copy(timeText = it)) },
        label = "시간",
        placeholder = "09:00",
        // HH:MM needs a colon; a number keyboard hides ":" on most Android IMEs,
        // so use the text keyboard for this field.
        numeric = false,
        modifier = Modifier.fillMaxWidth(),
    )
}

@Composable
private fun WeekdayChips(draft: ScheduleDraft, onDraft: (ScheduleDraft) -> Unit) {
    val labels = listOf("일", "월", "화", "수", "목", "금", "토")
    val haptics = rememberHaptics()
    Text("요일", style = DenebType.meta, color = denebHint())
    Spacer(Modifier.height(8.dp))
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
        labels.forEachIndexed { day, label ->
            FilterChip(
                selected = day in draft.weekdays,
                onClick = {
                    haptics.tap()
                    val next = draft.weekdays.toMutableSet().apply { if (!add(day)) remove(day) }
                    onDraft(draft.copy(weekdays = next))
                },
                label = { Text(label) },
                modifier = Modifier.weight(1f),
            )
        }
    }
}

@Composable
private fun IntervalField(draft: ScheduleDraft, onDraft: (ScheduleDraft) -> Unit) {
    Text("주기", style = DenebType.meta, color = denebHint())
    Spacer(Modifier.height(8.dp))
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.width(72.dp)) {
            BasicTextField(
                value = draft.intervalText,
                onValueChange = { onDraft(draft.copy(intervalText = it.filter(Char::isDigit))) },
                textStyle = DenebType.body.copy(color = MaterialTheme.colorScheme.onBackground),
                cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                modifier = Modifier.fillMaxWidth(),
                decorationBox = { inner ->
                    if (draft.intervalText.isEmpty()) Text("30", style = DenebType.body, color = denebHint())
                    inner()
                },
            )
            Spacer(Modifier.height(8.dp))
            HorizontalDivider(color = denebHairline())
        }
        Spacer(Modifier.width(16.dp))
        val units = listOf(IntervalUnit.MIN to "분", IntervalUnit.HOUR to "시간")
        SingleChoiceSegmentedButtonRow {
            units.forEachIndexed { i, (u, label) ->
                SegmentedButton(
                    selected = draft.intervalUnit == u,
                    onClick = { onDraft(draft.copy(intervalUnit = u)) },
                    shape = SegmentedButtonDefaults.itemShape(i, units.size),
                ) { Text(label) }
            }
        }
    }
}

// DenebField: one flat, hairline-underlined input — a muted label, the value in light
// Pretendard body, and a single hairline rule beneath. No box, no fill, so the form
// reads like the rest of the Deneb (mail/calendar detail) surface rather than a stack
// of Material boxes. The control itself is a foundation BasicTextField.
@Composable
private fun DenebField(
    value: String,
    onValueChange: (String) -> Unit,
    label: String,
    modifier: Modifier = Modifier,
    placeholder: String? = null,
    numeric: Boolean = false,
    singleLine: Boolean = true,
    minLines: Int = 1,
) {
    Column(modifier) {
        Text(label, style = DenebType.meta, color = denebHint())
        Spacer(Modifier.height(8.dp))
        BasicTextField(
            value = value,
            onValueChange = onValueChange,
            textStyle = DenebType.body.copy(color = MaterialTheme.colorScheme.onBackground),
            cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
            singleLine = singleLine,
            minLines = minLines,
            keyboardOptions = KeyboardOptions(
                keyboardType = if (numeric) KeyboardType.Number else KeyboardType.Text,
                imeAction = ImeAction.Default,
            ),
            modifier = Modifier.fillMaxWidth(),
            decorationBox = { inner ->
                if (value.isEmpty() && placeholder != null) {
                    Text(placeholder, style = DenebType.body, color = denebHint())
                }
                inner()
            },
        )
        Spacer(Modifier.height(10.dp))
        HorizontalDivider(color = denebHairline())
    }
}

// DenebPickerRow: a read-only field that opens a picker on tap (the one-shot date). Same
// flat hairline treatment as [DenebField] so it sits in the same rhythm.
@Composable
private fun DenebPickerRow(label: String, value: String, onClick: () -> Unit) {
    Column(Modifier.fillMaxWidth().clickable(onClick = onClick)) {
        Text(label, style = DenebType.meta, color = denebHint())
        Spacer(Modifier.height(8.dp))
        Text(value, style = DenebType.body, color = MaterialTheme.colorScheme.onBackground)
        Spacer(Modifier.height(10.dp))
        HorizontalDivider(color = denebHairline())
    }
}

// --- schedule spec <-> draft ----------------------------------------------------

private fun emptyDraft(today: LocalDate): ScheduleDraft =
    ScheduleDraft(SchedMode.DAILY, "09:00", emptySet(), "30", IntervalUnit.MIN, today, "")

// parseScheduleDraft seeds the editor from a job's stored schedule. Daily/weekly cron
// expressions and simple intervals map to the friendly pickers; anything richer (ranges,
// steps, minute lists) falls back to ADVANCED with the raw spec so it stays editable.
internal fun parseScheduleDraft(kind: String, spec: String, today: LocalDate): ScheduleDraft {
    val base = emptyDraft(today).copy(mode = SchedMode.ADVANCED, rawSpec = spec.trim())
    val s = spec.trim()
    return when (kind) {
        "every" -> {
            val match = Regex("^(\\d+)(m|h)$").find(s)
            if (match != null) {
                base.copy(
                    mode = SchedMode.INTERVAL,
                    intervalText = match.groupValues[1],
                    intervalUnit = if (match.groupValues[2] == "h") IntervalUnit.HOUR else IntervalUnit.MIN,
                )
            } else {
                base
            }
        }
        "at" -> {
            val instant = runCatching { Instant.parse(s) }.getOrNull()
            if (instant != null) {
                val ldt = instant.toLocalDateTime(TimeZone.currentSystemDefault())
                base.copy(mode = SchedMode.ONCE, onceDate = ldt.date, timeText = fmtHhmm(ldt.hour, ldt.minute))
            } else {
                base
            }
        }
        "cron" -> {
            val f = s.split(Regex("\\s+"))
            val minute = f.getOrNull(0)?.toIntOrNull()
            val hour = f.getOrNull(1)?.toIntOrNull()
            if (f.size == 5 && minute != null && hour != null && f[2] == "*" && f[3] == "*") {
                if (f[4] == "*") {
                    base.copy(mode = SchedMode.DAILY, timeText = fmtHhmm(hour, minute))
                } else {
                    val days = parseDow(f[4])
                    if (days != null) {
                        base.copy(mode = SchedMode.WEEKLY, weekdays = days, timeText = fmtHhmm(hour, minute))
                    } else {
                        base
                    }
                }
            } else {
                base
            }
        }
        else -> base
    }
}

// buildScheduleSpec turns the draft back into the gateway's smart-schedule string, or a
// validation failure with a Korean message the form shows inline.
internal fun buildScheduleSpec(draft: ScheduleDraft, tz: TimeZone): Result<String> = when (draft.mode) {
    SchedMode.DAILY -> parseHhmm(draft.timeText)
        ?.let { Result.success("${it.second} ${it.first} * * *") }
        ?: Result.failure(IllegalArgumentException("시간 형식을 확인해 주세요 (예: 09:00)."))
    SchedMode.WEEKLY -> when {
        draft.weekdays.isEmpty() -> Result.failure(IllegalArgumentException("요일을 하나 이상 선택해 주세요."))
        else -> parseHhmm(draft.timeText)
            ?.let { Result.success("${it.second} ${it.first} * * ${draft.weekdays.sorted().joinToString(",")}") }
            ?: Result.failure(IllegalArgumentException("시간 형식을 확인해 주세요 (예: 09:00)."))
    }
    SchedMode.INTERVAL -> draft.intervalText.trim().toIntOrNull()?.takeIf { it > 0 }
        ?.let { Result.success("$it${if (draft.intervalUnit == IntervalUnit.HOUR) "h" else "m"}") }
        ?: Result.failure(IllegalArgumentException("주기를 숫자로 입력해 주세요."))
    SchedMode.ONCE -> parseHhmm(draft.timeText)
        ?.let { Result.success(LocalDateTime(draft.onceDate, LocalTime(it.first, it.second)).toInstant(tz).toString()) }
        ?: Result.failure(IllegalArgumentException("시간 형식을 확인해 주세요 (예: 09:00)."))
    SchedMode.ADVANCED -> draft.rawSpec.trim().ifEmpty { null }
        ?.let { Result.success(it) }
        ?: Result.failure(IllegalArgumentException("일정을 입력해 주세요."))
}

// parseDow reads a cron day-of-week list ("6", "1,3,5") into a 0..6 (Sun..Sat) set, or
// null when it isn't a plain comma list (so the caller falls back to ADVANCED).
private fun parseDow(s: String): Set<Int>? {
    val out = mutableSetOf<Int>()
    for (token in s.split(",")) {
        val v = token.trim().toIntOrNull() ?: return null
        if (v !in 0..7) return null
        out.add(if (v == 7) 0 else v) // cron allows 7 for Sunday
    }
    return out.ifEmpty { null }
}

private fun parseHhmm(s: String): Pair<Int, Int>? {
    val parts = s.trim().split(":")
    if (parts.size != 2) return null
    val h = parts[0].trim().toIntOrNull() ?: return null
    val m = parts[1].trim().toIntOrNull() ?: return null
    return if (h in 0..23 && m in 0..59) h to m else null
}

private fun fmtHhmm(h: Int, m: Int): String =
    "${h.toString().padStart(2, '0')}:${m.toString().padStart(2, '0')}"

private fun dateLabelKo(d: LocalDate): String = "${d.year}년 ${d.month.ordinal + 1}월 ${d.day}일"

private fun localDateToUtcMillis(d: LocalDate): Long =
    LocalDateTime(d, LocalTime(0, 0)).toInstant(TimeZone.UTC).toEpochMilliseconds()

private fun utcMillisToLocalDate(ms: Long): LocalDate =
    Instant.fromEpochMilliseconds(ms).toLocalDateTime(TimeZone.UTC).date
