package ai.deneb.mcp

import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlin.io.encoding.Base64

/**
 * MCP 2026-07-28 ("MCP 2.0") — the stateless revision.
 *
 * The handshake era pinned a connection's protocol version once, in
 * `initialize`, and tracked it with an `Mcp-Session-Id`. 2026-07-28 removes
 * both: every request declares its own version, identity, and capabilities in
 * `params._meta`, mirrored into HTTP headers so gateways can route and
 * authorize without parsing the body.
 *
 * This file holds only what is specific to that revision. [McpClient] decides
 * per server which era to speak — see its `connect`.
 */

/** The stateless revision — "MCP 2.0" in SDK numbering. */
const val PROTOCOL_VERSION_2026 = "2026-07-28"

/**
 * The revision offered in an `initialize` handshake. Deliberately not the
 * newest we speak: 2026-07-28 has no `initialize`, so a server reached through
 * the handshake is in the older era by definition.
 */
const val HANDSHAKE_PROTOCOL_VERSION = "2025-11-25"

/**
 * Handshake-era revisions this client can actually speak. A server may
 * legally negotiate down to any of them, but the lifecycle rules say a client
 * must not continue on a version it does not implement — so an answer outside
 * this set is a failed connection, not a value to echo back in
 * `MCP-Protocol-Version` on every later request.
 */
val SUPPORTED_HANDSHAKE_VERSIONS = setOf(
    "2025-11-25",
    "2025-06-18",
    "2025-03-26",
    "2024-11-05",
)

/**
 * How long the 2.0 discovery probe may take before the client gives up and
 * falls back to the handshake.
 *
 * A conforming server answers instantly either way — a result, or "method not
 * found". A handshake-era server is allowed to simply stay silent on a method
 * it does not know, though, and without this cap that silence would spend the
 * client's whole 60s request budget before the real handshake even started.
 */
const val DISCOVER_PROBE_TIMEOUT_MS = 5_000L

// Reserved `_meta` keys. The `io.modelcontextprotocol/` prefix is reserved for
// the spec, so these names are stable.
const val META_PROTOCOL_VERSION = "io.modelcontextprotocol/protocolVersion"
const val META_CLIENT_INFO = "io.modelcontextprotocol/clientInfo"
const val META_CLIENT_CAPABILITIES = "io.modelcontextprotocol/clientCapabilities"

// Standard request headers the revision mirrors from the JSON-RPC body.
const val HEADER_PROTOCOL_VERSION = "MCP-Protocol-Version"
const val HEADER_MCP_METHOD = "Mcp-Method"
const val HEADER_MCP_NAME = "Mcp-Name"

/** Marks an MRTR interim result — the server needs input before it can finish. */
const val RESULT_TYPE_INPUT_REQUIRED = "input_required"

/** Marks an ordinary finished result. Absent on pre-2026-07-28 servers. */
const val RESULT_TYPE_COMPLETE = "complete"

// Base64 sentinel wrapping a header value that cannot travel as plain ASCII.
private const val SENTINEL_PREFIX = "=?base64?"
private const val SENTINEL_SUFFIX = "?="

private const val CLIENT_NAME = "Deneb"
private const val CLIENT_VERSION = "1.0"

/**
 * The per-request metadata 2026-07-28 requires in place of the handshake.
 *
 * Capabilities are declared per request and servers must not infer them from
 * an earlier one, so the empty object is a standing statement: this client
 * offers no sampling, elicitation, or roots.
 */
fun statelessMeta(): JsonObject = buildJsonObject {
    put(META_PROTOCOL_VERSION, JsonPrimitive(PROTOCOL_VERSION_2026))
    put(
        META_CLIENT_INFO,
        buildJsonObject {
            put("name", JsonPrimitive(CLIENT_NAME))
            put("version", JsonPrimitive(CLIENT_VERSION))
        },
    )
    put(META_CLIENT_CAPABILITIES, buildJsonObject {})
}

/**
 * Attaches that metadata when [protocol] is the stateless revision, leaving
 * handshake-era params untouched. A stateless request always gets a params
 * object, even when it carries no arguments of its own — `_meta` is required.
 */
fun withStatelessMeta(params: JsonElement?, protocol: String?): JsonElement? {
    if (protocol != PROTOCOL_VERSION_2026) return params
    val existing = params as? JsonObject
    return buildJsonObject {
        existing?.forEach { (key, value) -> put(key, value) }
        put("_meta", statelessMeta())
    }
}

/**
 * The value for the `Mcp-Name` header, which the revision requires on
 * `tools/call`, `resources/read`, and `prompts/get`. Its source is
 * `params.name` or `params.uri`; anything else has no name to mirror.
 */
fun mcpNameFor(params: JsonElement?): String? {
    val obj = params as? JsonObject ?: return null
    val source = obj["name"] ?: obj["uri"] ?: return null
    return runCatching { source.jsonPrimitive.content }.getOrNull()
}

/**
 * Encodes a header value per the revision's rules: plain when it is already
 * safe ASCII, Base64 sentinel otherwise. A value that merely *looks* like the
 * sentinel must also be encoded, or a server could not tell the two apart.
 */
@OptIn(kotlin.io.encoding.ExperimentalEncodingApi::class)
fun encodeHeaderValue(value: String): String {
    val plainIsSafe = value.isNotEmpty() &&
        value.all { it.code in 0x21..0x7E } &&
        !(value.startsWith(SENTINEL_PREFIX) && value.endsWith(SENTINEL_SUFFIX))
    if (plainIsSafe) return value
    return SENTINEL_PREFIX + Base64.encode(value.encodeToByteArray()) + SENTINEL_SUFFIX
}

/**
 * Names the capabilities an MRTR server asked for, so the failure says which
 * one is missing rather than a bare "unsupported". Sorted for a stable message.
 */
fun describeInputRequests(requests: Map<String, McpInputRequest>?): String {
    val methods = requests?.values?.mapNotNull { it.method }?.filter { it.isNotEmpty() }?.sorted()
    if (methods.isNullOrEmpty()) return "no input requests declared"
    return methods.joinToString(", ")
}
