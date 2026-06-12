package ai.deneb.deneb

import ai.deneb.ui.DenebRow
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Checkbox
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
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
 * To-do list (`miniapp.todo.*`): the task-list companion to the calendar. Active
 * items first (with a checkbox to complete inline), completed items below. A "추가"
 * button opens the add screen; tapping a row opens it for editing. Pull to refresh
 * re-fetches.
 *
 * Design split (see .claude/rules/native-design-system.md): frame + type are the
 * Deneb skin (DenebScreenScaffold + DenebType + DenebRow); the checkbox and button
 * are Material. The list is a stateless body ([TodoListContent]) the render harness
 * previews with mock data; this composable is the stateful shell (fetch + toggle +
 * loading/error states).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DenebTodoScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onAddTodo: () -> Unit = {},
    onOpenTodo: (String) -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var todos by remember { mutableStateOf<List<Todo>>(emptyList()) }
    // null = load in flight, true = ok, false = fetch failed.
    var loadOk by remember { mutableStateOf<Boolean?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    suspend fun load() {
        val fetched = client.fetchTodos()
        if (fetched == null) {
            loadOk = false
        } else {
            todos = fetched
            loadOk = true
        }
    }
    LaunchedEffect(Unit) { load() }

    // Optimistic toggle: flip locally so the checkbox responds instantly, then
    // persist and re-fetch to settle ordering (completed items sink to the bottom).
    fun toggle(id: String, done: Boolean) {
        todos = todos.map { if (it.id == id) it.copy(done = done) else it }
        scope.launch {
            client.setTodoDone(id, done)
            load()
        }
    }

    DenebScreenScaffold(title = "할 일", onBack = onBack, tabBar = navigationTabBar) {
        Column(Modifier.fillMaxWidth().weight(1f).padding(horizontal = 24.dp)) {
            Row(Modifier.fillMaxWidth().padding(top = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                val openCount = todos.count { !it.done }
                Text(
                    if (openCount > 0) "진행 중 ${openCount}건" else "모두 완료",
                    style = DenebType.sectionLabel,
                    color = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.weight(1f),
                )
                FilledTonalButton(
                    onClick = onAddTodo,
                    contentPadding = PaddingValues(horizontal = 14.dp, vertical = 8.dp),
                ) { Text("추가", maxLines = 1, softWrap = false) }
            }
            Spacer(Modifier.height(4.dp))
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
                        loadOk == null && todos.isEmpty() -> DenebLoading()

                        loadOk == false && todos.isEmpty() -> DenebError(
                            "할 일을 불러오지 못했어요.",
                            onRetry = {
                                scope.launch {
                                    loadOk = null
                                    load()
                                }
                            },
                        )

                        todos.isEmpty() -> DenebEmpty("할 일이 없어요.", actionLabel = "할 일 추가", onAction = onAddTodo)

                        else -> TodoListContent(todos, onToggle = ::toggle, onOpen = onOpenTodo)
                    }
                    Spacer(Modifier.height(24.dp))
                }
            }
        }
    }
}

// --- stateless body (previewable) ----------------------------------------

/** The to-do list: active items under "할 일", completed under "완료". Each row is a
 *  Material checkbox + the title (struck through when done) + a due/note line. Pure
 *  presentation — the shell owns fetch + toggle. */
@Composable
internal fun TodoListContent(
    todos: List<Todo>,
    onToggle: (String, Boolean) -> Unit,
    onOpen: (String) -> Unit,
) {
    val tz = remember { TimeZone.currentSystemDefault() }
    val active = todos.filter { !it.done }
    val done = todos.filter { it.done }
    Column(Modifier.fillMaxWidth()) {
        if (active.isNotEmpty()) {
            DenebSectionLabel("할 일")
            active.forEach { TodoCheckRow(it, tz, onToggle, onOpen) }
        }
        if (done.isNotEmpty()) {
            DenebSectionLabel("완료")
            done.forEach { TodoCheckRow(it, tz, onToggle, onOpen) }
        }
    }
}

/** A single to-do row: Material checkbox + title (struck through when done) + a
 *  due/note meta line. Shared by the to-do list and the calendar day section. */
@Composable
internal fun TodoCheckRow(
    todo: Todo,
    tz: TimeZone,
    onToggle: (String, Boolean) -> Unit,
    onOpen: (String) -> Unit,
) {
    val haptics = rememberHaptics()
    val due = todoDueLabel(todo.due, todo.dueAllDay, tz)
    DenebRow(onClick = {
        haptics.tap()
        onOpen(todo.id)
    }) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.Top) {
            Checkbox(
                checked = todo.done,
                onCheckedChange = {
                    haptics.toggle(it)
                    onToggle(todo.id, it)
                },
            )
            Spacer(Modifier.width(4.dp))
            Column(Modifier.weight(1f).padding(top = 12.dp)) {
                Text(
                    todo.title.ifBlank { "(제목 없음)" },
                    style = DenebType.body,
                    fontWeight = FontWeight.SemiBold,
                    color = if (todo.done) denebHint() else MaterialTheme.colorScheme.onSurface,
                    textDecoration = if (todo.done) TextDecoration.LineThrough else TextDecoration.None,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
                val meta = listOfNotNull(due, todo.note.ifBlank { null }).joinToString(" · ")
                if (meta.isNotBlank()) {
                    Text(
                        meta,
                        style = DenebType.meta,
                        color = if (isOverdue(todo, tz)) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
        }
    }
}

// --- helpers --------------------------------------------------------------

/** A short Korean due label: 오늘/내일/M월 D일, plus HH:mm for a timed due. Returns
 *  null when the to-do has no due date (so the meta line omits it). */
internal fun todoDueLabel(dueIso: String, allDay: Boolean, tz: TimeZone): String? {
    if (dueIso.isBlank()) return null
    val local = runCatching { Instant.parse(dueIso).toLocalDateTime(tz) }.getOrNull() ?: return null
    val today = Clock.System.todayIn(tz)
    val month = local.month.ordinal + 1
    val dow = koreanDayOfWeek.getOrElse(local.dayOfWeek.ordinal) { "" }
    val dayLabel = when (local.date) {
        today -> "오늘"
        today.plus(1, DateTimeUnit.DAY) -> "내일"
        else -> "${month}월 ${local.day}일 ($dow)"
    }
    if (allDay) return dayLabel
    val hh = local.hour.toString().padStart(2, '0')
    val mm = local.minute.toString().padStart(2, '0')
    return "$dayLabel $hh:$mm"
}

/** True when an incomplete to-do's due instant is in the past — used to tint the
 *  due line red so overdue tasks stand out. */
private fun isOverdue(todo: Todo, tz: TimeZone): Boolean {
    if (todo.done || todo.due.isBlank()) return false
    val due = runCatching { Instant.parse(todo.due) }.getOrNull() ?: return false
    if (todo.dueAllDay) {
        // An all-day to-do is overdue only once its day has fully passed.
        val dueDate = due.toLocalDateTime(tz).date
        return dueDate < Clock.System.todayIn(tz)
    }
    return due < Clock.System.now()
}
