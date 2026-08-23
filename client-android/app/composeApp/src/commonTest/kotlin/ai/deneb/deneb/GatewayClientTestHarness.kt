package ai.deneb.deneb

import ai.deneb.data.AppSettings
import ai.deneb.data.SmsDraftStore
import ai.deneb.sms.SmsSender
import com.russhwolf.settings.MapSettings
import io.ktor.client.HttpClient
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.respond
import io.ktor.client.plugins.contentnegotiation.ContentNegotiation
import io.ktor.client.request.HttpRequestData
import io.ktor.http.ContentType
import io.ktor.http.Headers
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpMethod
import io.ktor.http.HttpStatusCode
import io.ktor.http.content.OutgoingContent
import io.ktor.http.content.TextContent
import io.ktor.http.headersOf
import io.ktor.serialization.kotlinx.json.json
import io.ktor.utils.io.ByteReadChannel
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.yield
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put

internal class GatewayHttpHarness {
    data class Request(
        val url: String,
        val method: HttpMethod,
        val headers: Headers,
        val body: String,
        val bodyContentType: ContentType?,
    ) {
        val jsonBody: JsonObject?
            get() = runCatching { TEST_JSON.parseToJsonElement(body).jsonObject }.getOrNull()

        val rpcMethod: String?
            get() = jsonBody?.get("method")?.jsonPrimitive?.content

        val rpcParams: JsonObject?
            get() = jsonBody?.get("params") as? JsonObject

        fun header(name: String): String? = headers[name]
    }

    data class Reply(
        val body: String,
        val status: HttpStatusCode = HttpStatusCode.OK,
        val contentType: String = ContentType.Application.Json.toString(),
        val headers: Headers = Headers.Empty,
        val gate: CompletableDeferred<Unit>? = null,
        val failure: Throwable? = null,
    )

    private val queued = ArrayDeque<suspend (Request) -> Reply>()
    val requests = mutableListOf<Request>()
    var fallback: (suspend (Request) -> Reply)? = null

    private val engine = MockEngine { raw ->
        val request = Request(
            url = raw.url.toString(),
            method = raw.method,
            headers = raw.headers,
            body = raw.body.bodyText(),
            bodyContentType = raw.body.contentType,
        )
        requests += request
        // Session focus is a fire-and-forget sidecar on switchSession. Auto-reply
        // so it never consumes a queued transcript/recent payload the test body
        // actually arranged.
        val reply = if (request.rpcMethod == "miniapp.sessions.focus") {
            Reply(body = """{"ok":true,"payload":{"sessionKey":""}}""")
        } else {
            val producer = if (queued.isNotEmpty()) queued.removeFirst() else fallback
            producer?.invoke(request)
                ?: error("No queued response for ${request.method.value} ${request.url}")
        }
        reply.gate?.await()
        reply.failure?.let { throw it }
        respond(
            content = ByteReadChannel(reply.body),
            status = reply.status,
            headers = headersOf(HttpHeaders.ContentType, reply.contentType),
        )
    }

    val httpClient: HttpClient = HttpClient(engine) {
        install(ContentNegotiation) {
            json(TEST_JSON)
        }
    }

    fun enqueue(reply: Reply) {
        queued += { reply }
    }

    fun enqueue(block: suspend (Request) -> Reply) {
        queued += block
    }

    fun enqueueJson(
        body: String,
        status: HttpStatusCode = HttpStatusCode.OK,
        gate: CompletableDeferred<Unit>? = null,
    ) {
        enqueue(Reply(body = body, status = status, gate = gate))
    }

    fun enqueueRpc(
        payload: String = "{}",
        ok: Boolean = true,
        status: HttpStatusCode = HttpStatusCode.OK,
        gate: CompletableDeferred<Unit>? = null,
    ) {
        enqueueJson(
            body = """{"ok":$ok,"payload":$payload}""",
            status = status,
            gate = gate,
        )
    }

    fun enqueueRpc(payload: JsonElement, ok: Boolean = true) {
        enqueueRpc(payload = payload.toString(), ok = ok)
    }

    fun enqueueWrite(
        ok: Boolean,
        message: String = "",
        status: HttpStatusCode = HttpStatusCode.OK,
    ) {
        val error = if (message.isBlank()) {
            JsonNull
        } else {
            buildJsonObject {
                put("code", "rejected")
                put("message", message)
            }
        }
        enqueueJson(
            buildJsonObject {
                put("ok", ok)
                put("error", error)
            }.toString(),
            status,
        )
    }

    fun enqueueSse(
        text: String,
        status: HttpStatusCode = HttpStatusCode.OK,
        gate: CompletableDeferred<Unit>? = null,
    ) {
        enqueue(
            Reply(
                body = text,
                status = status,
                contentType = ContentType.Text.EventStream.toString(),
                gate = gate,
            ),
        )
    }

    fun enqueueFailure(failure: Throwable) {
        enqueue(Reply(body = "", failure = failure))
    }

    fun clearRequests() {
        requests.clear()
    }

    fun singleRequest(): Request = requests.single()

    fun requestMethods(): List<String?> = requests.map { it.rpcMethod }

    fun lastRequest(): Request = requests.last()

    suspend fun awaitRequestCount(count: Int) {
        while (requests.size < count) yield()
    }

    private fun OutgoingContent.bodyText(): String = when (this) {
        is TextContent -> text
        is OutgoingContent.ByteArrayContent -> bytes().decodeToString()
        else -> ""
    }

    companion object {
        val TEST_JSON = Json {
            ignoreUnknownKeys = true
            isLenient = true
            coerceInputValues = true
        }
    }
}

internal data class GatewayClientFixture(
    val settings: AppSettings,
    val transport: GatewayHttpHarness,
    val client: DenebGatewayClient,
)

/** In-memory wiki-mirror storage so tests never read — or wipe (foreign-owner
 *  load path) — the machine's real mirror files. Pass a prior fixture's
 *  instance to simulate a restart over the same mirror disk. */
internal class MemoryWikiMirrorFiles : WikiMirrorFiles {
    val files = mutableMapOf<String, String>()

    override fun read(name: String): String? = files[name]

    override fun write(name: String, content: String) {
        files[name] = content
    }

    override fun delete(name: String) {
        files.remove(name)
    }
}

internal fun gatewayClientFixture(
    token: String = "test-token",
    url: String = "https://gateway.example",
    transport: GatewayHttpHarness = GatewayHttpHarness(),
    // Pass a prior fixture's settings to simulate a process restart over the same
    // disk (cache-then-network seeding); default is a fresh store.
    settings: AppSettings = AppSettings(MapSettings()),
    wikiMirrorFiles: WikiMirrorFiles = MemoryWikiMirrorFiles(),
): GatewayClientFixture {
    settings.settings.putString(DenebGatewayClient.KEY_TOKEN, token)
    settings.settings.putString(DenebGatewayClient.KEY_URL, url)
    val client = DenebGatewayClient(
        settings,
        SmsDraftStore(settings),
        SmsSender(),
        transport.httpClient,
        wikiMirrorFiles,
    )
    return GatewayClientFixture(settings, transport, client)
}

internal fun GatewayHttpHarness.Request.requireRpc(method: String): JsonObject {
    check(this.rpcMethod == method) { "Expected $method, got $rpcMethod" }
    return checkNotNull(rpcParams)
}

internal fun gatewayRpcReply(
    payload: String = "{}",
    ok: Boolean = true,
    status: HttpStatusCode = HttpStatusCode.OK,
): GatewayHttpHarness.Reply = GatewayHttpHarness.Reply(
    body = """{"ok":$ok,"payload":$payload}""",
    status = status,
)
