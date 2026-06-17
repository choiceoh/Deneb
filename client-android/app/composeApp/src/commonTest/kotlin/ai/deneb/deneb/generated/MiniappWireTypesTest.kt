package ai.deneb.deneb.generated

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

    @Test
    fun `mail detail decodes with nested attachments`() {
        val payload = """
            {
              "id": "m1", "threadId": "t1", "from": "a@b.kr", "to": "me@x.kr",
              "subject": "견적", "date": "2026-06-03", "body": "본문",
              "bodyTotal": 1200, "labels": ["INBOX"],
              "attachments": [ { "id": "att1", "filename": "quote.pdf", "mimeType": "application/pdf", "size": 20480 } ]
            }
        """.trimIndent()

        val m = json.decodeFromString<MailMessageOut>(payload)

        assertEquals("m1", m.id)
        assertEquals("a@b.kr", m.from)
        assertEquals(1200, m.bodyTotal)
        assertEquals(1, m.attachments.size)
        assertEquals("quote.pdf", m.attachments[0].filename)
        assertEquals(20480, m.attachments[0].size)
    }

    @Test
    fun `mail list row decodes the fields the list view reads`() {
        val row = json.decodeFromString<MailRowOut>(
            """{ "id": "m2", "from": "x@y.kr", "subject": "회의", "snippet": "내일", "date": "2026-06-03", "isUnread": true, "mailbox": "Gmail", "hasAttachment": true, "attachmentCount": 2 }""",
        )
        assertEquals("m2", row.id)
        assertEquals("회의", row.subject)
        assertTrue(row.isUnread)
        assertEquals("Gmail", row.mailbox)
        assertTrue(row.hasAttachment)
        assertEquals(2, row.attachmentCount)
    }

    @Test
    fun `mail native status decodes archive mailbox stats`() {
        val status = json.decodeFromString<MailNativeStatusOut>(
            """
            {
              "source": "archive",
              "available": true,
              "offlineCapable": true,
              "generatedAt": "2026-06-17T01:02:03Z",
              "mailboxes": [
                {
                  "name": "INBOX",
                  "total": 12,
                  "unread": 3,
                  "locallyRead": 2,
                  "latestUid": "55",
                  "attachmentCapable": true
                }
              ],
              "overlay": { "messages": 4, "read": 2, "archived": 1, "trashed": 1 }
            }
            """.trimIndent(),
        )

        assertEquals("archive", status.source)
        assertTrue(status.offlineCapable)
        assertEquals("INBOX", status.mailboxes.single().name)
        assertEquals(3, status.mailboxes.single().unread)
        assertEquals(1, status.overlay.archived)
    }

    @Test
    fun `session row decodes with optional pointer fields absent as null`() {
        // startedAtMs/runtimeMs/totalTokens are *int64 (omitempty) on the gateway,
        // so for a not-yet-started session they are absent → null, not 0.
        val row = json.decodeFromString<SessionRowOut>(
            """{ "key": "client:main", "label": "업무", "channel": "client", "updatedAtMs": 1717000000000 }""",
        )
        assertEquals("client:main", row.key)
        assertEquals("업무", row.label)
        assertEquals(1717000000000L, row.updatedAtMs)
        assertNull(row.startedAtMs)
        assertNull(row.totalTokens)
    }

    @Test
    fun `transcript message decodes with nested attachments`() {
        val msg = json.decodeFromString<TranscriptMsgOut>(
            """
            {
              "role": "assistant", "content": "보고서입니다",
              "attachments": [ { "type": "image", "mimeType": "image/png", "data": "BASE64", "name": "report.png", "size": 4096 } ],
              "timestampMs": 1717000000000
            }
            """.trimIndent(),
        )
        assertEquals("assistant", msg.role)
        assertEquals(1, msg.attachments.size)
        assertEquals("image/png", msg.attachments[0].mimeType)
        assertEquals("report.png", msg.attachments[0].name)
    }

    @Test
    fun `mail analysis decodes with RFC3339 createdAt and related projects`() {
        // createdAt is time.Time on the gateway; the generator maps it to String,
        // so the RFC3339 wire value decodes straight into a Kotlin String.
        val payload = """
            {
              "id": "m1", "subject": "견적 회신", "from": "a@b.kr",
              "analysis": "핵심 요약", "cached": true, "durationMs": 820,
              "createdAt": "2026-06-03T09:00:00Z",
              "relatedProjects": [ { "path": "p/x.md", "title": "프로젝트X", "summary": "요약" } ]
            }
        """.trimIndent()

        val a = json.decodeFromString<MailAnalysisOut>(payload)

        assertEquals("핵심 요약", a.analysis)
        assertTrue(a.cached)
        assertEquals(820L, a.durationMs)
        assertEquals("2026-06-03T09:00:00Z", a.createdAt)
        assertEquals(1, a.relatedProjects.size)
        assertEquals("프로젝트X", a.relatedProjects[0].title)
    }

    @Test
    fun `sender context wiki hit and recent rows decode`() {
        val hit = json.decodeFromString<SenderWikiHitOut>(
            """{ "path": "people/김부장.md", "title": "김부장", "summary": "탑솔라 구매", "category": "people" }""",
        )
        assertEquals("김부장", hit.title)
        assertEquals("people", hit.category)

        val recent = json.decodeFromString<SenderRecentOut>(
            """{ "count": 5, "lastReceivedAt": "2026-06-01", "windowDays": 30, "truncated": true }""",
        )
        assertEquals(5, recent.count)
        assertEquals(30, recent.windowDays)
        assertTrue(recent.truncated)
    }

    @Test
    fun `todo decodes with omitempty fields defaulting`() {
        // Shape emitted by miniapp.todo.list: an undated, incomplete to-do drops
        // every omitempty field, leaving only id + title.
        val undated = json.decodeFromString<TodoOut>("""{ "id": "todo:1", "title": "장보기" }""")
        assertEquals("todo:1", undated.id)
        assertEquals("장보기", undated.title)
        assertEquals("", undated.due)
        assertTrue(!undated.dueAllDay)
        assertTrue(!undated.done)
        assertEquals("", undated.doneAt)

        // A completed, dated to-do carries the full shape.
        val done = json.decodeFromString<TodoOut>(
            """
            {
              "id": "todo:2", "title": "보고서", "note": "Q2",
              "due": "2026-06-10T00:00:00Z", "dueAllDay": true,
              "done": true, "doneAt": "2026-06-09T01:00:00Z"
            }
            """.trimIndent(),
        )
        assertEquals("Q2", done.note)
        assertEquals("2026-06-10T00:00:00Z", done.due)
        assertTrue(done.dueAllDay)
        assertTrue(done.done)
        assertEquals("2026-06-09T01:00:00Z", done.doneAt)
    }
}
