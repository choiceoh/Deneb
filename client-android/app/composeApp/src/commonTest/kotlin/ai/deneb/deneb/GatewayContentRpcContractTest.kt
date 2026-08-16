package ai.deneb.deneb

import ai.deneb.deneb.generated.OrgNodeOut
import io.ktor.http.HttpStatusCode
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonPrimitive
import kotlin.io.encoding.Base64
import kotlin.io.encoding.ExperimentalEncodingApi
import kotlin.test.Test
import kotlin.test.assertContentEquals
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * End-to-end request/response contracts for the read-oriented mini-app domains.
 *
 * These tests deliberately go through Ktor's [GatewayHttpHarness] instead of
 * calling the mapping helpers directly. That keeps the native client honest
 * about wire method names, exact parameter keys, URL encoding, authentication,
 * default-value coercion, and malformed/failed response handling.
 */
class GatewayContentRpcContractTest {
    @Test
    fun filesListSendsRequestedDisplayPathAndMapsEveryEntryField() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "entries":[{
                    "tag":"file",
                    "name":"Report.PDF",
                    "pathDisplay":"Projects/Report.PDF",
                    "pathLower":"/projects/report.pdf",
                    "id":"file-7",
                    "size":9223372036854775807,
                    "serverModified":"2026-07-11T01:02:03Z"
                }],
                "path":"Projects"
            }
            """.trimIndent(),
        )

        val rows = f.client.filesList("Projects")

        val request = f.transport.singleRequest()
        assertEquals("miniapp.files.list", request.rpcMethod)
        assertEquals("Projects", request.rpcParams?.get("path")?.jsonPrimitive?.content)
        val row = assertNotNull(rows).single()
        assertEquals("Report.PDF", row.name)
        assertEquals("Projects/Report.PDF", row.pathDisplay)
        assertEquals("/projects/report.pdf", row.pathLower)
        assertEquals("file-7", row.id)
        assertEquals(Long.MAX_VALUE, row.size)
        assertEquals("2026-07-11T01:02:03Z", row.modified)
        assertFalse(row.isFolder)
    }

    @Test
    fun filesListUsesNameWhenGatewayOmitsDisplayPath() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "entries":[{
                    "tag":"folder",
                    "name":"자료",
                    "pathDisplay":"",
                    "pathLower":"/자료",
                    "id":"dir-1"
                }]
            }
            """.trimIndent(),
        )

        val row = assertNotNull(f.client.filesList()).single()

        assertEquals("", f.transport.singleRequest().rpcParams?.get("path")?.jsonPrimitive?.content)
        assertEquals("자료", row.pathDisplay)
        assertTrue(row.isFolder)
        assertEquals(0L, row.size)
        assertEquals("", row.modified)
    }

    @Test
    fun filesListReturnsEmptyListForSuccessfulEmptyFolder() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"entries":[],"path":"Empty"}""")

        val rows = f.client.filesList("Empty")

        assertNotNull(rows)
        assertTrue(rows.isEmpty())
        assertEquals("Empty", f.transport.singleRequest().rpcParams?.get("path")?.jsonPrimitive?.content)
    }

    @Test
    fun filesListReturnsNullForMalformedPayloadInsteadOfPretendingFolderIsEmpty() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"entries":"not-an-array"}""")

        val rows = f.client.filesList("Broken")

        assertNull(rows)
        assertEquals("miniapp.files.list", f.transport.singleRequest().rpcMethod)
    }

    @Test
    fun filesSearchNameModeOmitsOptionalFlags() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"entries":[]}""")

        val rows = f.client.filesSearch("분기 보고서")

        assertNotNull(rows)
        val params = f.transport.singleRequest().requireRpc("miniapp.files.search")
        assertEquals("분기 보고서", params["query"]?.jsonPrimitive?.content)
        assertFalse(params.containsKey("content"))
        assertFalse(params.containsKey("semantic"))
    }

    @Test
    fun filesSearchContentModeSendsOnlyContentFlag() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"entries":[]}""")

        f.client.filesSearch("계약 위약금", content = true)

        val params = f.transport.singleRequest().requireRpc("miniapp.files.search")
        assertEquals(true, params["content"]?.jsonPrimitive?.content?.toBoolean())
        assertFalse(params.containsKey("semantic"))
    }

    @Test
    fun filesSearchSemanticModeWinsWhenBothModesRequested() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"entries":[]}""")

        f.client.filesSearch("비슷한 투자 검토", content = true, semantic = true)

        val params = f.transport.singleRequest().requireRpc("miniapp.files.search")
        assertEquals(true, params["semantic"]?.jsonPrimitive?.content?.toBoolean())
        assertFalse(params.containsKey("content"))
    }

    @Test
    fun filesShareReturnsSignedUrlAndPreservesExactCasePath() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"url":"https://download.example/signed?id=7"}""")

        val url = f.client.filesShare("Projects/Report.PDF")

        assertEquals("https://download.example/signed?id=7", url)
        val params = f.transport.singleRequest().requireRpc("miniapp.files.share")
        assertEquals("Projects/Report.PDF", params["path"]?.jsonPrimitive?.content)
    }

    @Test
    fun filesShareRejectsBlankGatewayUrl() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"url":"   "}""")

        val url = f.client.filesShare("missing.txt")

        assertNull(url)
        assertEquals("miniapp.files.share", f.transport.singleRequest().rpcMethod)
    }

    @Test
    fun filesDeleteForwardsPathAndGatewayValidationMessage() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = false, message = "folder is not empty")

        val error = f.client.filesDelete("Archive")

        assertEquals("folder is not empty", error)
        val params = f.transport.singleRequest().requireRpc("miniapp.files.delete")
        assertEquals("Archive", params["path"]?.jsonPrimitive?.content)
    }

    @Test
    fun filesMkdirForwardsNestedPathAndAcceptsWrite() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)

        val error = f.client.filesMkdir("Projects/2026/Q3")

        assertNull(error)
        val params = f.transport.singleRequest().requireRpc("miniapp.files.mkdir")
        assertEquals("Projects/2026/Q3", params["path"]?.jsonPrimitive?.content)
    }

    @Test
    fun filesMovePreservesSourceAndDestinationSeparately() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)

        val error = f.client.filesMove("Inbox/초안.docx", "Projects/최종안.docx")

        assertNull(error)
        val params = f.transport.singleRequest().requireRpc("miniapp.files.move")
        assertEquals("Inbox/초안.docx", params["src"]?.jsonPrimitive?.content)
        assertEquals("Projects/최종안.docx", params["dst"]?.jsonPrimitive?.content)
        assertEquals(2, params.size)
    }

    @OptIn(ExperimentalEncodingApi::class)
    @Test
    fun filesUploadBase64EncodesBinaryBytesAndMapsStoredEntry() = runTest {
        val f = gatewayClientFixture()
        val bytes = byteArrayOf(0, 1, 2, 0x7f, 0x80.toByte(), 0xff.toByte())
        f.transport.enqueueRpc(
            """{
                "entry":{
                    "tag":"file",
                    "name":"binary.bin",
                    "pathDisplay":"Uploads/binary.bin",
                    "pathLower":"/uploads/binary.bin",
                    "id":"stored-1",
                    "size":6,
                    "serverModified":"now"
                }
            }
            """.trimIndent(),
        )

        val row = f.client.filesUpload("Uploads/binary.bin", bytes, "application/octet-stream")

        val params = f.transport.singleRequest().requireRpc("miniapp.files.upload")
        assertEquals("Uploads/binary.bin", params["path"]?.jsonPrimitive?.content)
        assertEquals("application/octet-stream", params["mimeType"]?.jsonPrimitive?.content)
        assertContentEquals(bytes, Base64.decode(params["dataBase64"]?.jsonPrimitive?.content.orEmpty()))
        assertEquals("stored-1", row?.id)
        assertEquals(6L, row?.size)
        assertEquals("Uploads/binary.bin", row?.pathDisplay)
    }

    @Test
    fun filesUploadSendsExplicitBlankMimeTypeByDefault() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"entry":{"name":"note","tag":"file"}}""")

        val row = f.client.filesUpload("note", byteArrayOf())

        assertEquals("note", row?.name)
        val params = f.transport.singleRequest().requireRpc("miniapp.files.upload")
        assertEquals("", params["mimeType"]?.jsonPrimitive?.content)
        assertEquals("", params["dataBase64"]?.jsonPrimitive?.content)
    }

    @Test
    fun filesDownloadUrlEncodesPathAndTokenWithoutLowercasing() {
        val f = gatewayClientFixture(
            url = "https://gateway.example/root/",
            token = "token +/=?",
        )

        val url = f.client.filesDownloadUrl("Projects/Q3 Report #1.PDF")

        assertTrue(url.startsWith("https://gateway.example/root/api/v1/files/download?"))
        assertTrue("path=Projects%2FQ3%20Report%20%231.PDF" in url)
        assertTrue("clientToken=token%20%2B%2F%3D%3F" in url)
        assertFalse("q3%20report" in url)
    }

    @Test
    fun filesDownloadTextReturnsBodyAndSendsHeaderCredential() = runTest {
        val f = gatewayClientFixture(token = "download-secret")
        f.transport.enqueue(
            GatewayHttpHarness.Reply(
                body = "첫째 줄\n둘째 줄",
                contentType = "text/plain; charset=utf-8",
            ),
        )

        val body = f.client.filesDownloadText("Notes/업무.md")

        assertEquals("첫째 줄\n둘째 줄", body)
        val request = f.transport.singleRequest()
        assertEquals("GET", request.method.value)
        assertTrue("path=Notes%2F%EC%97%85%EB%AC%B4.md" in request.url)
        assertEquals("download-secret", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
    }

    @Test
    fun filesDownloadTextTruncatesAtConfiguredCharacterBudget() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueue(
            GatewayHttpHarness.Reply(
                body = "가나다라마바사",
                contentType = "text/plain; charset=utf-8",
            ),
        )

        val body = f.client.filesDownloadText("large.txt", maxBytes = 4)

        assertEquals("가나다라\n\n…(이하 생략 — 파일이 너무 큽니다)", body)
    }

    @Test
    fun filesDownloadTextDoesNotTruncateAtExactBoundary() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueue(
            GatewayHttpHarness.Reply(
                body = "1234",
                contentType = "text/plain; charset=utf-8",
            ),
        )

        val body = f.client.filesDownloadText("exact.txt", maxBytes = 4)

        assertEquals("1234", body)
        assertFalse(body.orEmpty().contains("이하 생략"))
    }

    @Test
    fun filesDownloadTextSkipsNetworkWithoutToken() = runTest {
        val f = gatewayClientFixture(token = "")

        val body = f.client.filesDownloadText("private.txt")

        assertNull(body)
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun filesDownloadTextRejectsHttpFailureBody() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueue(
            GatewayHttpHarness.Reply(
                body = "proxy error page",
                status = HttpStatusCode.BadGateway,
                contentType = "text/plain",
            ),
        )

        val body = f.client.filesDownloadText("unavailable.txt")

        assertNull(body)
        assertEquals(1, f.transport.requests.size)
    }

    @Test
    fun filesDownloadTextPropagatesCancellation() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel download"))

        val failure = assertFailsWith<CancellationException> {
            f.client.filesDownloadText("slow.txt")
        }

        assertEquals("cancel download", failure.message)
    }

    @Test
    fun fetchNotebooksMapsCompleteSummaryListAndUsesEmptyParams() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "notebooks":[{
                    "id":"nb-1",
                    "name":"남도에코 실사",
                    "description":"투자 검토 근거",
                    "dealRef":"deal:namdo",
                    "projectRefs":["wiki/projects/namdo.md","deal:namdo"],
                    "sourceCount":17,
                    "updated":9223372036854775807
                }]
            }
            """.trimIndent(),
        )

        val notebooks = f.client.fetchNotebooks()

        val request = f.transport.singleRequest()
        assertEquals("miniapp.notebook.list", request.rpcMethod)
        assertTrue(request.rpcParams?.isEmpty() == true)
        val notebook = assertNotNull(notebooks).single()
        assertEquals("nb-1", notebook.id)
        assertEquals("남도에코 실사", notebook.name)
        assertEquals("deal:namdo", notebook.dealRef)
        assertEquals(2, notebook.projectRefs.size)
        assertEquals(17, notebook.sourceCount)
        assertEquals(Long.MAX_VALUE, notebook.updated)
    }

    @Test
    fun fetchNotebooksReturnsEmptyListForValidEmptyPayload() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"notebooks":[]}""")

        val notebooks = f.client.fetchNotebooks()

        assertNotNull(notebooks)
        assertTrue(notebooks.isEmpty())
    }

    @Test
    fun fetchNotebooksReturnsNullForMalformedList() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"notebooks":{"id":"wrong"}}""")

        val notebooks = f.client.fetchNotebooks()

        assertNull(notebooks)
    }

    @Test
    fun fetchNotebookSendsIdAndMapsPinnedSources() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "id":"nb-2",
                "name":"계약 검토",
                "description":"핵심 조항",
                "dealRef":"deal:contract",
                "mode":"evidence",
                "updated":1234,
                "sources":[{
                    "cite":"S1",
                    "kind":"mail",
                    "ref":"mail-7",
                    "title":"수정 계약서",
                    "text":"위약금 조항"
                }]
            }
            """.trimIndent(),
        )

        val notebook = f.client.fetchNotebook("nb-2")

        val params = f.transport.singleRequest().requireRpc("miniapp.notebook.get")
        assertEquals("nb-2", params["id"]?.jsonPrimitive?.content)
        assertEquals("계약 검토", notebook?.name)
        assertEquals("evidence", notebook?.mode)
        val source = assertNotNull(notebook).sources.single()
        assertEquals("S1", source.cite)
        assertEquals("mail", source.kind)
        assertEquals("mail-7", source.ref)
        assertEquals("수정 계약서", source.title)
        assertEquals("위약금 조항", source.text)
    }

    @Test
    fun fetchNotebookReturnsNullForMissingPayload() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("""{"ok":true}""")

        val notebook = f.client.fetchNotebook("missing")

        assertNull(notebook)
        assertEquals("missing", f.transport.singleRequest().rpcParams?.get("id")?.jsonPrimitive?.content)
    }

    @Test
    fun fetchPromptsMapsRowsAndIgnoresCountMismatch() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "count":99,
                "prompts":[{
                    "id":"main.system",
                    "title":"시스템",
                    "description":"기본 시스템 프롬프트",
                    "category":"runtime",
                    "editable":true,
                    "overridden":true,
                    "updatedAtMs":1700000000000
                }]
            }
            """.trimIndent(),
        )

        val prompts = f.client.fetchPrompts()

        assertEquals("miniapp.prompts.list", f.transport.singleRequest().rpcMethod)
        assertTrue(f.transport.singleRequest().rpcParams?.isEmpty() == true)
        val prompt = assertNotNull(prompts).single()
        assertEquals("main.system", prompt.id)
        assertEquals("시스템", prompt.title)
        assertEquals("runtime", prompt.category)
        assertTrue(prompt.editable)
        assertTrue(prompt.overridden)
        assertEquals(1_700_000_000_000L, prompt.updatedAtMs)
    }

    @Test
    fun fetchPromptSendsExactIdAndMapsEditableDetail() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "id":"assistant.soul",
                "title":"Soul",
                "description":"Identity",
                "category":"workspace",
                "text":"custom body",
                "defaultText":"default body",
                "editable":true,
                "overridden":true,
                "updatedAtMs":42
            }
            """.trimIndent(),
        )

        val prompt = f.client.fetchPrompt("assistant.soul")

        val params = f.transport.singleRequest().requireRpc("miniapp.prompts.get")
        assertEquals("assistant.soul", params["id"]?.jsonPrimitive?.content)
        assertEquals(1, params.size)
        assertEquals("custom body", prompt?.text)
        assertEquals("default body", prompt?.defaultText)
        assertEquals(42L, prompt?.updatedAtMs)
    }

    @Test
    fun updatePromptPreservesWhitespaceAndUnicodeBody() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"id":"p-1","text":"저장됨","overridden":true}""")
        val text = "  첫 줄\n\n- 둘째 줄 🚀\n"

        val prompt = f.client.updatePrompt("p-1", text)

        val params = f.transport.singleRequest().requireRpc("miniapp.prompts.update")
        assertEquals("p-1", params["id"]?.jsonPrimitive?.content)
        assertEquals(text, params["text"]?.jsonPrimitive?.content)
        assertEquals(2, params.size)
        assertEquals("저장됨", prompt?.text)
        assertTrue(prompt?.overridden == true)
    }

    @Test
    fun resetPromptUsesIdOnlyAndReturnsDefaultDetail() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"id":"p-2","text":"default","overridden":false}""")

        val prompt = f.client.resetPrompt("p-2")

        val params = f.transport.singleRequest().requireRpc("miniapp.prompts.reset")
        assertEquals(setOf("id"), params.keys)
        assertEquals("p-2", params["id"]?.jsonPrimitive?.content)
        assertEquals("default", prompt?.text)
        assertFalse(prompt?.overridden ?: true)
    }

    @Test
    fun promptOperationsReturnNullOnRejectedEnvelope() = runTest {
        val f = gatewayClientFixture()
        repeat(4) { f.transport.enqueueRpc(payload = "{}", ok = false) }

        val list = f.client.fetchPrompts()
        val get = f.client.fetchPrompt("p")
        val update = f.client.updatePrompt("p", "body")
        val reset = f.client.resetPrompt("p")

        assertNull(list)
        assertNull(get)
        assertNull(update)
        assertNull(reset)
        assertEquals(
            listOf(
                "miniapp.prompts.list",
                "miniapp.prompts.get",
                "miniapp.prompts.update",
                "miniapp.prompts.reset",
            ),
            f.transport.requestMethods(),
        )
    }

    @Test
    fun fetchTopicDocUsesCurrentTopicWithoutLeakingClientPath() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "key":"client-main-topic",
                "name":"업무 주제",
                "content":"# 지식\n본문",
                "size":17,
                "modified":"2026-07-11T00:00:00Z"
            }
            """.trimIndent(),
        )

        val doc = f.client.fetchTopicDoc()

        val request = f.transport.singleRequest()
        assertEquals("miniapp.topicdocs.read_current", request.rpcMethod)
        assertTrue(request.rpcParams?.isEmpty() == true)
        assertEquals("client-main-topic", doc?.key)
        assertEquals("업무 주제", doc?.name)
        assertEquals("# 지식\n본문", doc?.content)
        assertEquals(17L, doc?.size)
        assertEquals("2026-07-11T00:00:00Z", doc?.modified)
    }

    @Test
    fun saveTopicDocSendsContentAndDeferredApplyFlag() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "key":"topic",
                "name":"Topic",
                "size":8,
                "modified":"now",
                "applied":false
            }
            """.trimIndent(),
        )

        val saved = f.client.saveTopicDoc("# Topic\n")

        val params = f.transport.singleRequest().requireRpc("miniapp.topicdocs.write_current")
        assertEquals("# Topic\n", params["content"]?.jsonPrimitive?.content)
        assertEquals(false, params["applyNow"]?.jsonPrimitive?.content?.toBoolean())
        assertEquals(2, params.size)
        assertEquals("topic", saved?.key)
        assertFalse(saved?.applied ?: true)
    }

    @Test
    fun saveTopicDocCanRequestImmediateApplication() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"key":"topic","applied":true,"size":4}""")

        val saved = f.client.saveTopicDoc("body", applyNow = true)

        val params = f.transport.singleRequest().requireRpc("miniapp.topicdocs.write_current")
        assertEquals(true, params["applyNow"]?.jsonPrimitive?.content?.toBoolean())
        assertEquals("body", params["content"]?.jsonPrimitive?.content)
        assertTrue(saved?.applied == true)
    }

    @Test
    fun fetchWormholeStatusMapsFeatureFlagsAndModels() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "reachable":true,
                "listen":"127.0.0.1:11435",
                "localOnly":true,
                "effortRouting":false,
                "auto":["reasoning","vision"],
                "models":[{
                    "name":"provider/model",
                    "protocol":"openai",
                    "local":false,
                    "thinking":true,
                    "source":"wormhole",
                    "keyHealth":"ok"
                }]
            }
            """.trimIndent(),
        )

        val status = f.client.fetchWormholeStatus()

        assertEquals("miniapp.wormhole.status", f.transport.singleRequest().rpcMethod)
        assertTrue(f.transport.singleRequest().rpcParams?.isEmpty() == true)
        assertTrue(status?.reachable == true)
        assertEquals("127.0.0.1:11435", status?.listen)
        assertTrue(status?.localOnly == true)
        assertFalse(status?.effortRouting ?: true)
        assertEquals(listOf("reasoning", "vision"), status?.auto)
        val model = assertNotNull(status).models.single()
        assertEquals("provider/model", model.name)
        assertEquals("openai", model.protocol)
        assertFalse(model.local)
        assertTrue(model.thinking)
        assertEquals("wormhole", model.source)
        assertEquals("ok", model.keyHealth)
    }

    @Test
    fun setWormholeFeatureSendsExactFeatureAndBoolean() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("{}")

        val changed = f.client.setWormholeFeature("effortRouting", enabled = true)

        assertTrue(changed)
        val params = f.transport.singleRequest().requireRpc("miniapp.wormhole.set_feature")
        assertEquals("effortRouting", params["feature"]?.jsonPrimitive?.content)
        assertEquals(true, params["enabled"]?.jsonPrimitive?.content?.toBoolean())
        assertEquals(2, params.size)
    }

    @Test
    fun setWormholeFeatureReturnsFalseForRejectedOrMalformedResponses() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(payload = "{}", ok = false)
        f.transport.enqueueJson("not-json")

        val rejected = f.client.setWormholeFeature("localOnly", enabled = true)
        val malformed = f.client.setWormholeFeature("localOnly", enabled = false)

        assertFalse(rejected)
        assertFalse(malformed)
        assertEquals(2, f.transport.requests.size)
    }

    @Test
    fun setWormholeKeyPreservesSecretAndParsesProbeOutcome() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"ok":true,"valid":false,"status":401}""")

        val result = f.client.setWormholeKey("cloud-model", "sk-secret-한글")

        val params = f.transport.singleRequest().requireRpc("miniapp.wormhole.set_key")
        assertEquals("cloud-model", params["model"]?.jsonPrimitive?.content)
        assertEquals("sk-secret-한글", params["key"]?.jsonPrimitive?.content)
        assertEquals(2, params.size)
        assertTrue(result?.ok == true)
        assertFalse(result?.valid ?: true)
        assertEquals(401, result?.status)
    }

    @Test
    fun setWormholeKeyUsesSafeBooleanDefaultsAndAcceptsLenientNumericStatus() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"ok":"yes","valid":1,"status":"200","future":true}""")

        val result = f.client.setWormholeKey("model", "key")

        assertNotNull(result)
        assertFalse(result.ok)
        assertFalse(result.valid)
        assertEquals(200, result.status)
    }

    @Test
    fun setWormholeKeyReturnsNullWhenGatewayRejectsRotation() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(payload = "{}", ok = false)

        val result = f.client.setWormholeKey("literal-key-model", "new-key")

        assertNull(result)
    }

    @Test
    fun fetchOrgMapsFlatHierarchyAndClassificationFields() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "nodes":[
                    {
                        "id":"root",
                        "name":"기획조정실",
                        "type":"department",
                        "parentId":"",
                        "lane":"planning",
                        "members":[],
                        "keywords":["기획","전략"],
                        "companies":["Deneb Corp"]
                    },
                    {
                        "id":"team-1",
                        "name":"1팀",
                        "type":"team",
                        "parentId":"root",
                        "lane":"planning-1",
                        "members":[],
                        "keywords":["투자"],
                        "companies":[]
                    }
                ]
            }
            """.trimIndent(),
        )

        val tree = f.client.fetchOrg()

        assertEquals("miniapp.org.get", f.transport.singleRequest().rpcMethod)
        assertTrue(f.transport.singleRequest().rpcParams?.isEmpty() == true)
        assertEquals(2, tree?.nodes?.size)
        val root = tree?.nodes?.first()
        assertEquals("root", root?.id)
        assertEquals("planning", root?.lane)
        assertEquals(listOf("기획", "전략"), root?.keywords)
        assertEquals(listOf("Deneb Corp"), root?.companies)
        val child = tree?.nodes?.last()
        assertEquals("team-1", child?.id)
        assertEquals("root", child?.parentId)
    }

    @Test
    fun saveOrgAlwaysSerializesNodesKeyForEmptyChart() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)

        val error = f.client.saveOrg(emptyList())

        assertNull(error)
        val params = f.transport.singleRequest().requireRpc("miniapp.org.save")
        assertTrue(params.containsKey("nodes"))
        assertTrue(params["nodes"]?.jsonArray?.isEmpty() == true)
        assertEquals(1, params.size)
    }

    @Test
    fun saveOrgSerializesEveryClassificationFieldWithoutDroppingEmptyLists() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)
        val node = OrgNodeOut(
            id = "team",
            name = "사업팀",
            type = "team",
            parentId = "root",
            lane = "business",
            members = emptyList(),
            keywords = listOf("매출", "고객"),
            companies = listOf("A사", "B사"),
        )

        val error = f.client.saveOrg(listOf(node))

        assertNull(error)
        val params = f.transport.singleRequest().requireRpc("miniapp.org.save")
        val encoded = params["nodes"]?.jsonArray?.single()?.let { it as JsonObject }
        assertEquals("team", encoded?.get("id")?.jsonPrimitive?.content)
        assertEquals("사업팀", encoded?.get("name")?.jsonPrimitive?.content)
        assertEquals("team", encoded?.get("type")?.jsonPrimitive?.content)
        assertEquals("root", encoded?.get("parentId")?.jsonPrimitive?.content)
        assertEquals("business", encoded?.get("lane")?.jsonPrimitive?.content)
        assertEquals(listOf("매출", "고객"), encoded?.get("keywords")?.jsonArray?.map { it.jsonPrimitive.content })
        assertEquals(listOf("A사", "B사"), encoded?.get("companies")?.jsonArray?.map { it.jsonPrimitive.content })
    }

    @Test
    fun saveOrgReturnsValidationMessageWithoutMutatingInput() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = false, message = "parent does not exist")
        val nodes = listOf(
            OrgNodeOut(
                id = "orphan",
                name = "고아 노드",
                parentId = "missing",
            ),
        )

        val error = f.client.saveOrg(nodes)

        assertEquals("parent does not exist", error)
        assertEquals("orphan", nodes.single().id)
        assertEquals("missing", nodes.single().parentId)
    }

    @Test
    fun saveOrgAckParsesTypedAcknowledgement() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"saved":true,"nodeCount":3,"hasLanes":true}""")

        val ack = f.client.saveOrgAck(
            listOf(
                OrgNodeOut(id = "root", name = "본부"),
                OrgNodeOut(id = "a", name = "A팀", parentId = "root"),
                OrgNodeOut(id = "b", name = "B팀", parentId = "root"),
            ),
        )

        assertEquals("miniapp.org.save", f.transport.singleRequest().rpcMethod)
        assertTrue(ack?.saved == true)
        assertEquals(3, ack?.nodeCount)
        assertTrue(ack?.hasLanes == true)
    }

    @Test
    fun fetchDashboardMapsLanesInGatewayOrderWithoutRegrouping() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "lanes":[
                    {
                        "key":"planning-2",
                        "name":"2팀",
                        "items":[{
                            "title":"계약 검토",
                            "subtitle":"오늘 마감",
                            "source":"calendar",
                            "refType":"event",
                            "refId":"event-7",
                            "whenMs":1700000000000
                        }]
                    },
                    {"key":"unclassified","name":"미분류","items":[]}
                ]
            }
            """.trimIndent(),
        )

        val dashboard = f.client.fetchDashboardLanes()

        assertEquals("miniapp.dashboard.lanes", f.transport.singleRequest().rpcMethod)
        assertTrue(f.transport.singleRequest().rpcParams?.isEmpty() == true)
        assertEquals(listOf("planning-2", "unclassified"), dashboard?.lanes?.map { it.key })
        val item = dashboard?.lanes?.first()?.items?.single()
        assertEquals("계약 검토", item?.title)
        assertEquals("오늘 마감", item?.subtitle)
        assertEquals("calendar", item?.source)
        assertEquals("event", item?.refType)
        assertEquals("event-7", item?.refId)
        assertEquals(1_700_000_000_000L, item?.whenMs)
    }

    @Test
    fun readOnlyDomainCallsReturnNullOnHttpFailure() = runTest {
        val f = gatewayClientFixture()
        repeat(6) {
            f.transport.enqueueRpc(payload = "{}", status = HttpStatusCode.ServiceUnavailable)
        }

        val files = f.client.filesList()
        val notebooks = f.client.fetchNotebooks()
        val notebook = f.client.fetchNotebook("id")
        val topic = f.client.fetchTopicDoc()
        val wormhole = f.client.fetchWormholeStatus()
        val org = f.client.fetchOrg()

        assertNull(files)
        assertNull(notebooks)
        assertNull(notebook)
        assertNull(topic)
        assertNull(wormhole)
        assertNull(org)
        assertEquals(
            listOf(
                "miniapp.files.list",
                "miniapp.notebook.list",
                "miniapp.notebook.get",
                "miniapp.topicdocs.read_current",
                "miniapp.wormhole.status",
                "miniapp.org.get",
            ),
            f.transport.requestMethods(),
        )
    }

    @Test
    fun contentRpcCancellationIsNeverConvertedToEmptyDomainData() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel content rpc"))

        val failure = assertFailsWith<CancellationException> {
            f.client.fetchNotebooks()
        }

        assertEquals("cancel content rpc", failure.message)
        assertEquals(1, f.transport.requests.size)
    }
}
