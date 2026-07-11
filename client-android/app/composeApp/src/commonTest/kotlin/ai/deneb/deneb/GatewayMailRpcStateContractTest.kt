package ai.deneb.deneb

import io.ktor.http.ContentType
import io.ktor.http.HttpStatusCode
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.Test
import kotlin.test.assertContentEquals
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

/** Mail RPC mapping, optimistic state, pagination, attachment, and cancellation contracts. */
@OptIn(ExperimentalCoroutinesApi::class)
class GatewayMailRpcStateContractTest {
    private val emptyNativeStatus = """{
        "source":"native",
        "available":true,
        "offlineCapable":true,
        "mailboxes":[],
        "overlay":{},
        "pipeline":{},
        "generatedAt":0,
        "error":""
    }
    """.trimIndent()

    @Test
    fun refreshMailTrimsQueryMapsRowsAndPublishesPaginationState() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "messages":[
                    {"id":"","from":"drop"},
                    {
                        "id":"mail-1",
                        "threadId":"thread-1",
                        "from":"Alice <alice@example.com>",
                        "subject":"계약 검토",
                        "snippet":"검토 부탁드립니다",
                        "date":"2026-07-11T01:00:00Z",
                        "isUnread":true,
                        "labels":["INBOX"],
                        "mailbox":"INBOX",
                        "hasAttachment":false,
                        "attachmentCount":2,
                        "priority":"high",
                        "priorityHint":"오늘 회신",
                        "analysisStatus":"done",
                        "analysisQuality":"high",
                        "feedStatus":"created",
                        "calendarProposalCount":1,
                        "todoCount":2,
                        "workStateHint":"처리 필요"
                    }
                ],
                "nextPageToken":"page-2"
            }
            """.trimIndent(),
        )
        f.transport.enqueueRpc(emptyNativeStatus)

        val refreshed = f.client.refreshMail("  from:alice@example.com  ")

        assertTrue(refreshed)
        val params = f.transport.requests[0].requireRpc("miniapp.mail.list_recent")
        assertEquals(60, params["limit"]?.jsonPrimitive?.content?.toInt())
        assertEquals("from:alice@example.com", params["query"]?.jsonPrimitive?.content)
        assertEquals("from:alice@example.com", f.client.denebMailActiveQuery)
        assertEquals("page-2", f.client.denebMailNextToken.value)
        val row = f.client.denebMail.value.single()
        assertEquals("mail-1", row.id)
        assertEquals("Alice <alice@example.com>", row.from)
        assertEquals("계약 검토", row.subject)
        assertEquals("검토 부탁드립니다", row.snippet)
        assertTrue(row.unread)
        assertEquals("high", row.priority)
        assertEquals("오늘 회신", row.priorityHint)
        assertEquals("INBOX", row.mailbox)
        assertTrue(row.hasAttachment)
        assertEquals(2, row.attachmentCount)
        assertEquals("done", row.workState.analysisStatus)
        assertEquals("high", row.workState.analysisQuality)
        assertEquals("created", row.workState.feedStatus)
        assertEquals(1, row.workState.calendarProposalCount)
        assertEquals(2, row.workState.todoCount)
        assertEquals("처리 필요", row.workState.hint)
        assertEquals("miniapp.mail.native_status", f.transport.requests[1].rpcMethod)
    }

    @Test
    fun refreshMailTreatsBlankQueryAsDefaultViewAndOmitsQueryKey() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"messages":[],"nextPageToken":""}""")
        f.transport.enqueueRpc(emptyNativeStatus)

        val refreshed = f.client.refreshMail(" \n\t ")

        assertTrue(refreshed)
        val params = f.transport.requests[0].requireRpc("miniapp.mail.list_recent")
        assertFalse(params.containsKey("query"))
        assertEquals(60, params["limit"]?.jsonPrimitive?.content?.toInt())
        assertNull(f.client.denebMailActiveQuery)
        assertNull(f.client.denebMailNextToken.value)
        assertTrue(f.client.denebMail.value.isEmpty())
    }

    @Test
    fun refreshMailReappliesLocalReadOverlayToStaleGatewayRows() = runTest {
        val f = gatewayClientFixture()
        f.client.locallyReadMailIds += "mail-read"
        f.transport.enqueueRpc(
            """{
                "messages":[
                    {"id":"mail-read","subject":"Read","isUnread":true},
                    {"id":"mail-new","subject":"New","isUnread":true}
                ]
            }
            """.trimIndent(),
        )
        f.transport.enqueueRpc(emptyNativeStatus)

        val refreshed = f.client.refreshMail()

        assertTrue(refreshed)
        assertEquals(listOf(false, true), f.client.denebMail.value.map { it.unread })
    }

    @Test
    fun failedMailRefreshPreservesRowsQueryAndCursor() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"messages":[{"id":"stable"}],"nextPageToken":"next"}""")
        f.transport.enqueueRpc(emptyNativeStatus)
        assertTrue(f.client.refreshMail("stable query"))
        f.transport.enqueueJson("not-json")

        val refreshed = f.client.refreshMail("new query")

        assertFalse(refreshed)
        assertEquals(listOf("stable"), f.client.denebMail.value.map { it.id })
        assertEquals("stable query", f.client.denebMailActiveQuery)
        assertEquals("next", f.client.denebMailNextToken.value)
    }

    @Test
    fun refreshMailNativeStatusMapsMailboxOverlayAndPipelineDiagnostics() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "source":"imap",
                "available":true,
                "offlineCapable":true,
                "mailboxes":[{
                    "name":"INBOX",
                    "total":100,
                    "unread":7,
                    "locallyRead":3,
                    "locallyArchived":2,
                    "locallyTrashed":1,
                    "latestUid":"9223372036854775807",
                    "attachmentCapable":true
                }],
                "overlay":{"messages":90,"read":3,"archived":2,"trashed":1},
                "pipeline":{
                    "messages":80,
                    "analyzed":70,
                    "analyzing":2,
                    "failed":1,
                    "feedCreated":15,
                    "feedMissing":4,
                    "calendarCandidates":8,
                    "todoCandidates":9,
                    "updatedAt":"2026-07-11T01:02:03Z",
                    "error":"one failed"
                },
                "generatedAt":"2026-07-11T01:02:04Z",
                "error":""
            }
            """.trimIndent(),
        )

        val status = f.client.refreshMailNativeStatus()

        assertEquals("miniapp.mail.native_status", f.transport.singleRequest().rpcMethod)
        assertEquals("imap", status?.source)
        assertTrue(status?.available == true)
        assertTrue(status?.offlineCapable == true)
        val mailbox = assertNotNull(status).mailboxes.single()
        assertEquals("INBOX", mailbox.name)
        assertEquals(100, mailbox.total)
        assertEquals(7, mailbox.unread)
        assertEquals(3, mailbox.locallyRead)
        assertEquals(2, mailbox.locallyArchived)
        assertEquals(1, mailbox.locallyTrashed)
        assertEquals("9223372036854775807", mailbox.latestUid)
        assertTrue(mailbox.attachmentCapable)
        assertEquals(90, status.overlay.messages)
        assertEquals(3, status.overlay.read)
        assertEquals(70, status.pipeline.analyzed)
        assertEquals(2, status.pipeline.analyzing)
        assertEquals(1, status.pipeline.failed)
        assertEquals(15, status.pipeline.feedCreated)
        assertEquals(4, status.pipeline.feedMissing)
        assertEquals(8, status.pipeline.calendarCandidates)
        assertEquals(9, status.pipeline.todoCandidates)
        assertEquals("2026-07-11T01:02:03Z", status.pipeline.updatedAt)
        assertEquals("one failed", status.pipeline.error)
        assertEquals(status, f.client.denebMailNativeStatus.value)
    }

    @Test
    fun loadMoreMailSkipsNetworkWithoutCursor() = runTest {
        val f = gatewayClientFixture()

        f.client.loadMoreMail()

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun loadMoreMailSendsActiveQueryDeduplicatesAndAdvancesCursor() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"messages":[{"id":"existing","subject":"Old"}],"nextPageToken":"page-2"}""")
        f.transport.enqueueRpc(emptyNativeStatus)
        f.client.refreshMail("from:alice")
        f.transport.clearRequests()
        f.transport.enqueueRpc(
            """{
                "messages":[
                    {"id":"existing","subject":"Duplicate"},
                    {"id":"new","subject":"New","isUnread":true}
                ],
                "nextPageToken":"page-3"
            }
            """.trimIndent(),
        )

        f.client.loadMoreMail()

        val params = f.transport.singleRequest().requireRpc("miniapp.mail.list_recent")
        assertEquals(60, params["limit"]?.jsonPrimitive?.content?.toInt())
        assertEquals("page-2", params["pageToken"]?.jsonPrimitive?.content)
        assertEquals("from:alice", params["query"]?.jsonPrimitive?.content)
        assertEquals(listOf("existing", "new"), f.client.denebMail.value.map { it.id })
        assertEquals("page-3", f.client.denebMailNextToken.value)
    }

    @Test
    fun failedLoadMoreKeepsRowsAndCursorForRetry() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"messages":[{"id":"stable"}],"nextPageToken":"retry-token"}""")
        f.transport.enqueueRpc(emptyNativeStatus)
        f.client.refreshMail()
        f.transport.enqueueJson("not-json")

        f.client.loadMoreMail()

        assertEquals(listOf("stable"), f.client.denebMail.value.map { it.id })
        assertEquals("retry-token", f.client.denebMailNextToken.value)
    }

    @Test
    fun fetchMailDetailMapsFullBodyAttachmentsAndWorkState() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "id":"mail-full",
                "from":"Alice <a@example.com>",
                "to":"me@example.com",
                "cc":"cc@example.com",
                "subject":"계약",
                "date":"2026-07-11",
                "body":"clean body",
                "bodyTotal":100,
                "rawBody":"raw body",
                "rawBodyTotal":200,
                "bodyCleaned":true,
                "bodyHiddenBlockCount":3,
                "bodyHiddenLineCount":17,
                "attachments":[
                    {"id":"","filename":"drop.pdf","mimeType":"application/pdf","size":1},
                    {"id":"att-1","filename":"","mimeType":"image/png","size":2147483647},
                    {"id":"att-2","filename":"report.pdf","mimeType":"application/pdf","size":42}
                ],
                "analysisStatus":"done",
                "analysisQuality":"high",
                "feedStatus":"created",
                "calendarProposalCount":2,
                "todoCount":3,
                "workStateHint":"follow up"
            }
            """.trimIndent(),
        )

        val detail = f.client.fetchMailDetail("mail-full", full = true)

        val params = f.transport.singleRequest().requireRpc("miniapp.mail.get")
        assertEquals("mail-full", params["id"]?.jsonPrimitive?.content)
        assertEquals(true, params["full"]?.jsonPrimitive?.content?.toBoolean())
        assertEquals("Alice <a@example.com>", detail?.from)
        assertEquals("me@example.com", detail?.to)
        assertEquals("cc@example.com", detail?.cc)
        assertEquals("clean body", detail?.body)
        assertEquals(100, detail?.bodyTotal)
        assertEquals("raw body", detail?.rawBody)
        assertEquals(200, detail?.rawBodyTotal)
        assertTrue(detail?.bodyCleaned == true)
        assertEquals(3, detail?.bodyHiddenBlockCount)
        assertEquals(17, detail?.bodyHiddenLineCount)
        assertEquals(listOf("att-1", "att-2"), detail?.attachments?.map { it.id })
        assertEquals(listOf("image/png", "report.pdf"), detail?.attachments?.map { it.filename })
        assertEquals(Int.MAX_VALUE, detail?.attachments?.first()?.size)
        assertEquals("done", detail?.workState?.analysisStatus)
        assertEquals("high", detail?.workState?.analysisQuality)
        assertEquals("created", detail?.workState?.feedStatus)
        assertEquals(2, detail?.workState?.calendarProposalCount)
        assertEquals(3, detail?.workState?.todoCount)
        assertEquals("follow up", detail?.workState?.hint)
    }

    @Test
    fun fetchMailDetailOmitsFullFlagByDefault() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"id":"mail"}""")

        val detail = f.client.fetchMailDetail("mail")

        assertEquals("mail", detail?.id)
        val params = f.transport.singleRequest().requireRpc("miniapp.mail.get")
        assertEquals(setOf("id"), params.keys)
    }

    @Test
    fun markMailReadClearsLiveRowRecordsOverlayAndRefreshesNativeStatus() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"messages":[{"id":"read-me","isUnread":true},{"id":"other","isUnread":true}]}""")
        f.transport.enqueueRpc(emptyNativeStatus)
        f.client.refreshMail("search")
        f.transport.clearRequests()
        f.transport.enqueueRpc("""{"ok":true}""")
        f.transport.enqueueRpc(emptyNativeStatus)

        val marked = f.client.markMailRead("read-me")

        assertTrue(marked)
        assertEquals("miniapp.mail.mark_read", f.transport.requests[0].rpcMethod)
        assertEquals("read-me", f.transport.requests[0].rpcParams?.get("id")?.jsonPrimitive?.content)
        assertEquals(listOf(false, true), f.client.denebMail.value.map { it.unread })
        assertTrue("read-me" in f.client.locallyReadMailIds)
        assertEquals("miniapp.mail.native_status", f.transport.requests[1].rpcMethod)
    }

    @Test
    fun failedMarkReadDoesNotMutateListOrOverlayOrRefreshStatus() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"messages":[{"id":"mail","isUnread":true}]}""")
        f.transport.enqueueRpc(emptyNativeStatus)
        f.client.refreshMail("search")
        f.transport.clearRequests()
        f.transport.enqueueRpc("""{"ok":false}""")

        val marked = f.client.markMailRead("mail")

        assertFalse(marked)
        assertTrue(f.client.denebMail.value.single().unread)
        assertFalse("mail" in f.client.locallyReadMailIds)
        assertEquals(1, f.transport.requests.size)
    }

    @Test
    fun archiveMailRemovesOnlyTargetAfterAcceptedMutation() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"messages":[{"id":"archive"},{"id":"keep"}]}""")
        f.transport.enqueueRpc(emptyNativeStatus)
        f.client.refreshMail("search")
        f.transport.clearRequests()
        f.transport.enqueueRpc("""{"ok":true}""")
        f.transport.enqueueRpc(emptyNativeStatus)

        val archived = f.client.archiveMail("archive")

        assertTrue(archived)
        assertEquals(listOf("keep"), f.client.denebMail.value.map { it.id })
        assertEquals("miniapp.mail.archive", f.transport.requests[0].rpcMethod)
    }

    @Test
    fun trashMailKeepsListWhenMutationRejected() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"messages":[{"id":"mail"}]}""")
        f.transport.enqueueRpc(emptyNativeStatus)
        f.client.refreshMail("search")
        f.transport.clearRequests()
        f.transport.enqueueRpc("""{"ok":false}""")

        val trashed = f.client.trashMail("mail")

        assertFalse(trashed)
        assertEquals(listOf("mail"), f.client.denebMail.value.map { it.id })
        assertEquals(1, f.transport.requests.size)
    }

    @Test
    fun analyzeMailMapsAnalysisRelatedProjectsTimingAndWorkState() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "analysis":"핵심 분석",
                "relatedProjects":[{"path":"projects/a.md","title":"A","summary":"관련"}],
                "durationMs":9223372036854775807,
                "cached":true,
                "createdAt":"2026-07-11T01:02:03Z",
                "analysisStatus":"done",
                "analysisQuality":"high",
                "feedStatus":"created",
                "calendarProposalCount":1,
                "todoCount":2,
                "workStateHint":"결정 필요"
            }
            """.trimIndent(),
        )

        val analysis = f.client.analyzeMail("mail", force = true)

        val params = f.transport.singleRequest().requireRpc("miniapp.mail.analyze")
        assertEquals("mail", params["id"]?.jsonPrimitive?.content)
        assertEquals(true, params["force"]?.jsonPrimitive?.content?.toBoolean())
        assertEquals("핵심 분석", analysis?.text)
        assertEquals("projects/a.md", analysis?.related?.single()?.path)
        assertEquals("A", analysis?.related?.single()?.title)
        assertEquals("관련", analysis?.related?.single()?.summary)
        assertTrue(analysis?.cached == true)
        assertEquals(Long.MAX_VALUE, analysis?.durationMs)
        assertEquals("2026-07-11T01:02:03Z", analysis?.createdAt)
        assertEquals("done", analysis?.workState?.analysisStatus)
        assertEquals(1, analysis?.workState?.calendarProposalCount)
        assertEquals(2, analysis?.workState?.todoCount)
    }

    @Test
    fun analyzeMailOmitsForceFlagByDefaultAndRejectsBlankAnalysis() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"analysis":"   ","cached":true}""")

        val analysis = f.client.analyzeMail("mail")

        assertNull(analysis)
        val params = f.transport.singleRequest().requireRpc("miniapp.mail.analyze")
        assertEquals(setOf("id"), params.keys)
    }

    @Test
    fun fetchCachedAnalysisUsesDedicatedMethodAndRejectsBlankText() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"analysis":""}""")

        val analysis = f.client.fetchCachedAnalysis("mail")

        assertNull(analysis)
        val params = f.transport.singleRequest().requireRpc("miniapp.mail.analysis_cached")
        assertEquals("mail", params["id"]?.jsonPrimitive?.content)
    }

    @Test
    fun askMailSerializesGeneratedQATurnKeysAndPreservesOrder() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"answer":"최종 답변"}""")

        val answer = f.client.askMail(
            id = "mail",
            question = "이번에는?",
            history = listOf(
                "첫 질문" to "첫 답",
                "둘째 질문" to "둘째 답",
            ),
        )

        assertEquals("최종 답변", answer)
        val params = f.transport.singleRequest().requireRpc("miniapp.mail.ask")
        assertEquals("mail", params["id"]?.jsonPrimitive?.content)
        assertEquals("이번에는?", params["question"]?.jsonPrimitive?.content)
        val history = params["history"]?.jsonArray.orEmpty()
        assertEquals(2, history.size)
        assertEquals(setOf("q", "a"), history[0].jsonObject.keys)
        assertEquals("첫 질문", history[0].jsonObject["q"]?.jsonPrimitive?.content)
        assertEquals("첫 답", history[0].jsonObject["a"]?.jsonPrimitive?.content)
        assertEquals("둘째 질문", history[1].jsonObject["q"]?.jsonPrimitive?.content)
        assertEquals("둘째 답", history[1].jsonObject["a"]?.jsonPrimitive?.content)
    }

    @Test
    fun askMailReturnsNullForBlankAnswerButStillSendsEmptyHistoryArray() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"answer":"  "}""")

        val answer = f.client.askMail("mail", "question")

        assertNull(answer)
        assertTrue(f.transport.singleRequest().rpcParams?.get("history")?.jsonArray?.isEmpty() == true)
    }

    @Test
    fun attachmentUrlEncodesEveryOpaqueValueAndToken() {
        val f = gatewayClientFixture(token = "token +/=?", url = "https://gateway.example/")
        val attachment = MailAttachment(
            id = "att /?#",
            filename = "계약서 #1.PDF",
            mimeType = "application/pdf; x=1",
            size = 10,
        )

        val url = f.client.attachmentUrl("msg /?#", attachment)

        assertTrue(url.startsWith("https://gateway.example/api/v1/miniapp/gmail/attachment?"))
        assertTrue("messageId=msg%20%2F%3F%23" in url)
        assertTrue("attachmentId=att%20%2F%3F%23" in url)
        assertTrue("filename=%EA%B3%84%EC%95%BD%EC%84%9C%20%231.PDF" in url)
        assertTrue("mimeType=application%2Fpdf%3B%20x%3D1" in url)
        assertTrue("clientToken=token%20%2B%2F%3D%3F" in url)
    }

    @Test
    fun fetchAttachmentBytesReturnsBinaryBodyAndUsesAuthedUrl() = runTest {
        val f = gatewayClientFixture(token = "attachment-token")
        val bytes = "binary-payload".encodeToByteArray()
        f.transport.enqueue(
            GatewayHttpHarness.Reply(
                body = bytes.decodeToString(),
                contentType = ContentType.Application.OctetStream.toString(),
            ),
        )
        val attachment = MailAttachment("att", "file.bin", "application/octet-stream", bytes.size)

        val loaded = f.client.fetchAttachmentBytes("message", attachment)

        assertContentEquals(bytes, loaded)
        val request = f.transport.singleRequest()
        assertEquals("GET", request.method.value)
        assertTrue("messageId=message" in request.url)
        assertTrue("attachmentId=att" in request.url)
        assertTrue("clientToken=attachment-token" in request.url)
    }

    @Test
    fun fetchAttachmentBytesRejectsHttpErrorBody() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueue(
            GatewayHttpHarness.Reply(
                body = "proxy error",
                status = HttpStatusCode.BadGateway,
                contentType = ContentType.Text.Plain.toString(),
            ),
        )
        val attachment = MailAttachment("att", "file.bin", "application/octet-stream", 1)

        val loaded = f.client.fetchAttachmentBytes("message", attachment)

        assertNull(loaded)
    }

    @Test
    fun fetchAttachmentBytesSkipsNetworkWithoutToken() = runTest {
        val f = gatewayClientFixture(token = "")
        val attachment = MailAttachment("att", "file.bin", "application/octet-stream", 1)

        val loaded = f.client.fetchAttachmentBytes("message", attachment)

        assertNull(loaded)
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun fetchAttachmentBytesPropagatesCancellation() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel attachment"))
        val attachment = MailAttachment("att", "file.bin", "application/octet-stream", 1)

        val failure = assertFailsWith<CancellationException> {
            f.client.fetchAttachmentBytes("message", attachment)
        }

        assertEquals("cancel attachment", failure.message)
    }

    @Test
    fun fetchAttachmentBytesDropsOldCredentialResponse() = runTest {
        val f = gatewayClientFixture(token = "old-token", url = "https://old.example")
        val gate = CompletableDeferred<Unit>()
        f.transport.enqueue(
            GatewayHttpHarness.Reply(
                body = "private bytes",
                contentType = ContentType.Application.OctetStream.toString(),
                gate = gate,
            ),
        )
        val attachment = MailAttachment("att", "file.bin", "application/octet-stream", 13)
        val pending = async { f.client.fetchAttachmentBytes("message", attachment) }
        runCurrent()

        f.client.onCredentialsChanged("https://new.example", "new-token")
        gate.complete(Unit)

        assertNull(pending.await())
        assertTrue("clientToken=old-token" in f.transport.singleRequest().url)
    }

    @Test
    fun fetchSenderContextMapsFallbackNameRecentWindowAndWikiFacts() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "sender":"fallback@example.com",
                "displayName":"",
                "email":"fallback@example.com",
                "recent":{"count":17,"lastReceivedAt":"now","windowDays":30,"truncated":true},
                "wikiHits":[
                    {"path":"people/a.md","title":"","summary":"A summary","category":"people"},
                    {"path":"people/b.md","title":"B","summary":"B summary","category":"people"}
                ],
                "wikiFacts":"대표이사 · 광주"
            }
            """.trimIndent(),
        )

        val context = f.client.fetchSenderContext("fallback@example.com")

        val params = f.transport.singleRequest().requireRpc("miniapp.mail.sender_context")
        assertEquals("fallback@example.com", params["sender"]?.jsonPrimitive?.content)
        assertEquals("fallback@example.com", context?.displayName)
        assertEquals("fallback@example.com", context?.email)
        assertEquals(17, context?.recentCount)
        assertEquals(30, context?.windowDays)
        assertEquals(listOf("people/a.md", "B"), context?.wikiHits?.map { it.title })
        assertEquals(listOf("A summary", "B summary"), context?.wikiHits?.map { it.summary })
        assertEquals("대표이사 · 광주", context?.wikiFacts)
    }

    @Test
    fun fetchRecentFromSenderSkipsBlankEmailWithoutNetwork() = runTest {
        val f = gatewayClientFixture()

        val rows = f.client.fetchRecentFromSender(" \t ")

        assertTrue(rows.isEmpty())
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun fetchRecentFromSenderBuildsQuotedQueryAndAppliesReadOverlay() = runTest {
        val f = gatewayClientFixture()
        f.client.locallyReadMailIds += "read"
        f.transport.enqueueRpc(
            """{
                "messages":[
                    {"id":"read","from":"a@example.com","isUnread":true},
                    {"id":"new","from":"a@example.com","isUnread":true}
                ]
            }
            """.trimIndent(),
        )

        val rows = f.client.fetchRecentFromSender("a+b@example.com", limit = Int.MAX_VALUE)

        val params = f.transport.singleRequest().requireRpc("miniapp.mail.list_recent")
        assertEquals("from:\"a+b@example.com\"", params["query"]?.jsonPrimitive?.content)
        assertEquals(Int.MAX_VALUE, params["limit"]?.jsonPrimitive?.content?.toInt())
        assertEquals(listOf(false, true), rows.map { it.unread })
    }

    @Test
    fun mailRpcCancellationPropagatesWithoutPublishingReplacementList() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"messages":[{"id":"stable"}]}""")
        f.transport.enqueueRpc(emptyNativeStatus)
        f.client.refreshMail("stable")
        f.transport.enqueueFailure(CancellationException("cancel mail"))

        val failure = assertFailsWith<CancellationException> {
            f.client.refreshMail("replacement")
        }

        assertEquals("cancel mail", failure.message)
        assertEquals(listOf("stable"), f.client.denebMail.value.map { it.id })
        assertEquals("stable", f.client.denebMailActiveQuery)
    }
}
