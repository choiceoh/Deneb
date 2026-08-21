package ai.deneb.mcp

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject

@Serializable
data class JsonRpcRequest(
    val jsonrpc: String = "2.0",
    val id: Int,
    val method: String,
    val params: JsonElement? = null,
)

@Serializable
data class JsonRpcResponse(
    val jsonrpc: String = "2.0",
    val id: Int? = null,
    val result: JsonElement? = null,
    val error: JsonRpcError? = null,
)

@Serializable
data class JsonRpcError(
    val code: Int,
    val message: String,
    val data: JsonElement? = null,
)

@Serializable
data class McpToolDefinition(
    val name: String,
    val description: String? = null,
    val inputSchema: JsonObject? = null,
)

@Serializable
data class McpToolsResult(
    val tools: List<McpToolDefinition> = emptyList(),
)

@Serializable
data class McpCallToolResult(
    val content: List<McpContent> = emptyList(),
    @SerialName("isError")
    val isError: Boolean = false,
    /**
     * MCP 2026-07-28. Absent on older servers, which the spec says to read as
     * "complete" — null does exactly that. [RESULT_TYPE_INPUT_REQUIRED] means
     * the server returned an MRTR interim result instead of a final one.
     */
    val resultType: String? = null,
    /** The requests an [RESULT_TYPE_INPUT_REQUIRED] result wants answered. */
    val inputRequests: Map<String, McpInputRequest>? = null,
)

/** One server-initiated request inside an MRTR interim result. */
@Serializable
data class McpInputRequest(
    val method: String? = null,
)

/**
 * The result of `server/discover` — MCP 2026-07-28's replacement for the
 * `initialize` handshake as a version and capability probe.
 */
@Serializable
data class McpDiscoverResult(
    val supportedVersions: List<String> = emptyList(),
    val capabilities: JsonObject? = null,
    val instructions: String? = null,
)

/** The result of a handshake-era `initialize`. */
@Serializable
data class McpInitializeResult(
    val protocolVersion: String? = null,
)

@Serializable
data class McpContent(
    val type: String,
    val text: String? = null,
)

data class McpToolMetadata(
    val serverId: String,
    val name: String,
    val description: String,
    val inputSchema: JsonObject?,
)
