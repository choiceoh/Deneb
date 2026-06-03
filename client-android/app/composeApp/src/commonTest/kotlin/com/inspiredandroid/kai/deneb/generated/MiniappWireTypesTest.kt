package com.inspiredandroid.kai.deneb.generated

import kotlinx.serialization.json.Json
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * Runtime decode tests for the generated miniapp wire types.
 *
 * These pin the Kotlin types against the exact JSON the Go gateway emits
 * (the `json:"..."` tags on calendarEventOut / calendarAttendeeOut /
 * calendarConferenceOut). The codegen drift gate (`make kotlin-models-check`)
 * guarantees the *shape* matches Go; these tests guarantee the shape actually
 * round-trips real payloads, which compilation alone cannot.
 */
class MiniappWireTypesTest {

    // Mirror the gateway client's lenient decoder: forward-compatible with
    // new gateway fields, tolerant of omitted (omitempty) ones.
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
    }

    @Test
    fun `list-view element decodes with omitempty fields defaulting`() {
        // Shape emitted by miniapp.calendar.list_upcoming elements: only the
        // populated fields are present (Go omitempty drops the rest).
        val payload = """
            {
              "id": "evt-1",
              "summary": "Standup",
              "start": "2026-06-03T09:00:00Z",
              "end": "2026-06-03T09:30:00Z",
              "hasMeet": true
            }
        """.trimIndent()

        val ev = json.decodeFromString<CalendarEventOut>(payload)

        assertEquals("evt-1", ev.id)
        assertEquals("Standup", ev.summary)
        assertEquals("2026-06-03T09:00:00Z", ev.start)
        assertEquals("2026-06-03T09:30:00Z", ev.end)
        assertTrue(ev.hasMeet)
        // Omitted fields fall back to defaults rather than failing the decode.
        assertEquals("", ev.description)
        assertEquals("", ev.status)
        assertNull(ev.organizer)
        assertNull(ev.conference)
        assertTrue(ev.attendees.isEmpty())
    }

    @Test
    fun `detail-view event decodes nested organizer, attendees and conference`() {
        // Shape emitted by miniapp.calendar.get: the full event, including the
        // nested types the old hand-written list mirror (CalRow) dropped.
        val payload = """
            {
              "id": "evt-2",
              "summary": "Quarterly review",
              "description": "Q2 numbers",
              "location": "Room 4",
              "start": "2026-06-04T10:00:00Z",
              "end": "2026-06-04T11:00:00Z",
              "status": "confirmed",
              "htmlLink": "https://calendar.example/evt-2",
              "organizer": { "email": "boss@topsolar.kr", "displayName": "김부장", "organizer": true },
              "attendees": [
                { "email": "a@topsolar.kr", "displayName": "직원A", "responseStatus": "accepted", "self": true },
                { "email": "b@topsolar.kr", "displayName": "직원B", "responseStatus": "needsAction" }
              ],
              "conference": { "solution": "Google Meet", "uri": "https://meet.example/abc" }
            }
        """.trimIndent()

        val ev = json.decodeFromString<CalendarEventOut>(payload)

        assertEquals("evt-2", ev.id)
        assertEquals("Q2 numbers", ev.description)
        assertEquals("confirmed", ev.status)

        // Nested organizer (recovered field: the boolean `organizer` flag).
        assertEquals("김부장", ev.organizer?.displayName)
        assertTrue(ev.organizer?.organizer == true)

        // Attendees list with the recovered `self` flag.
        assertEquals(2, ev.attendees.size)
        assertEquals("직원A", ev.attendees[0].displayName)
        assertTrue(ev.attendees[0].self)
        assertEquals("needsAction", ev.attendees[1].responseStatus)

        // Conference object — a type the client previously lacked entirely.
        assertEquals("Google Meet", ev.conference?.solution)
        assertEquals("https://meet.example/abc", ev.conference?.uri)
    }

    @Test
    fun `unknown gateway field is ignored for forward compatibility`() {
        // If the gateway adds a field before the client regenerates, decoding
        // must not throw — additive drift stays non-breaking.
        val payload = """{ "id": "evt-3", "summary": "x", "newGatewayField": 42 }"""
        val ev = json.decodeFromString<CalendarEventOut>(payload)
        assertEquals("evt-3", ev.id)
    }

    @Test
    fun `unified search response decodes wiki, diary and people rows`() {
        val payload = """
            {
              "wiki": [ { "path": "p/a.md", "title": "A", "snippet": "s", "category": "c", "score": 1.5 } ],
              "diary": [ { "file": "d.md", "header": "H", "content": "txt", "at": 1717000000000, "score": 2.0 } ],
              "people": [ { "email": "x@y.kr", "name": "이름", "messageCount": 7, "lastSubject": "건" } ]
            }
        """.trimIndent()

        val r = json.decodeFromString<SearchAllResult>(payload)

        assertEquals(1, r.wiki.size)
        assertEquals("p/a.md", r.wiki[0].path)
        assertEquals("s", r.wiki[0].snippet)
        assertEquals("H", r.diary[0].header)
        assertEquals("이름", r.people[0].name)
        assertEquals(7, r.people[0].messageCount)
    }

    @Test
    fun `cron detail decodes the full job shape`() {
        val payload = """
            {
              "id": "job-1", "name": "아침 브리핑", "enabled": true,
              "schedule": "매일 09:00", "scheduleSpec": "0 9 * * *", "scheduleKind": "cron",
              "payloadKind": "agentTurn", "prompt": "전체 프롬프트",
              "deliveryChannel": "client", "deliveryTo": "main",
              "nextRunAtMs": 1717000000000, "consecutiveErrors": 0
            }
        """.trimIndent()

        val d = json.decodeFromString<MiniappCronDetail>(payload)

        assertEquals("job-1", d.id)
        assertEquals("아침 브리핑", d.name)
        assertTrue(d.enabled)
        assertEquals("0 9 * * *", d.scheduleSpec)
        assertEquals("전체 프롬프트", d.prompt)
    }

    @Test
    fun `models section and role binding decode`() {
        val section = json.decodeFromString<ModelSection>(
            """{ "title": "OpenAI", "models": [ { "id": "gpt", "label": "GPT", "current": true } ] }""",
        )
        assertEquals("OpenAI", section.title)
        assertEquals("gpt", section.models[0].id)
        assertTrue(section.models[0].current)

        val role = json.decodeFromString<RoleModel>("""{ "role": "main", "model": "gpt" }""")
        assertEquals("main", role.role)
        assertEquals("gpt", role.model)
    }

    @Test
    fun `QATurn encodes to the gateway's q and a keys`() {
        // Locks the request-shape fix: the gateway binds history to []QATurn{q,a},
        // so the client must emit q/a — the old {question, answer} keys were dropped.
        val encoded = json.encodeToString(QATurn.serializer(), QATurn(q = "질문", a = "답변"))
        assertEquals("""{"q":"질문","a":"답변"}""", encoded)
    }
}
