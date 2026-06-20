package ai.deneb.deneb

import ai.deneb.deneb.generated.CalendarEventOut
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/**
 * Calendar + to-do surface of [DenebGatewayClient] (`miniapp.calendar.*`,
 * `miniapp.todo.*`) plus the home-screen widget summary built from both.
 * Extensions so the gateway client stays one facade while each RPC domain
 * lives in its own file.
 */

/** Refresh upcoming events. Returns false on a fetch failure so the screen can
 *  tell a real "no events" from a network error instead of spinning forever. */
suspend fun DenebGatewayClient.refreshCalendar(): Boolean {
    val payload = callRpc<CalListPayload>(
        "miniapp.calendar.list_upcoming",
        buildJsonObject {
            put("hoursAhead", 168) // one week ahead
            put("limit", 50)
        },
    ) ?: return false
    _denebCalendar.value = payload.events
        .filter { it.id.isNotBlank() }
        .map { CalendarEvent(it.id, it.summary, it.location, it.start, it.end, it.allDay, it.local, it.category) }
    storeCachedCalendar(_denebCalendar.value)
    return true
}

/**
 * Fetch events in an explicit [fromIso, toIso) window (`miniapp.calendar.list_range`).
 * The month grid uses this because it needs a whole month — often reaching into the
 * past — rather than [refreshCalendar]'s now-anchored look-ahead. Returns null on a
 * fetch failure so the screen can tell a real "no events" from a network error.
 *
 * Served from a short-lived client cache so re-opening the calendar (a new screen
 * composition on every tab switch) doesn't re-hit Google for the same months;
 * [force] (pull-to-refresh, after an add/edit) bypasses the cache and refreshes it.
 */
suspend fun DenebGatewayClient.fetchCalendarRange(
    fromIso: String,
    toIso: String,
    force: Boolean = false,
): List<CalendarEvent>? {
    val key = "$fromIso|$toIso"
    if (!force) cachedCalendarRange(key)?.let { return it }
    val payload = callRpc<CalListPayload>(
        "miniapp.calendar.list_range",
        buildJsonObject {
            put("from", fromIso)
            put("to", toIso)
        },
    ) ?: return null
    val events = payload.events
        .filter { it.id.isNotBlank() }
        .map { CalendarEvent(it.id, it.summary, it.location, it.start, it.end, it.allDay, it.local, it.category) }
    storeCalendarRange(key, events)
    return events
}

/**
 * Create a calendar event by hand (`miniapp.calendar.create`). The gateway
 * stores it locally, so this always works without a Google write scope.
 * Returns null on success, or a Korean error message on failure. start/end
 * are RFC3339; pass end blank to let the gateway apply a default duration.
 */
suspend fun DenebGatewayClient.createCalendarEvent(
    summary: String,
    description: String,
    location: String,
    allDay: Boolean,
    startIso: String,
    endIso: String,
    timeZone: String,
): String? = rpcWrite(
    "miniapp.calendar.create",
    calendarWriteParams(summary, description, location, allDay, startIso, endIso, timeZone),
)

/** Edit a locally-stored event (`miniapp.calendar.update`). Same return contract
 *  as [createCalendarEvent]; the gateway rejects non-local (Google) IDs. */
suspend fun DenebGatewayClient.updateCalendarEvent(
    id: String,
    summary: String,
    description: String,
    location: String,
    allDay: Boolean,
    startIso: String,
    endIso: String,
    timeZone: String,
): String? = rpcWrite(
    "miniapp.calendar.update",
    calendarWriteParams(summary, description, location, allDay, startIso, endIso, timeZone, id),
)

/** Delete a locally-stored event (`miniapp.calendar.delete`). Null on success,
 *  a Korean error message otherwise (e.g. when the id is a read-only Google event). */
suspend fun DenebGatewayClient.deleteCalendarEvent(id: String): String? = rpcWrite("miniapp.calendar.delete", buildJsonObject { put("id", id) })

/** Refresh the pending calendar proposals (the bell). Returns false on a fetch
 *  failure so the screen can tell a real "no proposals" from a network error. */
suspend fun DenebGatewayClient.refreshCalendarProposals(): Boolean {
    val payload = callRpc<CalProposalsPayload>(
        "miniapp.calendar.proposals.list",
        buildJsonObject {},
    ) ?: return false
    _denebCalProposals.value = payload.proposals.filter { it.id.isNotBlank() }
    return true
}

/** Accept a proposal — the gateway creates a local event and marks it accepted.
 *  Refreshes the proposal list on success. Null on success, Korean error otherwise. */
suspend fun DenebGatewayClient.acceptCalendarProposal(id: String): String? {
    val err = rpcWrite("miniapp.calendar.proposals.accept", buildJsonObject { put("id", id) })
    if (err == null) refreshCalendarProposals()
    return err
}

/** Reject (dismiss) a proposal so it is never re-proposed. Refreshes the list. */
suspend fun DenebGatewayClient.rejectCalendarProposal(id: String): String? {
    val err = rpcWrite("miniapp.calendar.proposals.reject", buildJsonObject { put("id", id) })
    if (err == null) refreshCalendarProposals()
    return err
}

/** Full calendar event (attendees, Meet link, description) for the detail screen. */
suspend fun DenebGatewayClient.fetchCalendarEvent(id: String): CalendarEventDetail? {
    val p = callRpc<CalendarEventOut>(
        "miniapp.calendar.get",
        buildJsonObject { put("id", id) },
    ) ?: return null
    return CalendarEventDetail(
        id = p.id,
        title = p.summary,
        description = p.description,
        location = p.location,
        start = p.start,
        end = p.end,
        allDay = p.allDay,
        organizer = p.organizer?.let { it.displayName.ifBlank { it.email } }.orEmpty(),
        attendees = p.attendees.mapNotNull { (it.displayName.ifBlank { it.email }).ifBlank { null } },
        status = p.status,
        local = p.local,
    )
}

// --- to-dos (miniapp.todo.*) -----------------------------------------

/**
 * Fetch all to-dos (`miniapp.todo.list`). Returns null on a fetch failure so
 * the screen can tell a real "no to-dos" from a network error. Completed items
 * are included; the screen decides how to present them.
 */
suspend fun DenebGatewayClient.fetchTodos(): List<Todo>? {
    val payload = callRpc<TodoListPayload>("miniapp.todo.list", buildJsonObject {}) ?: return null
    return payload.todos
        .filter { it.id.isNotBlank() }
        .map { Todo(it.id, it.title, it.note, it.due, it.dueAllDay, it.done) }
}

/**
 * Create a to-do (`miniapp.todo.create`). Returns null on success or a Korean
 * error message on failure. Pass dueIso blank for an undated to-do.
 */
suspend fun DenebGatewayClient.createTodo(
    title: String,
    note: String,
    dueIso: String,
    dueAllDay: Boolean,
): String? = rpcWrite("miniapp.todo.create", todoWriteParams(title, note, dueIso, dueAllDay))

/** Edit a to-do (`miniapp.todo.update`). Same return contract as [createTodo];
 *  completion state is preserved server-side. */
suspend fun DenebGatewayClient.updateTodo(
    id: String,
    title: String,
    note: String,
    dueIso: String,
    dueAllDay: Boolean,
): String? = rpcWrite("miniapp.todo.update", todoWriteParams(title, note, dueIso, dueAllDay, id))

/** Flip a to-do's completion (`miniapp.todo.set_done`). Null on success. */
suspend fun DenebGatewayClient.setTodoDone(id: String, done: Boolean): String? = rpcWrite(
    "miniapp.todo.set_done",
    buildJsonObject {
        put("id", id)
        put("done", done)
    },
)

/** Delete a to-do (`miniapp.todo.delete`). Null on success. */
suspend fun DenebGatewayClient.deleteTodo(id: String): String? = rpcWrite("miniapp.todo.delete", buildJsonObject { put("id", id) })

// todoWriteParams builds the shared create/update body; `id` is set only for
// updates. A blank due is omitted so the gateway stores an undated to-do.
private fun todoWriteParams(
    title: String,
    note: String,
    dueIso: String,
    dueAllDay: Boolean,
    id: String? = null,
): JsonObject = buildJsonObject {
    if (id != null) put("id", id)
    put("title", title)
    if (note.isNotBlank()) put("note", note)
    if (dueIso.isNotBlank()) {
        put("due", dueIso)
        put("dueAllDay", dueAllDay)
    }
}

// calendarWriteParams builds the shared create/update body; `id` is set only
// for updates. Blank optional fields are omitted so the gateway applies defaults.
private fun calendarWriteParams(
    summary: String,
    description: String,
    location: String,
    allDay: Boolean,
    startIso: String,
    endIso: String,
    timeZone: String,
    id: String? = null,
): JsonObject = buildJsonObject {
    if (id != null) put("id", id)
    put("summary", summary)
    if (description.isNotBlank()) put("description", description)
    if (location.isNotBlank()) put("location", location)
    put("allDay", allDay)
    put("start", startIso)
    if (endIso.isNotBlank()) put("end", endIso)
    if (timeZone.isNotBlank()) put("timeZone", timeZone)
}

/**
 * One-shot glanceable summary for the home-screen widget: the next upcoming
 * event and the unread-mail count. Returns a not-configured summary when the
 * gateway token is unset, and ok=false on a fetch error so the widget shows a
 * quiet fallback instead of stale data.
 */
suspend fun DenebGatewayClient.widgetSummary(): WidgetSummary {
    if (clientToken.isEmpty() || gatewayUrl.isBlank()) {
        return WidgetSummary(configured = false)
    }
    return runCatching {
        // Calendar and mail are independent — fetch them concurrently so the
        // widget refresh costs one RTT instead of the sum of two.
        coroutineScope {
            val calDeferred = async {
                callRpc<CalListPayload>(
                    "miniapp.calendar.list_upcoming",
                    buildJsonObject {
                        put("hoursAhead", 168)
                        put("limit", 5)
                    },
                )
            }
            val mailDeferred = async {
                callRpc<MailListPayload>(
                    "miniapp.gmail.list_recent",
                    buildJsonObject { put("limit", 25) },
                )
            }
            val cal = calDeferred.await()
            val mail = mailDeferred.await()
            val next = cal?.events?.firstOrNull { it.id.isNotBlank() }
            val meeting = next?.let { formatMeeting(it.summary, it.start, it.allDay) }.orEmpty()
            val msgs = mail?.messages.orEmpty()
            val unread = msgs.count { it.isUnread }
            // The most recent message (read or unread) as a one-line glance.
            val latestMail = msgs.firstOrNull { it.id.isNotBlank() }
                ?.let { mailGlance(it.from, it.subject) }.orEmpty()
            WidgetSummary(meeting = meeting, unread = unread, latestMail = latestMail)
        }
    }.getOrElse { WidgetSummary(ok = false) }
}

// mailGlance renders "sender · subject" for the widget's recent-mail line.
// Sender is the display name before any <email>; subject falls back to a
// placeholder so the line is never just a bare name. The widget layout already
// ellipsizes at one line; the code-side cap keeps a spammy 500-char subject off
// the RemoteViews binder anyway (dropping a trailing high surrogate so an emoji
// is never split in half).
private fun mailGlance(from: String, subject: String): String {
    val lt = from.indexOf('<')
    val name = (if (lt > 0) from.take(lt) else from).trim().trim('"').ifBlank { from.trim() }
    val subj = subject.trim().ifBlank { "(제목 없음)" }
    val line = "$name · $subj"
    if (line.length <= 100) return line
    return line.take(100).dropLastWhile { it.isHighSurrogate() } + "…"
}

// formatMeeting renders "M/D HH:mm · title" from an RFC3339 start using only
// string ops, to keep this widget hot-path free of a date-library dependency.
private fun formatMeeting(title: String, start: String, allDay: Boolean): String {
    val t = title.trim().ifBlank { "일정" }
    val md = runCatching {
        val parts = start.take(10).split("-") // 2026-05-31
        "${parts[1].toInt()}/${parts[2].toInt()}"
    }.getOrDefault("")
    val hm = if (!allDay && start.length >= 16 && start[10] == 'T') start.substring(11, 16) else ""
    val whenStr = listOf(md, hm).filter { it.isNotBlank() }.joinToString(" ")
    return if (whenStr.isBlank()) t else "$whenStr · $t"
}
