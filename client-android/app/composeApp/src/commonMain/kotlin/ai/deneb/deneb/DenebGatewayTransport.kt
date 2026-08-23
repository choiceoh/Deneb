package ai.deneb.deneb

import ai.deneb.getBackgroundDispatcher
import ai.deneb.network.httpTeardownTolerantHandler
import io.ktor.client.HttpClient
import io.ktor.client.call.body
import io.ktor.client.plugins.timeout
import io.ktor.client.request.header
import io.ktor.client.request.post
import io.ktor.client.request.preparePost
import io.ktor.client.request.setBody
import io.ktor.client.statement.bodyAsChannel
import io.ktor.http.ContentType
import io.ktor.http.contentType
import io.ktor.http.isSuccess
import io.ktor.utils.io.ByteReadChannel
import io.ktor.utils.io.readByte
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlin.coroutines.CoroutineContext
import kotlin.uuid.ExperimentalUuidApi
import kotlin.uuid.Uuid

/**
 * Reads SSE lines without decoding a partial UTF-8 code point.
 *
 * Ktor's line reader may see a Korean character split across network buffers.
 * Accumulating bytes until the ASCII newline keeps the multibyte sequence intact.
 * Callback failures propagate; only a normal channel close ends the loop.
 */
internal suspend fun readSseLines(channel: ByteReadChannel, onLine: (String) -> Unit) {
    val line = ArrayList<Byte>(256)
    fun emit() {
        val text = line.toByteArray().decodeToString()
        line.clear()
        onLine(if (text.endsWith('\r')) text.dropLast(1) else text)
    }
    while (!channel.isClosedForRead) {
        val byte = try {
            channel.readByte()
        } catch (cancel: CancellationException) {
            throw cancel
        } catch (_: Exception) {
            break
        }
        if (byte.toInt() == 10) emit() else line.add(byte)
    }
    if (line.isNotEmpty()) emit()
}

/** Performs the blocking chat RPC used only after a stream failed before delivery. */
@OptIn(ExperimentalUuidApi::class)
internal suspend fun sendGatewayChat(
    http: HttpClient,
    gatewayUrl: String,
    clientToken: String,
    sessionKey: String?,
    message: String,
): GatewayReply {
    if (clientToken.isEmpty()) return missingTokenReply()
    val response = http.post("${gatewayUrl.trimEnd('/')}/api/v1/miniapp/rpc") {
        header(DenebGatewayClient.CLIENT_TOKEN_HEADER, clientToken)
        contentType(ContentType.Application.Json)
        setBody(
            RpcRequest(
                id = Uuid.random().toString(),
                method = "miniapp.chat.send",
                params = SendParams(message = message, sessionKey = sessionKey),
            ),
        )
    }
    if (!response.status.isSuccess()) {
        throw IllegalStateException("chat HTTP ${response.status.value}")
    }
    val envelope = response.body<RpcResponse>()
    val payload = envelope.payload
    return if (envelope.ok && payload != null) {
        GatewayReply(text = payload.text, model = payload.model, fellBack = payload.fellBack)
    } else {
        GatewayReply("⚠️ 게이트웨이 오류", ok = false)
    }
}

/**
 * Streams one gateway chat turn and dispatches its typed progress frames.
 *
 * Unknown events and keepalive comments are ignored for compatibility with
 * older gateways. The request has no overall deadline because a tool-heavy
 * turn can run for minutes; the socket deadline is kept finite and refreshed
 * by the server's keepalive frames.
 */
internal suspend fun streamGatewayChat(
    http: HttpClient,
    jsonCodec: Json,
    gatewayUrl: String,
    clientToken: String,
    sessionKey: String?,
    message: String,
    onTool: (ToolEvent) -> Unit = {},
    onProgress: (ProgressEvent) -> Unit = {},
    onThinking: (String) -> Unit = {},
    onReasoning: (String) -> Unit = {},
    onDelta: (String) -> Unit,
): GatewayReply {
    if (clientToken.isEmpty()) return missingTokenReply()
    var terminal: DoneEvent? = null
    // A failure before the gateway answered at all means the turn never started, so
    // there is nothing in the transcript to recover; a failure after it answered may
    // be a half-open socket over a run that IS still going. The two are separated
    // here rather than lumped together as "the stream threw".
    var gotResponse = false
    try {
        http.preparePost("${gatewayUrl.trimEnd('/')}/api/v1/miniapp/chat/stream") {
            header(DenebGatewayClient.CLIENT_TOKEN_HEADER, clientToken)
            header("Accept", "text/event-stream")
            contentType(ContentType.Application.Json)
            setBody(SendParams(message = message, sessionKey = sessionKey))
            timeout {
                requestTimeoutMillis = Long.MAX_VALUE
                socketTimeoutMillis = DenebGatewayClient.STREAM_SOCKET_TIMEOUT_MS
            }
        }.execute { response ->
            // The gateway answered, whatever it said — so a later failure is a
            // dropped stream, not a connect failure.
            gotResponse = true
            if (!response.status.isSuccess()) {
                val code = response.status.value
                throw if (isCredentialRejection(code)) {
                    GatewayStreamRefusedException(code)
                } else {
                    IllegalStateException("stream HTTP $code")
                }
            }
            val channel = response.bodyAsChannel()
            var event = ""
            val data = StringBuilder()
            fun flushEvent() {
                if (event.isEmpty() && data.isEmpty()) return
                val pendingEvent = event
                val pendingData = data.toString()
                event = ""
                data.clear()
                dispatchGatewayEvent(
                    event = pendingEvent,
                    data = pendingData,
                    jsonCodec = jsonCodec,
                    onDelta = onDelta,
                    onTool = onTool,
                    onProgress = onProgress,
                    onThinking = onThinking,
                    onReasoning = onReasoning,
                    onDone = { terminal = it },
                )
            }
            readSseLines(channel) { line ->
                when {
                    line.startsWith(":") -> Unit

                    line.startsWith("event:") -> event = line.removePrefix("event:").trim()

                    line.startsWith("data:") -> {
                        if (data.isNotEmpty()) data.append('\n')
                        data.append(line.removePrefix("data:").trimStart())
                    }

                    line.isEmpty() -> flushEvent()
                }
            }
            // Some proxies close immediately after the final data line instead of
            // forwarding SSE's terminating blank line. Preserve that last complete
            // frame rather than returning an empty or stale reply.
            flushEvent()
        }
    } catch (typed: GatewayStreamErrorException) {
        throw typed
    } catch (typed: GatewayStreamRefusedException) {
        throw typed
    } catch (cancel: CancellationException) {
        throw cancel
    } catch (failure: Exception) {
        // Connect refused / DNS / TLS / timeout before the response: nothing is
        // running on the gateway, so this must not be treated as a dropped socket
        // over a live turn.
        if (!gotResponse) throw GatewayStreamRefusedException(null, failure)
        throw failure
    }
    // EOF is not success until the gateway confirms the detached turn with a
    // terminal frame. Fail so askGateway() reconciles the canonical transcript
    // instead of committing a blank or partial answer as complete.
    val done = terminal ?: throw IllegalStateException("chat stream ended before terminal event")
    return GatewayReply(
        text = done.text,
        model = done.model,
        fellBack = done.fellBack,
        reasoning = done.reasoning.ifBlank { null },
    )
}

/**
 * The gateway reported the turn failed. Distinct from a transport failure: the
 * server spoke, so there is no detached run to recover — its message is the
 * answer, and polling the transcript for one would only waste the 90s budget and
 * then report a different (wrong) reason.
 */
internal class GatewayStreamErrorException(val gatewayMessage: String) : IllegalStateException(gatewayMessage)

/**
 * The stream failed in a way that makes transcript recovery pointless too: the
 * connection never landed, or the gateway rejected the credentials.
 *
 * Deliberately narrow. Other non-2xx keep the generic failure path, because that
 * path is also the compatibility fallback — a gateway without the streaming
 * endpoint answers 404, and re-sending through the blocking `miniapp.chat.send`
 * RPC is exactly the right response. What is NOT right is running that recovery
 * when the transcript poll it depends on is guaranteed to fail the same way
 * (offline, or a rejected token): it then spends its whole budget before
 * reporting a misleading reason.
 *
 * [status] is 401/403 when the gateway rejected us, null when the connection
 * itself failed.
 */
internal class GatewayStreamRefusedException(val status: Int?, cause: Throwable? = null) : IllegalStateException("stream refused${status?.let { " HTTP $it" }.orEmpty()}", cause)

/** Statuses where the transcript RPC would be rejected the same way the stream was. */
private fun isCredentialRejection(status: Int): Boolean = status == 401 || status == 403

/**
 * The context every miniapp RPC body runs in.
 *
 * Two jobs, both of which have to happen at this choke point rather than at call
 * sites. First, dispatch: without it the ktor pipeline and all request/response
 * JSON run on the caller's thread, and ~45 screens call these helpers from
 * `rememberCoroutineScope()` / `LaunchedEffect`, i.e. the main thread. Second,
 * teardown containment: cancelling one of those screen scopes (every navigation
 * away does) while a request is in flight raises the crash described in
 * [httpTeardownTolerantHandler], which can ONLY be intercepted by a
 * CoroutineExceptionHandler in the context of the coroutine being cancelled.
 * Individual `rememberCoroutineScope()` call sites cannot install one at all when
 * the caller is a `LaunchedEffect` (the Recomposer's effect context is not ours),
 * so containment has to live inside the call itself — here.
 */
internal val gatewayRpcContext: CoroutineContext by lazy {
    getBackgroundDispatcher() + httpTeardownTolerantHandler("transport")
}

/**
 * Calls a non-critical miniapp read RPC and drops stale responses when the
 * gateway credentials change while the request is in flight.
 */
@OptIn(ExperimentalUuidApi::class)
internal suspend inline fun <reified T> DenebGatewayClient.callRpc(method: String, params: JsonObject): T? {
    val url = gatewayUrl
    val token = clientToken
    val epoch = credEpoch
    if (token.isEmpty()) return null
    return withContext(gatewayRpcContext) {
        val response = try {
            http.post("$url/api/v1/miniapp/rpc") {
                header(DenebGatewayClient.CLIENT_TOKEN_HEADER, token)
                contentType(ContentType.Application.Json)
                setBody(RpcReq(id = Uuid.random().toString(), method = method, params = params))
            }
        } catch (cancel: CancellationException) {
            throw cancel
        } catch (_: Exception) {
            return@withContext null
        }
        if (!response.status.isSuccess()) return@withContext null
        val envelope = try {
            response.body<RpcEnv<T>>()
        } catch (cancel: CancellationException) {
            throw cancel
        } catch (_: Exception) {
            return@withContext null
        }
        val payload = envelope.takeIf { it.ok }?.payload
        if (epoch == credEpoch && url == gatewayUrl && token == clientToken) payload else null
    }
}

/**
 * Discriminated outcome of a miniapp RPC, for callers that must tell a
 * gateway-side rejection (e.g. NOT_FOUND — act on it) apart from a transport
 * failure (offline — do nothing). [callRpc] collapses both to null, which is
 * right for read caches but wrong for the wiki mirror's delete decision.
 */
internal sealed interface RpcOutcome<out T> {
    data class Ok<T>(val payload: T) : RpcOutcome<T>

    data class Rejected(val code: String) : RpcOutcome<Nothing>

    data object Unreachable : RpcOutcome<Nothing>
}

@OptIn(ExperimentalUuidApi::class)
internal suspend inline fun <reified T> DenebGatewayClient.callRpcOutcome(method: String, params: JsonObject): RpcOutcome<T> {
    val url = gatewayUrl
    val token = clientToken
    val epoch = credEpoch
    if (token.isEmpty()) return RpcOutcome.Unreachable
    return withContext(gatewayRpcContext) {
        val response = try {
            http.post("$url/api/v1/miniapp/rpc") {
                header(DenebGatewayClient.CLIENT_TOKEN_HEADER, token)
                contentType(ContentType.Application.Json)
                setBody(RpcReq(id = Uuid.random().toString(), method = method, params = params))
            }
        } catch (cancel: CancellationException) {
            throw cancel
        } catch (_: Exception) {
            return@withContext RpcOutcome.Unreachable
        }
        if (!response.status.isSuccess()) return@withContext RpcOutcome.Unreachable
        val envelope = try {
            response.body<RpcEnvFull<T>>()
        } catch (cancel: CancellationException) {
            throw cancel
        } catch (_: Exception) {
            return@withContext RpcOutcome.Unreachable
        }
        // Stale-credential fence, same as callRpc: never act on account A's answer
        // under account B.
        if (epoch != credEpoch || url != gatewayUrl || token != clientToken) return@withContext RpcOutcome.Unreachable
        if (envelope.ok) {
            return@withContext envelope.payload?.let { RpcOutcome.Ok(it) } ?: RpcOutcome.Unreachable
        }
        val code = envelope.error?.code.orEmpty()
        if (code.isBlank()) RpcOutcome.Unreachable else RpcOutcome.Rejected(code)
    }
}

/** Calls a miniapp write RPC and preserves the gateway's user-facing error. */
@OptIn(ExperimentalUuidApi::class)
internal suspend fun DenebGatewayClient.rpcWrite(method: String, params: JsonObject): String? {
    if (clientToken.isEmpty()) return "게이트웨이에 연결되어 있지 않습니다."
    return withContext(gatewayRpcContext) {
        try {
            val response = http.post("$gatewayUrl/api/v1/miniapp/rpc") {
                header(DenebGatewayClient.CLIENT_TOKEN_HEADER, clientToken)
                contentType(ContentType.Application.Json)
                setBody(RpcReq(id = Uuid.random().toString(), method = method, params = params))
            }
            if (!response.status.isSuccess()) return@withContext "요청을 처리하지 못했습니다."
            val result = response.body<RpcResult>()
            if (result.ok) null else result.error?.message?.ifBlank { null } ?: "요청을 처리하지 못했습니다."
        } catch (cancel: CancellationException) {
            throw cancel
        } catch (_: Exception) {
            "요청을 처리하지 못했습니다."
        }
    }
}

private fun dispatchGatewayEvent(
    event: String,
    data: String,
    jsonCodec: Json,
    onDelta: (String) -> Unit,
    onTool: (ToolEvent) -> Unit,
    onProgress: (ProgressEvent) -> Unit,
    onThinking: (String) -> Unit,
    onReasoning: (String) -> Unit,
    onDone: (DoneEvent) -> Unit,
) {
    when (event) {
        "delta" -> decodeOrNull<DeltaEvent>(jsonCodec, data)?.delta
            ?.takeIf(String::isNotEmpty)
            ?.let(onDelta)

        "tool" -> decodeOrNull<ToolEvent>(jsonCodec, data)
            ?.takeIf { it.tool.isNotEmpty() }
            ?.let(onTool)

        "progress" -> decodeOrNull<ProgressEvent>(jsonCodec, data)
            ?.takeIf { it.label.isNotEmpty() }
            ?.let(onProgress)

        "thinking" -> onThinking(decodeOrNull<ThinkingEvent>(jsonCodec, data)?.preview.orEmpty())

        "reasoning" -> decodeOrNull<ReasoningEvent>(jsonCodec, data)?.reasoning
            ?.takeIf(String::isNotEmpty)
            ?.let(onReasoning)

        "done" -> decodeOrNull<DoneEvent>(jsonCodec, data)?.let(onDone)

        "error" -> {
            val message = decodeOrNull<ErrorEvent>(jsonCodec, data)?.error ?: "gateway stream error"
            throw GatewayStreamErrorException(message)
        }
    }
}

private inline fun <reified T> decodeOrNull(jsonCodec: Json, data: String): T? = runCatching { jsonCodec.decodeFromString<T>(data) }.getOrNull()

private fun missingTokenReply() = GatewayReply(
    "⚠️ Deneb 클라이언트 토큰이 설정되지 않았습니다. 게이트웨이에서 deneb-client-token을 생성해 설정하세요.",
    ok = false,
)

@Serializable
internal data class RpcRequest(val id: String, val method: String, val params: SendParams)

@Serializable
internal data class SendParams(val message: String, val sessionKey: String? = null)

@Serializable
internal data class RpcResponse(val ok: Boolean = false, val payload: SendPayload? = null)

@Serializable
internal data class SendPayload(
    val text: String = "",
    val model: String = "",
    val sessionKey: String = "",
    val fellBack: Boolean = false,
)

/** Result of one gateway chat turn. */
internal data class GatewayReply(
    val text: String,
    val model: String = "",
    val fellBack: Boolean = false,
    val ok: Boolean = true,
    // Accumulated chain-of-thought for the turn, shown as the expandable reasoning
    // block. Null/blank when the model produced none.
    val reasoning: String? = null,
)

@Serializable
private data class DeltaEvent(val delta: String = "")

@Serializable
private data class ThinkingEvent(val preview: String = "")

@Serializable
private data class ReasoningEvent(val reasoning: String = "")

@Serializable
private data class DoneEvent(
    val text: String = "",
    val model: String = "",
    val fellBack: Boolean = false,
    val reasoning: String = "",
)

@Serializable
private data class ErrorEvent(val error: String = "")

@Serializable
internal data class PushEvent(
    val title: String = "",
    val body: String = "",
    val kind: String = "",
    val ref: String = "",
    val data: Map<String, String> = emptyMap(),
)

@Serializable
internal data class RpcReq(val id: String, val method: String, val params: JsonObject)

@Serializable
internal data class RpcEnv<T>(val ok: Boolean = false, val payload: T? = null)

// RpcEnv variant that also surfaces the gateway error, for callRpcOutcome.
@Serializable
internal data class RpcEnvFull<T>(
    val ok: Boolean = false,
    val payload: T? = null,
    val error: RpcError? = null,
)

@Serializable
internal data class RpcResult(val ok: Boolean = false, val error: RpcError? = null)

@Serializable
internal data class RpcError(val code: String = "", val message: String = "")
