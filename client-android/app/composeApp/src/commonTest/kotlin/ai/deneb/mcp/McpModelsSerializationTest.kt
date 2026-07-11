package ai.deneb.mcp

import kotlinx.serialization.SerializationException
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class McpModelsSerializationTest {

    private val json = Json {
        encodeDefaults = true
        explicitNulls = false
        ignoreUnknownKeys = true
    }

    @Test
    fun requestUsesJsonRpcVersionAndOptionalParams() {
        val withoutParams = JsonRpcRequest(id = 1, method = "tools/list")
        val withParams = JsonRpcRequest(
            id = 2,
            method = "tools/call",
            params = buildJsonObject { put("name", JsonPrimitive("search")) },
        )

        val encodedWithout = json.encodeToString(withoutParams)
        val encodedWith = json.encodeToString(withParams)

        assertTrue("\"jsonrpc\":\"2.0\"" in encodedWithout)
        assertTrue("\"id\":1" in encodedWithout)
        assertTrue("\"method\":\"tools/list\"" in encodedWithout)
        assertFalse("params" in encodedWithout)
        assertTrue("\"params\"" in encodedWith)
        assertEquals(withParams, json.decodeFromString<JsonRpcRequest>(encodedWith))
    }

    @Test
    fun responseSupportsSuccessNotificationAndErrorShapes() {
        val success = json.decodeFromString<JsonRpcResponse>(
            """{"jsonrpc":"2.0","id":7,"result":{"ok":true}}""",
        )
        val notification = json.decodeFromString<JsonRpcResponse>("""{"jsonrpc":"2.0"}""")
        val failure = json.decodeFromString<JsonRpcResponse>(
            """{"jsonrpc":"2.0","id":8,"error":{"code":-32601,"message":"missing"}}""",
        )

        assertEquals(7, success.id)
        assertEquals("true", success.result?.let { it.toString().substringAfter(":").trimEnd('}') })
        assertNull(success.error)
        assertNull(notification.id)
        assertNull(notification.result)
        assertEquals(-32601, failure.error?.code)
        assertEquals("missing", failure.error?.message)
        assertNull(failure.result)
    }

    @Test
    fun responseDefaultsProtocolVersionWhenOlderFixtureOmitsIt() {
        val decoded = json.decodeFromString<JsonRpcResponse>("""{"id":3,"result":null}""")

        assertEquals("2.0", decoded.jsonrpc)
        assertEquals(3, decoded.id)
        assertNull(decoded.result)
        assertNull(decoded.error)
    }

    @Test
    fun rpcErrorPreservesStructuredDiagnosticData() {
        val error = JsonRpcError(
            code = -32000,
            message = "tool failed",
            data = buildJsonObject {
                put("retryable", JsonPrimitive(false))
                put("attempt", JsonPrimitive(3))
            },
        )

        val decoded = json.decodeFromString<JsonRpcError>(json.encodeToString(error))

        assertEquals(error, decoded)
        assertTrue(decoded.data.toString().contains("retryable"))
        assertTrue(decoded.data.toString().contains("3"))
    }

    @Test
    fun toolDefinitionRoundTripsDescriptionAndInputSchema() {
        val definition = McpToolDefinition(
            name = "search",
            description = "Search documents",
            inputSchema = buildJsonObject {
                put("type", JsonPrimitive("object"))
                put(
                    "properties",
                    buildJsonObject {
                        put("query", buildJsonObject { put("type", JsonPrimitive("string")) })
                    },
                )
            },
        )

        val decoded = json.decodeFromString<McpToolDefinition>(json.encodeToString(definition))

        assertEquals(definition, decoded)
        assertEquals("object", decoded.inputSchema?.get("type")?.toString()?.trim('"'))
    }

    @Test
    fun toolDefinitionAcceptsMissingOptionalMetadata() {
        val decoded = json.decodeFromString<McpToolDefinition>("""{"name":"ping"}""")

        assertEquals("ping", decoded.name)
        assertNull(decoded.description)
        assertNull(decoded.inputSchema)
    }

    @Test
    fun toolsResultDefaultsMissingListAndPreservesOrder() {
        assertEquals(emptyList(), json.decodeFromString<McpToolsResult>("{}").tools)

        val result = McpToolsResult(
            tools = listOf(
                McpToolDefinition("first"),
                McpToolDefinition("second", "Second tool"),
            ),
        )
        val decoded = json.decodeFromString<McpToolsResult>(json.encodeToString(result))

        assertEquals(listOf("first", "second"), decoded.tools.map { it.name })
        assertEquals("Second tool", decoded.tools.last().description)
    }

    @Test
    fun callToolErrorUsesWireNameIsError() {
        val result = McpCallToolResult(
            content = listOf(McpContent(type = "text", text = "failed")),
            isError = true,
        )

        val encoded = json.encodeToString(result)
        val decoded = json.decodeFromString<McpCallToolResult>(encoded)

        assertTrue("\"isError\":true" in encoded)
        assertFalse("is_error" in encoded)
        assertTrue(decoded.isError)
        assertEquals("failed", decoded.content.single().text)
    }

    @Test
    fun callToolResultDefaultsToSuccessfulEmptyContent() {
        val decoded = json.decodeFromString<McpCallToolResult>("{}")

        assertEquals(emptyList(), decoded.content)
        assertFalse(decoded.isError)
    }

    @Test
    fun explicitFalseErrorFlagRoundTripsWithoutChangingMeaning() {
        val original = McpCallToolResult(
            content = listOf(McpContent(type = "text", text = "ok")),
            isError = false,
        )

        val encoded = json.encodeToString(original)
        val decoded = json.decodeFromString<McpCallToolResult>(encoded)

        assertTrue("\"isError\":false" in encoded)
        assertEquals(original, decoded)
    }

    @Test
    fun contentSupportsTextAndNonTextPartsWithoutInventingText() {
        val result = McpCallToolResult(
            content = listOf(
                McpContent(type = "text", text = "hello"),
                McpContent(type = "image"),
                McpContent(type = "resource", text = "uri://item"),
            ),
        )

        val decoded = json.decodeFromString<McpCallToolResult>(json.encodeToString(result))

        assertEquals(listOf("text", "image", "resource"), decoded.content.map { it.type })
        assertEquals(listOf("hello", null, "uri://item"), decoded.content.map { it.text })
    }

    @Test
    fun unknownResponseAndToolFieldsAreIgnoredForForwardCompatibility() {
        val response = json.decodeFromString<JsonRpcResponse>(
            """{"jsonrpc":"2.0","id":1,"result":{},"future":"value"}""",
        )
        val tool = json.decodeFromString<McpToolDefinition>(
            """{"name":"future","description":"x","annotations":{"safe":true}}""",
        )

        assertEquals(1, response.id)
        assertEquals("future", tool.name)
    }

    @Test
    fun missingRequiredRpcFieldsFailDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromString<JsonRpcRequest>("""{"jsonrpc":"2.0","method":"tools/list"}""")
        }
        assertFailsWith<SerializationException> {
            json.decodeFromString<JsonRpcError>("""{"code":1}""")
        }
        assertFailsWith<SerializationException> {
            json.decodeFromString<McpContent>("""{"text":"missing type"}""")
        }
    }

    @Test
    fun metadataCopyKeepsServerToolIdentityIndependent() {
        val original = McpToolMetadata(
            serverId = "server-a",
            name = "search",
            description = "Search",
            inputSchema = null,
        )
        val copied = original.copy(serverId = "server-b")

        assertEquals("server-a", original.serverId)
        assertEquals("server-b", copied.serverId)
        assertEquals(original.name, copied.name)
        assertEquals(original.description, copied.description)
        assertEquals(original.inputSchema, copied.inputSchema)
    }
}
