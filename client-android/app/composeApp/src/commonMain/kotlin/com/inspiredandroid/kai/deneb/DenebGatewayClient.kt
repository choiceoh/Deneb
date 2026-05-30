package com.inspiredandroid.kai.deneb

import com.inspiredandroid.kai.data.AppSettings
import com.inspiredandroid.kai.data.DataRepository
import com.inspiredandroid.kai.data.RemoteDataRepository
import com.inspiredandroid.kai.data.UiSubmission
import com.inspiredandroid.kai.httpClient
import com.inspiredandroid.kai.ui.chat.History
import io.github.vinceglb.filekit.PlatformFile
import io.ktor.client.call.body
import io.ktor.client.plugins.HttpTimeout
import io.ktor.client.plugins.contentnegotiation.ContentNegotiation
import io.ktor.client.request.header
import io.ktor.client.request.post
import io.ktor.client.request.setBody
import io.ktor.http.ContentType
import io.ktor.http.contentType
import io.ktor.serialization.kotlinx.json.json
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlin.uuid.ExperimentalUuidApi
import kotlin.uuid.Uuid

/**
 * A [DataRepository] backed by the Deneb gateway.
 *
 * It delegates every non-chat member to [base] (Kai's RemoteDataRepository, kept
 * so settings and the rest keep working) and overrides only the chat path to
 * drive a turn through the gateway's `miniapp.chat.send` bridge. The reply text
 * may carry a ```kai-ui fence, which Kai's chat renderer turns into an
 * interactive screen.
 *
 * Auth uses the X-Deneb-Client-Token header. Generate the token on the gateway
 * host with `go run ./gateway-go/cmd/deneb-client-token` and set it, together
 * with the gateway URL, under the [KEY_URL] / [KEY_TOKEN] settings keys.
 */
@OptIn(ExperimentalUuidApi::class)
class DenebGatewayClient(
    private val base: RemoteDataRepository,
    private val appSettings: AppSettings,
) : DataRepository by base {

    private val jsonCodec = Json {
        ignoreUnknownKeys = true
        isLenient = true
    }

    private val http = httpClient {
        install(ContentNegotiation) { json(jsonCodec) }
        install(HttpTimeout) { requestTimeoutMillis = REQUEST_TIMEOUT_MS }
    }

    private val _chatHistory = MutableStateFlow<List<History>>(emptyList())
    override val chatHistory: StateFlow<List<History>> = _chatHistory

    private var sessionKey: String = "client:main"

    private val gatewayUrl: String
        get() = appSettings.settings.getString(KEY_URL, DEFAULT_URL).trimEnd('/')

    private val clientToken: String
        get() = appSettings.settings.getString(KEY_TOKEN, "")

    override suspend fun ask(question: String?, files: List<PlatformFile>, uiSubmission: UiSubmission?) {
        val displayText = question?.trim().orEmpty()
        // A kai-ui button press arrives as a UiSubmission. Show the friendly
        // question in the chat, but send the agent a structured callback naming
        // the event (per the kai-ui prompt contract) plus the collected inputs.
        val sendText = if (uiSubmission != null) formatCallback(uiSubmission) else displayText
        if (sendText.isEmpty()) return
        if (displayText.isNotEmpty()) {
            _chatHistory.update { it + History(role = History.Role.USER, content = displayText) }
        }
        val reply = runCatching { send(sendText) }
            .getOrElse { "⚠️ ${it.message ?: "gateway request failed"}" }
        _chatHistory.update { it + History(role = History.Role.ASSISTANT, content = reply) }
    }

    private fun formatCallback(submission: UiSubmission): String = buildString {
        append("[kai-ui] event=").append(submission.pressedEvent)
        if (submission.values.isNotEmpty()) {
            append(" values={")
            append(submission.values.entries.joinToString(", ") { "${it.key}=${it.value}" })
            append("}")
        }
    }

    override fun clearHistory() {
        _chatHistory.value = emptyList()
    }

    override fun startNewChat() {
        _chatHistory.value = emptyList()
        sessionKey = "client:${Uuid.random()}"
    }

    private suspend fun send(message: String): String {
        if (clientToken.isEmpty()) {
            return "⚠️ Deneb 클라이언트 토큰이 설정되지 않았습니다. 게이트웨이에서 deneb-client-token을 생성해 설정하세요."
        }
        val resp: RpcResponse = http.post("$gatewayUrl/api/v1/miniapp/rpc") {
            header(CLIENT_TOKEN_HEADER, clientToken)
            contentType(ContentType.Application.Json)
            setBody(
                RpcRequest(
                    id = Uuid.random().toString(),
                    method = "miniapp.chat.send",
                    params = SendParams(message = message, sessionKey = sessionKey),
                ),
            )
        }.body()
        return if (resp.ok && resp.payload != null) resp.payload.text else "⚠️ 게이트웨이 오류"
    }

    @Serializable
    private data class RpcRequest(val id: String, val method: String, val params: SendParams)

    @Serializable
    private data class SendParams(val message: String, val sessionKey: String? = null)

    @Serializable
    private data class RpcResponse(val ok: Boolean = false, val payload: SendPayload? = null)

    @Serializable
    private data class SendPayload(val text: String = "", val model: String = "", val sessionKey: String = "")

    private companion object {
        const val CLIENT_TOKEN_HEADER = "X-Deneb-Client-Token"
        const val KEY_URL = "deneb.gatewayUrl"
        const val KEY_TOKEN = "deneb.clientToken"

        // Android emulator → host loopback. On a real device set the gateway's
        // LAN/Tailscale URL under KEY_URL.
        const val DEFAULT_URL = "http://10.0.2.2:18789"
        const val REQUEST_TIMEOUT_MS = 180_000L
    }
}
