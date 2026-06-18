package ai.deneb.deneb

import ai.deneb.ui.DenebRow
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebPressable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Checkbox
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.graphics.vector.ImageVector
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
    var actionTodo by remember { mutableStateOf<Todo?>(null) }
    var confirmDelete by remember { mutableStateOf<Todo?>(null) }
    var actionError by remember { mutableStateOf<String?>(null) }
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

    fun delete(todo: Todo) {
        val previous = todos
        todos = todos.filterNot { it.id == todo.id }
        scope.launch {
            val err = client.deleteTodo(todo.id)
            if (err == null) {
                load()
            } else {
                todos = previous
                actionError = err
            }
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
                            "할 일을 불러오지 못했습니다.",
                            onRetry = {
                                scope.launch {
                                    loadOk = null
                                    load()
                                }
                            },
                        )

                        todos.isEmpty() -> DenebEmpty("할 일이 없습니다.", actionLabel = "할 일 추가", onAction = onAddTodo)

                        else -> TodoListContent(
                            todos,
                            onToggle = ::toggle,
                            onOpen = onOpenTodo,
                            onAction = { todo ->
                                actionTodo = todo
                            },
                        )
                    }
                    Spacer(Modifier.height(24.dp))
                }
            }
        }
    }

    actionTodo?.let { todo ->
        ModalBottomSheet(onDismissRequest = { actionTodo = null }) {
            TodoActionSheetContent(
                todo = todo,
                onOpen = {
                    actionTodo = null
                    onOpenTodo(todo.id)
                },
                onToggle = {
                    actionTodo = null
                    toggle(todo.id, !todo.done)
                },
                onDelete = {
                    actionTodo = null
                    confirmDelete = todo
                },
            )
        }
    }

    confirmDelete?.let { todo ->
        AlertDialog(
            onDismissRequest = { confirmDelete = null },
            title = { Text("할 일 삭제") },
            text = { Text("'${todo.title.ifBlank { "제목 없음" }}' 할 일을 삭제할까요? 되돌릴 수 없습니다.") },
            confirmButton = {
                TextButton(onClick = {
                    confirmDelete = null
                    delete(todo)
                }) {
                    Text("삭제", color = MaterialTheme.colorScheme.error)
                }
            },
            dismissButton = {
                TextButton(onClick = { confirmDelete = null }) { Text("취소") }
            },
        )
    }

    actionError?.let { msg ->
        AlertDialog(
            onDismissRequest = { actionError = null },
            title = { Text("작업 실패") },
            text = { Text(msg) },
            confirmButton = {
                TextButton(onClick = { actionError = null }) { Text("확인") }
            },
        )
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
    onAction: ((Todo) -> Unit)? = null,
) {
    val tz = remember { TimeZone.currentSystemDefault() }
    val active = todos.filter { !it.done }
    val done = todos.filter { it.done }
    Column(Modifier.fillMaxWidth()) {
        if (active.isNotEmpty()) {
            DenebSectionLabel("할 일")
            active.forEach { TodoCheckRow(it, tz, onToggle, onOpen, onAction) }
        }
        if (done.isNotEmpty()) {
            DenebSectionLabel("완료")
            done.forEach { TodoCheckRow(it, tz, onToggle, onOpen, onAction) }
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
    onLongAction: ((Todo) -> Unit)? = null,
) {
    val haptics = rememberHaptics()
    val due = todoDueLabel(todo.due, todo.dueAllDay, tz)
    DenebRow(
        onClick = {
            haptics.tap()
            onOpen(todo.id)
        },
        onLongClick = onLongAction?.let {
            {
                haptics.longPress()
                it(todo)
            }
        },
    ) {
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
                // Active items take the emphasized row-title (SemiBold); completed
                // items relax to the plain row-title + hint color so the open work
                // reads first. (Named row tokens, not body+manual weight — law 3.)
                Text(
                    todo.title.ifBlank { "(제목 없음)" },
                    style = if (todo.done) DenebType.rowTitle else DenebType.rowTitleStrong,
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

@Composable
private fun TodoActionSheetContent(
    todo: Todo,
    onOpen: () -> Unit,
    onToggle: () -> Unit,
    onDelete: () -> Unit,
) {
    Column(Modifier.fillMaxWidth().padding(bottom = 24.dp)) {
        Text(
            todo.title.ifBlank { "(제목 없음)" },
            style = DenebType.subject,
            color = MaterialTheme.colorScheme.onBackground,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.padding(horizontal = 24.dp, vertical = 12.dp),
        )
        HorizontalDivider(color = denebHairline())
        TodoSheetAction(Icons.Outlined.Edit, "편집", onOpen = onOpen)
        TodoSheetAction(
            Icons.Filled.CheckCircle,
            if (todo.done) "미완료로 변경" else "완료로 변경",
            onOpen = onToggle,
        )
        TodoSheetAction(Icons.Outlined.Delete, "삭제", destructive = true, onOpen = onDelete)
    }
}

@Composable
private fun TodoSheetAction(
    icon: ImageVector,
    label: String,
    destructive: Boolean = false,
    onOpen: () -> Unit,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .denebPressable(onClick = onOpen)
            .padding(horizontal = 24.dp, vertical = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        val color = if (destructive) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.primary
        Icon(icon, contentDescription = null, tint = color, modifier = Modifier.size(22.dp))
        Spacer(Modifier.width(16.dp))
        Text(label, style = DenebType.rowTitle, color = if (destructive) color else MaterialTheme.colorScheme.onBackground)
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
