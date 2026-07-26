package ai.deneb.deneb

import ai.deneb.executePhoneAction
import ai.deneb.sensing.readCurrentLocation
import ai.deneb.sensing.readRecentCallDigest
import ai.deneb.sensing.readWorkUsageDigest
import io.ktor.client.call.body
import io.ktor.client.plugins.timeout
import io.ktor.client.request.header
import io.ktor.client.request.post
import io.ktor.client.request.prepareGet
import io.ktor.client.request.setBody
import io.ktor.client.statement.bodyAsChannel
import io.ktor.http.ContentType
import io.ktor.http.contentType
import io.ktor.http.isSuccess
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlin.coroutines.coroutineContext
import kotlin.time.TimeSource
import kotlin.uuid.Uuid

/**
 * One-shot chat send for surfaces detached from the chat UI — the
 * notification voice-reply path (Android Auto / tray RemoteInput). Unlike
 * [DenebGatewayClient.send] it targets an explicit [targetSessionKey] (default
 * the main work session) instead of the currently open conversation, and never
 * touches in-memory conversation state: the exchange lands in the gateway
 * transcript and is picked up on the next app open / SSE sync. Returns
 * true when the gateway accepted and completed the turn.
 *
 * Split out of DenebGatewayClient.kt — the transport deps (http, clientToken,
 * gatewayUrl, the RPC envelopes) are all internal, matching the per-domain
 * extension-file pattern.
 */
suspend fun DenebGatewayClient.sendDetachedChat(
    message: String,
    targetSessionKey: String = "client:main",
): Boolean {
    val url = gatewayUrl
    val token = clientToken
    val epoch = credEpoch
    if (message.isBlank() || token.isEmpty() || url.isBlank()) return false
    return try {
        val response = http.post("$url/api/v1/miniapp/rpc") {
            header(DenebGatewayClient.CLIENT_TOKEN_HEADER, token)
            contentType(ContentType.Application.Json)
            setBody(
                RpcRequest(
                    id = Uuid.random().toString(),
                    method = "miniapp.chat.send",
                    params = SendParams(
                        message = message,
                        sessionKey = targetSessionKey,
                    ),
                ),
            )
        }
        if (!response.status.isSuccess()) return false
        val payload: RpcResponse = response.body()
        payload.ok && epoch == credEpoch && url == gatewayUrl && token == clientToken
    } catch (c: CancellationException) {
        throw c
    } catch (_: Exception) {
        false
    }
}

/**
 * Holds one long-lived SSE connection to the gateway's proactive-event
 * endpoint and invokes [onPush] for each {title, body} frame. Used by the
 * foreground daemon to raise a local notification the moment the gateway
 * produces a 업무-topic report (morning-letter, email-analysis), instead of
 * waiting for the next heartbeat poll.
 *
 * Reconnects with a small backoff after any drop (network change, server
 * restart, Android killing then restarting the daemon). Returns only when
 * the caller's coroutine is cancelled; missed frames while disconnected are
 * not replayed — the report is always also in the client:main transcript.
 *
 * Split out of DenebGatewayClient.kt.
 */
suspend fun DenebGatewayClient.subscribeEvents(onPush: (title: String, body: String) -> Unit) {
    var backoffMs = 2_000L
    while (coroutineContext.isActive) {
        if (clientToken.isEmpty() || gatewayUrl.isBlank()) {
            delay(10_000)
            continue
        }
        try {
            http.prepareGet("$gatewayUrl/api/v1/miniapp/events") {
                header(DenebGatewayClient.CLIENT_TOKEN_HEADER, clientToken)
                header(DenebGatewayClient.CLIENT_KIND_HEADER, "mobile")
                header("Accept", "text/event-stream")
                timeout {
                    // Long-lived: no overall cap. The 30s server keepalive
                    // keeps the socket under STREAM_SOCKET_TIMEOUT_MS.
                    requestTimeoutMillis = Long.MAX_VALUE
                    socketTimeoutMillis = DenebGatewayClient.STREAM_SOCKET_TIMEOUT_MS
                }
            }.execute { response ->
                if (!response.status.isSuccess()) {
                    throw IllegalStateException("events HTTP ${response.status.value}")
                }
                backoffMs = 2_000L // connected — reset backoff
                syncNativeStateAsync()
                reconcileOpenConversationAsync()
                val channel = response.bodyAsChannel()
                var event = ""
                val data = StringBuilder()
                readSseLines(channel) { line ->
                    when {
                        line.startsWith(":") -> Unit

                        // keepalive comment
                        line.startsWith("event:") -> event = line.removePrefix("event:").trim()

                        line.startsWith("data:") -> {
                            if (data.isNotEmpty()) data.append('\n')
                            data.append(line.removePrefix("data:").trimStart())
                        }

                        line.isEmpty() -> {
                            if (event == "push") {
                                runCatching {
                                    jsonCodec.decodeFromString(PushEvent.serializer(), data.toString())
                                }.getOrNull()?.let { p ->
                                    // kind=phone_action: run the gateway's Intent command in-app
                                    // (open_url/open_app/share/message/dial/photo) instead of
                                    // raising a notification. data["action"] + its args.
                                    if (p.kind == "phone_action") {
                                        val action = p.data["action"].orEmpty()
                                        if (action == "sync_state") {
                                            // Gateway asks for a fresh state fix (phone_read hit a
                                            // stale cache). Bypass the periodic throttle and push
                                            // location+battery and usage now; each read is a no-op
                                            // off Android / without its user-granted access.
                                            scope.launch {
                                                runCatching {
                                                    readCurrentLocation()?.let { fix ->
                                                        ingestEvent("location_update", "", fix)
                                                    }
                                                }
                                                runCatching {
                                                    readWorkUsageDigest()?.let { digest ->
                                                        // Re-arm the periodic throttle — this push is fresh, so
                                                        // the next scheduled forward would only repeat it. Benign
                                                        // race with the sync coroutine (worst case: one redundant
                                                        // forward, same as without the re-arm).
                                                        lastUsageForward = TimeSource.Monotonic.markNow()
                                                        ingestEvent("usage_update", "앱 사용 리듬", digest)
                                                    }
                                                }
                                                // Recent calls ride the same burst. No throttle of its
                                                // own: the digest is only built when the gateway asks,
                                                // and a no-op without READ_CALL_LOG.
                                                runCatching {
                                                    readRecentCallDigest()?.let { digest ->
                                                        ingestEvent("calllog_update", "최근 통화", digest)
                                                    }
                                                }
                                            }
                                        } else if (action.isNotBlank()) {
                                            val ok = runCatching { executePhoneAction(action, p.data) }
                                                .getOrDefault(false)
                                            // Report the outcome back so the gateway's phone_write
                                            // dispatch (waiting briefly on ref) can tell the agent
                                            // the intent actually launched — delivery ≠ execution.
                                            if (p.ref.isNotBlank()) {
                                                scope.launch {
                                                    runCatching {
                                                        ingestEvent(
                                                            "phone_action_result",
                                                            action,
                                                            buildJsonObject {
                                                                put("id", p.ref)
                                                                put("ok", ok)
                                                            }.toString(),
                                                        )
                                                    }
                                                }
                                            }
                                        }
                                    } else if (p.body.isNotBlank()) {
                                        syncNativeStateAsync()
                                        onPush(p.title.ifBlank { "Deneb" }, p.body)
                                    }
                                }
                            }
                            event = ""
                            data.clear()
                        }
                    }
                }
            }
        } catch (cancel: CancellationException) {
            throw cancel
        } catch (_: Throwable) {
            // Drop/refused/timeout — back off and retry. Capped (raised
            // 60s→120s, M3) so a long outage on a backgrounded phone retries
            // less often — fewer radio wakeups — while recovery stays quick.
            delay(backoffMs)
            backoffMs = (backoffMs * 2).coerceAtMost(120_000L)
        }
    }
}
