package ai.deneb.mcp

import ai.deneb.httpClient
import io.ktor.client.HttpClient
import io.ktor.client.plugins.HttpTimeout
import io.ktor.client.request.header
import io.ktor.client.request.post
import io.ktor.client.request.setBody
import io.ktor.client.statement.bodyAsText
import io.ktor.http.ContentType
import io.ktor.http.contentType
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.decodeFromJsonElement

/**
 * A Model Context Protocol client over streamable HTTP.
 *
 * Two protocol eras are spoken, decided per server by [connect]: MCP
 * 2026-07-28 ("MCP 2.0") is stateless — no handshake, no session, per-request
 * `_meta` and mirrored headers — while everything older uses the `initialize`
 * handshake. A `server/discover` probe tells them apart; the revision's
 * specifics live in McpProtocol2026.kt.
 */
class McpClient(
    private val url: String,
    private val headers: Map<String, String>,
) {
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        encodeDefaults = true
        explicitNulls = false
    }

    private val client: HttpClient = httpClient {
        install(HttpTimeout) {
            requestTimeoutMillis = 60_000
            connectTimeoutMillis = 10_000
        }
    }
    private var sessionId: String? = null
    private var requestId = 0

    /**
     * The revision settled by [connect]: null until then, then whatever the
     * server agreed to. [PROTOCOL_VERSION_2026] switches every request to the
     * stateless shape.
     */
    private var negotiatedProtocol: String? = null

    private fun nextId(): Int = ++requestId

    private suspend fun sendRequest(
        method: String,
        params: kotlinx.serialization.json.JsonElement? = null,
        protocolOverride: String? = null,
    ): JsonRpcResponse {
        // The override lets the discovery probe speak 2.0 before anything is
        // negotiated — that request is how the negotiation happens.
        val protocol = protocolOverride ?: negotiatedProtocol
        val stateless = protocol == PROTOCOL_VERSION_2026
        val effectiveParams = withStatelessMeta(params, protocol)
        val request = JsonRpcRequest(
            id = nextId(),
            method = method,
            params = effectiveParams,
        )
        val requestBody = json.encodeToString(JsonRpcRequest.serializer(), request)

        val response = client.post(url) {
            contentType(ContentType.Application.Json)
            header("Accept", "application/json, text/event-stream")
            // 2025-06-18 and newer expect the negotiated version on every
            // request; 2026-07-28 additionally mirrors the method and the
            // tool/resource name so gateways route without parsing the body.
            protocol?.let { header(HEADER_PROTOCOL_VERSION, it) }
            if (stateless) {
                header(HEADER_MCP_METHOD, method)
                mcpNameFor(effectiveParams)?.let { header(HEADER_MCP_NAME, encodeHeaderValue(it)) }
            } else {
                sessionId?.let { header("Mcp-Session-Id", it) }
            }
            this@McpClient.headers.keys.forEach { key ->
                header(key, this@McpClient.headers[key]!!)
            }
            setBody(requestBody)
        }

        // Track session ID from response. 2026-07-28 removed protocol-level
        // sessions, so a stateless server's header (if any) is ignored rather
        // than echoed back.
        if (!stateless) {
            response.headers["Mcp-Session-Id"]?.let { sessionId = it }
        }

        val responseText = response.bodyAsText()

        // Handle SSE response
        if (response.headers["Content-Type"]?.contains("text/event-stream") == true) {
            return parseSseResponse(responseText)
        }

        return json.decodeFromString(JsonRpcResponse.serializer(), responseText)
    }

    private fun parseSseResponse(sseText: String): JsonRpcResponse {
        // Parse SSE format: look for "data: " lines and find the JSON-RPC response
        val lines = sseText.lines()
        val dataLines = lines.filter { it.startsWith("data: ") }
        for (line in dataLines) {
            val data = line.removePrefix("data: ").trim()
            if (data.isEmpty()) continue
            try {
                return json.decodeFromString(JsonRpcResponse.serializer(), data)
            } catch (_: Exception) {
                // Not a valid JSON-RPC response, continue
            }
        }
        throw McpException("No valid JSON-RPC response found in SSE stream")
    }

    /**
     * Settles which protocol era this server speaks and makes the client ready
     * to call it.
     *
     * MCP 2026-07-28 deleted the `initialize` handshake, so the era has to be
     * detected rather than assumed: `server/discover` is the one method every
     * 2.0 server must implement, and an older server answers it with "method
     * not found". A failed probe is the expected answer from the older era —
     * never an error to report — so the handshake below is what surfaces a
     * genuinely unreachable or broken server.
     *
     * Probing first is safe here because each request is its own POST: a
     * strict server that rejects anything before `initialize` costs one 4xx,
     * and the handshake that follows opens a fresh request. (The gateway's
     * stdio client deliberately detects in the opposite order — there a
     * rejection can take the shared transport, and the child process with it.)
     */
    suspend fun connect() {
        if (discoverStatelessServer()) return
        handshake()
    }

    /** Returns true when the server speaks the stateless revision. */
    private suspend fun discoverStatelessServer(): Boolean {
        val result = runCatching {
            val response = sendRequest("server/discover", protocolOverride = PROTOCOL_VERSION_2026)
            if (response.error != null) return false
            val payload = response.result ?: return false
            json.decodeFromJsonElement<McpDiscoverResult>(payload)
        }.getOrNull() ?: return false

        // A server may implement discover for a revision we do not speak; only
        // a mutually supported one lets us go stateless.
        if (PROTOCOL_VERSION_2026 !in result.supportedVersions) return false
        negotiatedProtocol = PROTOCOL_VERSION_2026
        return true
    }

    /** The pre-2026-07-28 path: `initialize` pins the version for the connection. */
    private suspend fun handshake() {
        val params = buildJsonObject {
            put("protocolVersion", JsonPrimitive(HANDSHAKE_PROTOCOL_VERSION))
            put(
                "capabilities",
                buildJsonObject {},
            )
            put(
                "clientInfo",
                buildJsonObject {
                    put("name", JsonPrimitive("Deneb"))
                    put("version", JsonPrimitive("1.0"))
                },
            )
        }
        val response = sendRequest("initialize", params)
        if (response.error != null) {
            throw McpException("Initialize failed: ${response.error.message}")
        }
        // Adopt whatever the server negotiated down to: from 2025-06-18 on, it
        // has to ride the MCP-Protocol-Version header of every later request.
        negotiatedProtocol = response.result
            ?.let { runCatching { json.decodeFromJsonElement<McpInitializeResult>(it) }.getOrNull() }
            ?.protocolVersion
            ?: HANDSHAKE_PROTOCOL_VERSION

        // Send initialized notification (no id, no response expected)
        sendNotification("notifications/initialized")
    }

    private suspend fun sendNotification(method: String) {
        val body = buildJsonObject {
            put("jsonrpc", JsonPrimitive("2.0"))
            put("method", JsonPrimitive(method))
        }
        val requestBody = json.encodeToString(JsonObject.serializer(), body)

        client.post(url) {
            contentType(ContentType.Application.Json)
            negotiatedProtocol?.let { header(HEADER_PROTOCOL_VERSION, it) }
            sessionId?.let { header("Mcp-Session-Id", it) }
            this@McpClient.headers.keys.forEach { key ->
                header(key, this@McpClient.headers[key]!!)
            }
            setBody(requestBody)
        }
    }

    suspend fun listTools(): List<McpToolDefinition> {
        val response = sendRequest("tools/list")
        if (response.error != null) {
            throw McpException("tools/list failed: ${response.error.message}")
        }
        val result = response.result ?: return emptyList()
        val toolsResult = json.decodeFromJsonElement<McpToolsResult>(result)
        return toolsResult.tools
    }

    suspend fun callTool(name: String, arguments: JsonObject): String {
        val params = buildJsonObject {
            put("name", JsonPrimitive(name))
            put("arguments", arguments)
        }
        val response = sendRequest("tools/call", params)
        if (response.error != null) {
            throw McpException("tools/call failed: ${response.error.message}")
        }
        val result = response.result ?: return ""
        val callResult = json.decodeFromJsonElement<McpCallToolResult>(result)
        if (callResult.resultType == RESULT_TYPE_INPUT_REQUIRED) {
            // MRTR: the server wants sampling/elicitation/roots input before it
            // can finish. This client declares no such capability, and an
            // interim result has no content — so say that rather than return
            // "", which reads as "the tool ran and found nothing".
            throw McpException(
                "Tool $name needs client input this app does not provide: " +
                    describeInputRequests(callResult.inputRequests),
            )
        }
        if (callResult.isError) {
            val errorText = callResult.content.mapNotNull { it.text }.joinToString("\n")
            throw McpException("Tool error: $errorText")
        }
        return callResult.content.mapNotNull { it.text }.joinToString("\n")
    }

    fun close() {
        client.close()
    }
}

class McpException(message: String) : Exception(message)
