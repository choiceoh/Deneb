package ai.deneb.deneb

import io.ktor.http.HttpStatusCode
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

/** Calendar/to-do request contracts and state-preservation guarantees. */
class GatewayCalendarStateContractTest {
    @Test
    fun refreshCalendarFiltersBlankIdsKeepsLastDuplicateAndPublishesMappedRows() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "events":[
                    {"id":"","summary":"drop"},
                    {"id":"event-1","summary":"old","location":"old","start":"s1","end":"e1","allDay":false,"local":false,"category":"old"},
                    {"id":"event-2","summary":"second","location":"room","start":"s2","end":"e2","allDay":true,"local":true,"category":"personal"},
                    {"id":"event-1","summary":"new","location":"new room","start":"s3","end":"e3","allDay":true,"local":true,"category":"work"}
                ]
            }
            """.trimIndent(),
        )

        val refreshed = f.client.refreshCalendar()

        assertTrue(refreshed)
        val params = f.transport.singleRequest().requireRpc("miniapp.calendar.list_upcoming")
        assertEquals(168, params["hoursAhead"]?.jsonPrimitive?.content?.toInt())
        assertEquals(50, params["limit"]?.jsonPrimitive?.content?.toInt())
        val events = f.client.denebCalendar.value
        assertEquals(listOf("event-2", "event-1"), events.map { it.id })
        assertEquals(listOf("second", "new"), events.map { it.title })
        assertEquals(listOf("room", "new room"), events.map { it.location })
        assertEquals(listOf(false, true), events.map { it.id == "event-1" && it.allDay })
        assertEquals(listOf("personal", "work"), events.map { it.category })
    }

    @Test
    fun refreshCalendarLeavesExistingRowsUntouchedOnMalformedPayload() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"events":[{"id":"stable","summary":"Stable"}]}""")
        assertTrue(f.client.refreshCalendar())
        f.transport.enqueueRpc("""{"events":"wrong"}""")

        val refreshed = f.client.refreshCalendar()

        assertFalse(refreshed)
        assertEquals(listOf("stable"), f.client.denebCalendar.value.map { it.id })
    }

    @Test
    fun refreshCalendarPublishesSuccessfulEmptyList() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"events":[{"id":"old"}]}""")
        f.client.refreshCalendar()
        f.transport.enqueueRpc("""{"events":[]}""")

        val refreshed = f.client.refreshCalendar()

        assertTrue(refreshed)
        assertTrue(f.client.denebCalendar.value.isEmpty())
    }

    @Test
    fun fetchCalendarRangeSendsHalfOpenIsoWindowAndMapsRows() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "events":[{
                    "id":"range-1",
                    "summary":"범위 일정",
                    "location":"광주",
                    "start":"2026-07-01T00:00:00+09:00",
                    "end":"2026-07-02T00:00:00+09:00",
                    "allDay":true,
                    "local":false,
                    "category":"work"
                }]
            }
            """.trimIndent(),
        )

        val events = f.client.fetchCalendarRange(
            fromIso = "2026-07-01T00:00:00+09:00",
            toIso = "2026-08-01T00:00:00+09:00",
        )

        val params = f.transport.singleRequest().requireRpc("miniapp.calendar.list_range")
        assertEquals("2026-07-01T00:00:00+09:00", params["from"]?.jsonPrimitive?.content)
        assertEquals("2026-08-01T00:00:00+09:00", params["to"]?.jsonPrimitive?.content)
        assertEquals(2, params.size)
        val event = assertNotNull(events).single()
        assertEquals("range-1", event.id)
        assertEquals("범위 일정", event.title)
        assertEquals("광주", event.location)
        assertTrue(event.allDay)
        assertFalse(event.local)
    }

    @Test
    fun fetchCalendarRangeUsesCacheForSameWindow() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"events":[{"id":"cached","summary":"Cached"}]}""")

        val first = f.client.fetchCalendarRange("from", "to")
        val second = f.client.fetchCalendarRange("from", "to")

        assertEquals(listOf("cached"), first?.map { it.id })
        assertEquals(first, second)
        assertEquals(1, f.transport.requests.size)
    }

    @Test
    fun fetchCalendarRangeForceBypassesCacheAndReplacesValue() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"events":[{"id":"old","summary":"Old"}]}""")
        f.client.fetchCalendarRange("from", "to")
        f.transport.enqueueRpc("""{"events":[{"id":"new","summary":"New"}]}""")

        val refreshed = f.client.fetchCalendarRange("from", "to", force = true)
        val cached = f.client.fetchCalendarRange("from", "to")

        assertEquals(listOf("new"), refreshed?.map { it.id })
        assertEquals(refreshed, cached)
        assertEquals(2, f.transport.requests.size)
    }

    @Test
    fun failedForcedRangeRefreshDoesNotDestroyGoodCache() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"events":[{"id":"stable","summary":"Stable"}]}""")
        f.client.fetchCalendarRange("from", "to")
        f.transport.enqueueJson("not-json")

        val failed = f.client.fetchCalendarRange("from", "to", force = true)
        val cached = f.client.fetchCalendarRange("from", "to")

        assertNull(failed)
        assertEquals(listOf("stable"), cached?.map { it.id })
        assertEquals(2, f.transport.requests.size)
    }

    @Test
    fun createCalendarEventIncludesRequiredAndNonBlankOptionalFields() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)

        val error = f.client.createCalendarEvent(
            summary = "투자심의",
            description = "자료 검토",
            location = "3층 회의실",
            allDay = false,
            startIso = "2026-07-11T10:00:00+09:00",
            endIso = "2026-07-11T11:30:00+09:00",
            timeZone = "Asia/Seoul",
        )

        assertNull(error)
        val params = f.transport.singleRequest().requireRpc("miniapp.calendar.create")
        assertEquals("투자심의", params["summary"]?.jsonPrimitive?.content)
        assertEquals("자료 검토", params["description"]?.jsonPrimitive?.content)
        assertEquals("3층 회의실", params["location"]?.jsonPrimitive?.content)
        assertEquals(false, params["allDay"]?.jsonPrimitive?.content?.toBoolean())
        assertEquals("2026-07-11T10:00:00+09:00", params["start"]?.jsonPrimitive?.content)
        assertEquals("2026-07-11T11:30:00+09:00", params["end"]?.jsonPrimitive?.content)
        assertEquals("Asia/Seoul", params["timeZone"]?.jsonPrimitive?.content)
        assertFalse(params.containsKey("id"))
    }

    @Test
    fun createCalendarEventOmitsBlankOptionalFieldsButKeepsRequiredBlankStart() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)

        f.client.createCalendarEvent(
            summary = "일정",
            description = "",
            location = " ",
            allDay = true,
            startIso = "",
            endIso = "",
            timeZone = "",
        )

        val params = f.transport.singleRequest().requireRpc("miniapp.calendar.create")
        assertEquals(setOf("summary", "allDay", "start"), params.keys)
        assertEquals("", params["start"]?.jsonPrimitive?.content)
        assertEquals(true, params["allDay"]?.jsonPrimitive?.content?.toBoolean())
    }

    @Test
    fun updateCalendarEventAddsIdAndPreservesWriteFields() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = false, message = "google event is read-only")

        val error = f.client.updateCalendarEvent(
            id = "event-7",
            summary = "수정",
            description = "설명",
            location = "장소",
            allDay = false,
            startIso = "start",
            endIso = "end",
            timeZone = "Asia/Seoul",
        )

        assertEquals("google event is read-only", error)
        val params = f.transport.singleRequest().requireRpc("miniapp.calendar.update")
        assertEquals("event-7", params["id"]?.jsonPrimitive?.content)
        assertEquals("수정", params["summary"]?.jsonPrimitive?.content)
        assertEquals("설명", params["description"]?.jsonPrimitive?.content)
        assertEquals("장소", params["location"]?.jsonPrimitive?.content)
    }

    @Test
    fun deleteCalendarEventSendsIdOnly() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)

        val error = f.client.deleteCalendarEvent("local-1")

        assertNull(error)
        val params = f.transport.singleRequest().requireRpc("miniapp.calendar.delete")
        assertEquals(setOf("id"), params.keys)
        assertEquals("local-1", params["id"]?.jsonPrimitive?.content)
    }

    @Test
    fun refreshCalendarProposalsFiltersBlankIdsAndKeepsLastDuplicate() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "proposals":[
                    {"id":"","title":"drop"},
                    {"id":"p1","title":"old","start":"old","allDay":false,"kind":"mail"},
                    {"id":"p2","title":"second","start":"s2","allDay":true,"kind":"mail"},
                    {"id":"p1","title":"new","start":"s3","allDay":true,"kind":"agent","sourceSubject":"subject","sourceFrom":"sender"}
                ]
            }
            """.trimIndent(),
        )

        val refreshed = f.client.refreshCalendarProposals()

        assertTrue(refreshed)
        assertEquals("miniapp.calendar.proposals.list", f.transport.singleRequest().rpcMethod)
        val proposals = f.client.denebCalProposals.value
        assertEquals(listOf("p2", "p1"), proposals.map { it.id })
        assertEquals(listOf("second", "new"), proposals.map { it.title })
        assertEquals("subject", proposals.last().sourceSubject)
        assertEquals("sender", proposals.last().sourceFrom)
    }

    @Test
    fun failedProposalRefreshKeepsPriorSnapshot() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"proposals":[{"id":"stable","title":"Stable"}]}""")
        f.client.refreshCalendarProposals()
        f.transport.enqueueJson("not-json")

        val refreshed = f.client.refreshCalendarProposals()

        assertFalse(refreshed)
        assertEquals(listOf("stable"), f.client.denebCalProposals.value.map { it.id })
    }

    @Test
    fun acceptCalendarProposalRefreshesListOnlyAfterSuccessfulMutation() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)
        f.transport.enqueueRpc("""{"proposals":[{"id":"remaining","title":"Remaining"}]}""")

        val error = f.client.acceptCalendarProposal("accepted")

        assertNull(error)
        assertEquals(
            listOf("miniapp.calendar.proposals.accept", "miniapp.calendar.proposals.list"),
            f.transport.requestMethods(),
        )
        assertEquals("accepted", f.transport.requests[0].rpcParams?.get("id")?.jsonPrimitive?.content)
        assertEquals(listOf("remaining"), f.client.denebCalProposals.value.map { it.id })
    }

    @Test
    fun rejectedCalendarProposalMutationDoesNotRefreshList() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = false, message = "proposal expired")

        val error = f.client.acceptCalendarProposal("expired")

        assertEquals("proposal expired", error)
        assertEquals(1, f.transport.requests.size)
        assertEquals("miniapp.calendar.proposals.accept", f.transport.singleRequest().rpcMethod)
    }

    @Test
    fun rejectCalendarProposalRefreshesListOnSuccess() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)
        f.transport.enqueueRpc("""{"proposals":[]}""")

        val error = f.client.rejectCalendarProposal("p1")

        assertNull(error)
        assertEquals(
            listOf("miniapp.calendar.proposals.reject", "miniapp.calendar.proposals.list"),
            f.transport.requestMethods(),
        )
        assertTrue(f.client.denebCalProposals.value.isEmpty())
    }

    @Test
    fun fetchCalendarEventMapsOrganizerAttendeesAndDetailFields() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "id":"event-full",
                "summary":"상세 일정",
                "description":"설명",
                "location":"회의실",
                "start":"start",
                "end":"end",
                "allDay":false,
                "status":"confirmed",
                "local":true,
                "organizer":{"email":"owner@example.com","displayName":"담당자"},
                "attendees":[
                    {"email":"a@example.com","displayName":"Alice"},
                    {"email":"b@example.com","displayName":""},
                    {"email":"","displayName":""}
                ]
            }
            """.trimIndent(),
        )

        val detail = f.client.fetchCalendarEvent("event-full")

        val params = f.transport.singleRequest().requireRpc("miniapp.calendar.get")
        assertEquals("event-full", params["id"]?.jsonPrimitive?.content)
        assertEquals("상세 일정", detail?.title)
        assertEquals("설명", detail?.description)
        assertEquals("회의실", detail?.location)
        assertEquals("start", detail?.start)
        assertEquals("end", detail?.end)
        assertFalse(detail?.allDay ?: true)
        assertEquals("담당자", detail?.organizer)
        assertEquals(listOf("Alice", "b@example.com"), detail?.attendees)
        assertEquals("confirmed", detail?.status)
        assertTrue(detail?.local == true)
    }

    @Test
    fun fetchCalendarEventFallsBackToOrganizerEmail() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{"id":"e","organizer":{"email":"owner@example.com","displayName":""}}""",
        )

        val detail = f.client.fetchCalendarEvent("e")

        assertEquals("owner@example.com", detail?.organizer)
    }

    @Test
    fun fetchTodosFiltersBlankIdsKeepsLastDuplicateAndMapsCompletion() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "todos":[
                    {"id":"","title":"drop"},
                    {"id":"t1","title":"old","note":"old","due":"d1","dueAllDay":false,"done":false},
                    {"id":"t2","title":"second","note":"n2","due":"d2","dueAllDay":true,"done":true},
                    {"id":"t1","title":"new","note":"new note","due":"d3","dueAllDay":true,"done":true}
                ]
            }
            """.trimIndent(),
        )

        val todos = f.client.fetchTodos()

        assertEquals("miniapp.todo.list", f.transport.singleRequest().rpcMethod)
        assertEquals(listOf("t2", "t1"), todos?.map { it.id })
        assertEquals(listOf("second", "new"), todos?.map { it.title })
        assertEquals(listOf(true, true), todos?.map { it.done })
        assertEquals(listOf("d2", "d3"), todos?.map { it.due })
    }

    @Test
    fun createTodoOmitsBlankOptionalNoteAndDue() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)

        f.client.createTodo("할 일", " ", "", dueAllDay = true)

        val params = f.transport.singleRequest().requireRpc("miniapp.todo.create")
        assertEquals(setOf("title"), params.keys)
        assertEquals("할 일", params["title"]?.jsonPrimitive?.content)
    }

    @Test
    fun createTodoIncludesDueFlagOnlyWhenDueExists() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)

        f.client.createTodo("마감", "설명", "2026-07-31", dueAllDay = true)

        val params = f.transport.singleRequest().requireRpc("miniapp.todo.create")
        assertEquals("마감", params["title"]?.jsonPrimitive?.content)
        assertEquals("설명", params["note"]?.jsonPrimitive?.content)
        assertEquals("2026-07-31", params["due"]?.jsonPrimitive?.content)
        assertEquals(true, params["dueAllDay"]?.jsonPrimitive?.content?.toBoolean())
    }

    @Test
    fun updateTodoAddsIdAndPreservesAllEditableFields() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)

        f.client.updateTodo("todo-1", "수정", "노트", "2026-08-01T09:00:00+09:00", dueAllDay = false)

        val params = f.transport.singleRequest().requireRpc("miniapp.todo.update")
        assertEquals("todo-1", params["id"]?.jsonPrimitive?.content)
        assertEquals("수정", params["title"]?.jsonPrimitive?.content)
        assertEquals("노트", params["note"]?.jsonPrimitive?.content)
        assertEquals("2026-08-01T09:00:00+09:00", params["due"]?.jsonPrimitive?.content)
        assertEquals(false, params["dueAllDay"]?.jsonPrimitive?.content?.toBoolean())
    }

    @Test
    fun setTodoDoneSendsIdAndBooleanForBothTransitions() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)
        f.transport.enqueueWrite(ok = true)

        assertNull(f.client.setTodoDone("todo", true))
        assertNull(f.client.setTodoDone("todo", false))

        val first = f.transport.requests[0].requireRpc("miniapp.todo.set_done")
        assertEquals("todo", first["id"]?.jsonPrimitive?.content)
        assertEquals(true, first["done"]?.jsonPrimitive?.content?.toBoolean())
        val second = f.transport.requests[1].requireRpc("miniapp.todo.set_done")
        assertEquals("todo", second["id"]?.jsonPrimitive?.content)
        assertEquals(false, second["done"]?.jsonPrimitive?.content?.toBoolean())
    }

    @Test
    fun deleteTodoSendsIdOnlyAndSurfacesValidationMessage() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = false, message = "todo not found")

        val error = f.client.deleteTodo("missing")

        assertEquals("todo not found", error)
        val params = f.transport.singleRequest().requireRpc("miniapp.todo.delete")
        assertEquals(setOf("id"), params.keys)
        assertEquals("missing", params["id"]?.jsonPrimitive?.content)
    }

    @Test
    fun calendarReadCancellationPropagatesWithoutClearingExistingState() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"events":[{"id":"stable","summary":"Stable"}]}""")
        f.client.refreshCalendar()
        f.transport.enqueueFailure(CancellationException("cancel calendar"))

        val failure = assertFailsWith<CancellationException> {
            f.client.refreshCalendar()
        }

        assertEquals("cancel calendar", failure.message)
        assertEquals(listOf("stable"), f.client.denebCalendar.value.map { it.id })
    }

    @Test
    fun calendarReadsReturnFailureSentinelOnHttpError() = runTest {
        val f = gatewayClientFixture()
        repeat(5) {
            f.transport.enqueueRpc(payload = "{}", status = HttpStatusCode.ServiceUnavailable)
        }

        val refreshed = f.client.refreshCalendar()
        val range = f.client.fetchCalendarRange("x", "y", force = true)
        val proposals = f.client.refreshCalendarProposals()
        val detail = f.client.fetchCalendarEvent("id")
        val todos = f.client.fetchTodos()

        assertFalse(refreshed)
        assertNull(range)
        assertFalse(proposals)
        assertNull(detail)
        assertNull(todos)
    }
}
