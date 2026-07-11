package ai.deneb.deneb

import ai.deneb.deneb.generated.CalendarAttendeeOut
import ai.deneb.deneb.generated.CalendarEventOut
import ai.deneb.deneb.generated.CalendarProposalOut
import ai.deneb.deneb.generated.MailRowOut
import ai.deneb.deneb.generated.TodoOut
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class CalendarTodoWidgetBoundaryTest {

    private val json = Json { encodeDefaults = true }

    private fun event(
        id: String,
        summary: String = "Event $id",
        start: String = "2026-07-12T09:30:00+09:00",
        end: String = "2026-07-12T10:30:00+09:00",
        allDay: Boolean = false,
        local: Boolean = false,
        category: String = "work",
    ) = CalendarEventOut(
        id = id,
        summary = summary,
        description = "description-$id",
        location = "location-$id",
        start = start,
        end = end,
        allDay = allDay,
        local = local,
        category = category,
    )

    private fun calendarPayload(vararg events: CalendarEventOut): String = json.encodeToString(CalListPayload(events.toList()))

    private fun todo(
        id: String,
        title: String = "Todo $id",
        note: String = "note-$id",
        due: String = "2026-07-12",
        dueAllDay: Boolean = true,
        done: Boolean = false,
    ) = TodoOut(id = id, title = title, note = note, due = due, dueAllDay = dueAllDay, done = done)

    private fun todoPayload(vararg todos: TodoOut): String = json.encodeToString(TodoListPayload(todos.toList()))

    @Test
    fun upcomingRefreshUsesOneWeekAndFiftyRowBounds() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(calendarPayload())

        assertTrue(f.client.refreshCalendar())

        val params = f.transport.singleRequest().requireRpc("miniapp.calendar.list_upcoming")
        assertEquals(168, params["hoursAhead"]?.jsonPrimitive?.content?.toInt())
        assertEquals(50, params["limit"]?.jsonPrimitive?.content?.toInt())
    }

    @Test
    fun upcomingRefreshFiltersBlankAndDeduplicatesEventIds() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            calendarPayload(
                event(""),
                event("one", summary = "first"),
                event("one", summary = "latest"),
                event("two"),
            ),
        )

        f.client.refreshCalendar()

        assertEquals(listOf("one", "two"), f.client.denebCalendar.value.map { it.id })
        assertEquals("latest", f.client.denebCalendar.value.first().title)
    }

    @Test
    fun upcomingRefreshMapsEveryVisibleEventField() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            calendarPayload(
                event(
                    id = "one",
                    summary = "Board meeting",
                    start = "start",
                    end = "end",
                    allDay = true,
                    local = true,
                    category = "deal",
                ),
            ),
        )

        f.client.refreshCalendar()
        val mapped = f.client.denebCalendar.value.single()

        assertEquals("one", mapped.id)
        assertEquals("Board meeting", mapped.title)
        assertEquals("location-one", mapped.location)
        assertEquals("start", mapped.start)
        assertEquals("end", mapped.end)
        assertTrue(mapped.allDay)
        assertTrue(mapped.local)
        assertEquals("deal", mapped.category)
    }

    @Test
    fun failedUpcomingRefreshPreservesLastKnownCalendar() = runTest {
        val f = gatewayClientFixture()
        f.client._denebCalendar.value = listOf(
            CalendarEvent(
                id = "cached",
                title = "Cached",
                location = "",
                start = "",
                end = "",
                allDay = false,
            ),
        )
        f.transport.enqueueJson("bad")

        assertFalse(f.client.refreshCalendar())

        assertEquals("cached", f.client.denebCalendar.value.single().id)
    }

    @Test
    fun calendarRangeSendsExactHalfOpenBounds() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(calendarPayload(event("one")))

        val result = f.client.fetchCalendarRange("2026-07-01", "2026-08-01")

        assertEquals(listOf("one"), result?.map { it.id })
        val params = f.transport.singleRequest().requireRpc("miniapp.calendar.list_range")
        assertEquals("2026-07-01", params["from"]?.jsonPrimitive?.content)
        assertEquals("2026-08-01", params["to"]?.jsonPrimitive?.content)
    }

    @Test
    fun repeatedCalendarRangeUsesInMemoryCache() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(calendarPayload(event("one")))

        val first = f.client.fetchCalendarRange("from", "to")
        val second = f.client.fetchCalendarRange("from", "to")

        assertEquals(first, second)
        assertEquals(1, f.transport.requests.size)
    }

    @Test
    fun forcedCalendarRangeBypassesCacheAndReplacesIt() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(calendarPayload(event("old")))
        f.transport.enqueueRpc(calendarPayload(event("new")))

        assertEquals("old", f.client.fetchCalendarRange("from", "to")?.single()?.id)
        assertEquals("new", f.client.fetchCalendarRange("from", "to", force = true)?.single()?.id)
        assertEquals("new", f.client.fetchCalendarRange("from", "to")?.single()?.id)

        assertEquals(2, f.transport.requests.size)
    }

    @Test
    fun failedRangeDoesNotPoisonCacheWithEmptyValue() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("bad")
        f.transport.enqueueRpc(calendarPayload(event("recovered")))

        assertNull(f.client.fetchCalendarRange("from", "to"))
        assertEquals("recovered", f.client.fetchCalendarRange("from", "to")?.single()?.id)
        assertEquals(2, f.transport.requests.size)
    }

    @Test
    fun createCalendarEventOmitsBlankOptionalFields() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)

        assertNull(
            f.client.createCalendarEvent(
                summary = "Meeting",
                description = " ",
                location = "",
                allDay = false,
                startIso = "start",
                endIso = "",
                timeZone = " ",
            ),
        )

        val params = f.transport.singleRequest().requireRpc("miniapp.calendar.create")
        assertEquals(setOf("summary", "allDay", "start"), params.keys)
    }

    @Test
    fun createCalendarEventIncludesEveryNonBlankField() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)

        f.client.createCalendarEvent("Meeting", "Desc", "Room", true, "start", "end", "Asia/Seoul")

        val params = f.transport.singleRequest().rpcParams.orEmpty()
        assertEquals("Meeting", params["summary"]?.jsonPrimitive?.content)
        assertEquals("Desc", params["description"]?.jsonPrimitive?.content)
        assertEquals("Room", params["location"]?.jsonPrimitive?.content)
        assertEquals(true, params["allDay"]?.jsonPrimitive?.content?.toBoolean())
        assertEquals("start", params["start"]?.jsonPrimitive?.content)
        assertEquals("end", params["end"]?.jsonPrimitive?.content)
        assertEquals("Asia/Seoul", params["timeZone"]?.jsonPrimitive?.content)
    }

    @Test
    fun updateCalendarAddsIdWhileSharingCreateFieldContract() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)

        f.client.updateCalendarEvent("event-id", "Meeting", "", "", false, "start", "", "")

        val params = f.transport.singleRequest().requireRpc("miniapp.calendar.update")
        assertEquals("event-id", params["id"]?.jsonPrimitive?.content)
        assertEquals("Meeting", params["summary"]?.jsonPrimitive?.content)
    }

    @Test
    fun deleteCalendarSendsIdentityThroughErrorBearingWrite() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = false, message = "read-only Google event")

        val error = f.client.deleteCalendarEvent("google-id")

        assertEquals("read-only Google event", error)
        assertEquals("google-id", f.transport.singleRequest().rpcParams?.get("id")?.jsonPrimitive?.content)
    }

    @Test
    fun proposalsFilterBlankAndDeduplicateIdentity() = runTest {
        val f = gatewayClientFixture()
        val proposals = CalProposalsPayload(
            listOf(
                CalendarProposalOut(id = ""),
                CalendarProposalOut(id = "one", title = "old"),
                CalendarProposalOut(id = "one", title = "new"),
                CalendarProposalOut(id = "two"),
            ),
        )
        f.transport.enqueueRpc(json.encodeToString(proposals))

        assertTrue(f.client.refreshCalendarProposals())

        assertEquals(listOf("one", "two"), f.client.denebCalProposals.value.map { it.id })
        assertEquals("new", f.client.denebCalProposals.value.first().title)
    }

    @Test
    fun failedProposalRefreshPreservesSnapshot() = runTest {
        val f = gatewayClientFixture()
        f.client._denebCalProposals.value = listOf(CalendarProposalOut(id = "cached"))
        f.transport.enqueueJson("bad")

        assertFalse(f.client.refreshCalendarProposals())

        assertEquals("cached", f.client.denebCalProposals.value.single().id)
    }

    @Test
    fun acceptedProposalRefreshesListOnlyOnSuccess() = runTest {
        val success = gatewayClientFixture()
        success.transport.enqueueWrite(ok = true)
        success.transport.enqueueRpc(json.encodeToString(CalProposalsPayload()))
        assertNull(success.client.acceptCalendarProposal("one"))
        assertEquals(listOf("miniapp.calendar.proposals.accept", "miniapp.calendar.proposals.list"), success.transport.requestMethods())

        val failure = gatewayClientFixture()
        failure.transport.enqueueWrite(ok = false, message = "stale")
        assertEquals("stale", failure.client.acceptCalendarProposal("one"))
        assertEquals(1, failure.transport.requests.size)
    }

    @Test
    fun rejectedProposalRefreshesListOnlyOnSuccess() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)
        f.transport.enqueueRpc(json.encodeToString(CalProposalsPayload()))

        assertNull(f.client.rejectCalendarProposal("one"))

        assertEquals("one", f.transport.requests.first().rpcParams?.get("id")?.jsonPrimitive?.content)
        assertEquals(2, f.transport.requests.size)
    }

    @Test
    fun eventDetailUsesOrganizerNameThenEmailAndFiltersBlankAttendees() = runTest {
        val f = gatewayClientFixture()
        val wire = event("one").copy(
            organizer = CalendarAttendeeOut(email = "owner@example.com", displayName = "Owner"),
            attendees = listOf(
                CalendarAttendeeOut(email = "a@example.com", displayName = "Alice"),
                CalendarAttendeeOut(email = "b@example.com"),
                CalendarAttendeeOut(),
            ),
            status = "confirmed",
        )
        f.transport.enqueueRpc(json.encodeToString(wire))

        val detail = f.client.fetchCalendarEvent("one")

        assertEquals("Owner", detail?.organizer)
        assertEquals(listOf("Alice", "b@example.com"), detail?.attendees)
        assertEquals("confirmed", detail?.status)
        assertEquals("one", f.transport.singleRequest().rpcParams?.get("id")?.jsonPrimitive?.content)
    }

    @Test
    fun eventOrganizerFallsBackToEmailAndThenBlank() = runTest {
        val email = gatewayClientFixture()
        email.transport.enqueueRpc(json.encodeToString(event("one").copy(organizer = CalendarAttendeeOut(email = "owner@example.com"))))
        assertEquals("owner@example.com", email.client.fetchCalendarEvent("one")?.organizer)

        val none = gatewayClientFixture()
        none.transport.enqueueRpc(json.encodeToString(event("one")))
        assertEquals("", none.client.fetchCalendarEvent("one")?.organizer)
    }

    @Test
    fun todosFilterBlankAndDeduplicateIdentity() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(todoPayload(todo(""), todo("one", title = "old"), todo("one", title = "new"), todo("two")))

        val result = f.client.fetchTodos().orEmpty()

        assertEquals(listOf("one", "two"), result.map { it.id })
        assertEquals("new", result.first().title)
    }

    @Test
    fun todoMappingPreservesDueAndCompletionFields() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(todoPayload(todo("one", due = "2026-08-01", dueAllDay = false, done = true)))

        val result = f.client.fetchTodos().orEmpty().single()

        assertEquals("note-one", result.note)
        assertEquals("2026-08-01", result.due)
        assertFalse(result.dueAllDay)
        assertTrue(result.done)
    }

    @Test
    fun createTodoOmitsBlankNoteAndDuePair() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)

        f.client.createTodo("Title", " ", "", dueAllDay = true)

        val params = f.transport.singleRequest().requireRpc("miniapp.todo.create")
        assertEquals(setOf("title"), params.keys)
    }

    @Test
    fun updateTodoIncludesIdAndNonBlankOptionalFields() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)

        f.client.updateTodo("one", "Title", "Note", "2026-08-01", dueAllDay = false)

        val params = f.transport.singleRequest().requireRpc("miniapp.todo.update")
        assertEquals("one", params["id"]?.jsonPrimitive?.content)
        assertEquals("Note", params["note"]?.jsonPrimitive?.content)
        assertEquals("2026-08-01", params["due"]?.jsonPrimitive?.content)
        assertEquals(false, params["dueAllDay"]?.jsonPrimitive?.content?.toBoolean())
    }

    @Test
    fun todoDoneAndDeleteUseDistinctWriteMethods() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)
        f.transport.enqueueWrite(ok = true)

        assertNull(f.client.setTodoDone("one", true))
        assertNull(f.client.deleteTodo("one"))

        assertEquals(listOf("miniapp.todo.set_done", "miniapp.todo.delete"), f.transport.requestMethods())
        assertEquals(true, f.transport.requests.first().rpcParams?.get("done")?.jsonPrimitive?.content?.toBoolean())
    }

    @Test
    fun widgetWithoutTokenReturnsUnconfiguredWithoutNetwork() = runTest {
        val f = gatewayClientFixture(token = "")

        val result = f.client.widgetSummary()

        assertFalse(result.configured)
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun widgetCombinesNextMeetingUnreadCountAndLatestMail() = runTest {
        val f = gatewayClientFixture()
        val cal = calendarPayload(event("one", summary = "Deal review", start = "2026-07-12T09:30:00+09:00"))
        val mail = json.encodeToString(
            MailListPayload(
                messages = listOf(
                    MailRowOut(id = "m1", from = "\"Alice\" <alice@example.com>", subject = "Contract", isUnread = false),
                    MailRowOut(id = "m2", from = "Bob", subject = "Other", isUnread = true),
                    MailRowOut(id = "m3", from = "Carol", subject = "Third", isUnread = true),
                ),
            ),
        )
        f.transport.fallback = { request ->
            when (request.rpcMethod) {
                "miniapp.calendar.list_upcoming" -> gatewayRpcReply(cal)
                "miniapp.mail.list_recent" -> gatewayRpcReply(mail)
                else -> error("Unexpected ${request.rpcMethod}")
            }
        }

        val result = f.client.widgetSummary()

        assertTrue(result.ok)
        assertTrue(result.configured)
        assertEquals("7/12 09:30 · Deal review", result.meeting)
        assertEquals(2, result.unread)
        assertEquals("Alice · Contract", result.latestMail)
    }

    @Test
    fun widgetFormatsAllDayAndMalformedMeetingSafely() = runTest {
        val allDay = gatewayClientFixture()
        allDay.transport.fallback = { request ->
            when (request.rpcMethod) {
                "miniapp.calendar.list_upcoming" -> gatewayRpcReply(calendarPayload(event("one", summary = "", start = "2026-07-12", allDay = true)))
                else -> gatewayRpcReply(json.encodeToString(MailListPayload()))
            }
        }
        assertEquals("7/12 · 일정", allDay.client.widgetSummary().meeting)

        val malformed = gatewayClientFixture()
        malformed.transport.fallback = { request ->
            when (request.rpcMethod) {
                "miniapp.calendar.list_upcoming" -> gatewayRpcReply(calendarPayload(event("one", summary = "Title", start = "bad")))
                else -> gatewayRpcReply(json.encodeToString(MailListPayload()))
            }
        }
        assertEquals("Title", malformed.client.widgetSummary().meeting)
    }

    @Test
    fun widgetMailUsesSubjectFallbackAndUtf16SafeCap() = runTest {
        val f = gatewayClientFixture()
        val longSubject = "x".repeat(90) + "😀".repeat(20)
        val mail = json.encodeToString(
            MailListPayload(messages = listOf(MailRowOut(id = "m", from = "Sender", subject = longSubject))),
        )
        f.transport.fallback = { request ->
            when (request.rpcMethod) {
                "miniapp.calendar.list_upcoming" -> gatewayRpcReply(calendarPayload())
                else -> gatewayRpcReply(mail)
            }
        }

        val line = f.client.widgetSummary().latestMail

        assertTrue(line.endsWith("…"))
        assertFalse(line.dropLast(1).last().isHighSurrogate())
        assertTrue(line.length <= 101)
    }

    @Test
    fun widgetToleratesOneFailedBranchAndUsesOtherBranch() = runTest {
        val f = gatewayClientFixture()
        val mail = json.encodeToString(MailListPayload(messages = listOf(MailRowOut(id = "m", from = "Sender", subject = "Mail", isUnread = true))))
        f.transport.fallback = { request ->
            when (request.rpcMethod) {
                "miniapp.calendar.list_upcoming" -> GatewayHttpHarness.Reply("bad")
                else -> gatewayRpcReply(mail)
            }
        }

        val result = f.client.widgetSummary()

        assertTrue(result.ok)
        assertEquals("", result.meeting)
        assertEquals(1, result.unread)
        assertEquals("Sender · Mail", result.latestMail)
    }

    @Test
    fun widgetCancellationPropagatesInsteadOfBecomingQuietFailure() = runTest {
        val f = gatewayClientFixture()
        f.transport.fallback = { throw CancellationException("cancel widget") }

        val failure = assertFailsWith<CancellationException> { f.client.widgetSummary() }

        assertEquals("cancel widget", failure.message)
    }
}
