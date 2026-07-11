package ai.deneb.mcp

import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

/** JSON-RPC and MCP server configuration contracts at the native transport boundary. */
class McpWireContractBoundaryTest {
    private val json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
        explicitNulls = true
    }

    @Test
    fun requestPreservesProtocolIdMethodAndNestedParameters() {
        val input = json.parseToJsonElement(
            """{
                "jsonrpc":"2.0",
                "id":2147483647,
                "method":"tools/call",
                "params":{
                    "name":"mail.search",
                    "arguments":{"query":"한글 /?#","limit":20,"unread":true}
                }
            }
            """.trimIndent(),
        ).jsonObject

        val request = json.decodeFromJsonElement(JsonRpcRequest.serializer(), input)
        val encoded = json.encodeToJsonElement(JsonRpcRequest.serializer(), request).jsonObject

        assertEquals("2.0", request.jsonrpc)
        assertEquals(Int.MAX_VALUE, request.id)
        assertEquals("tools/call", request.method)
        assertEquals("mail.search", request.params?.jsonObject?.get("name")?.jsonPrimitive?.content)
        assertEquals("한글 /?#", request.params?.jsonObject?.get("arguments")?.jsonObject?.get("query")?.jsonPrimitive?.content)
        assertEquals(input, encoded)
    }

    @Test
    fun requestDefaultsProtocolButKeepsExplicitNullParams() {
        val request = json.decodeFromString<JsonRpcRequest>(
            """{"id":7,"method":"tools/list","params":null}""",
        )

        assertEquals("2.0", request.jsonrpc)
        assertEquals(7, request.id)
        assertEquals("tools/list", request.method)
        assertNull(request.params)
        val encoded = json.encodeToJsonElement(JsonRpcRequest.serializer(), request).jsonObject
        assertEquals("2.0", encoded["jsonrpc"]?.jsonPrimitive?.content)
        assertTrue(encoded.containsKey("params"))
    }

    @Test
    fun requestRejectsMissingIdAndMethodSeparately() {
        assertFailsWith<SerializationException> {
            json.decodeFromString<JsonRpcRequest>("""{"method":"tools/list"}""")
        }
        assertFailsWith<SerializationException> {
            json.decodeFromString<JsonRpcRequest>("""{"id":1}""")
        }
    }

    @Test
    fun responsePreservesSuccessfulNestedResultAndUnknownFutureFields() {
        val response = json.decodeFromString<JsonRpcResponse>(
            """{
                "jsonrpc":"2.0",
                "id":42,
                "result":{"tools":[{"name":"mail.search","future":true}]},
                "error":null,
                "traceId":"future-field"
            }
            """.trimIndent(),
        )

        assertEquals("2.0", response.jsonrpc)
        assertEquals(42, response.id)
        assertEquals("mail.search", response.result?.jsonObject?.get("tools")?.jsonArray?.single()?.jsonObject?.get("name")?.jsonPrimitive?.content)
        assertNull(response.error)
    }

    @Test
    fun responsePreservesStructuredErrorWhenResultIsNull() {
        val response = json.decodeFromString<JsonRpcResponse>(
            """{
                "jsonrpc":"2.0",
                "id":9,
                "result":null,
                "error":{
                    "code":-32602,
                    "message":"invalid params",
                    "data":{"field":"query","reason":"required"}
                }
            }
            """.trimIndent(),
        )

        assertNull(response.result)
        assertEquals(-32602, response.error?.code)
        assertEquals("invalid params", response.error?.message)
        assertEquals("query", response.error?.data?.jsonObject?.get("field")?.jsonPrimitive?.content)
        assertEquals("required", response.error?.data?.jsonObject?.get("reason")?.jsonPrimitive?.content)
    }

    @Test
    fun responseAllowsNotificationStyleMissingId() {
        val response = json.decodeFromString<JsonRpcResponse>(
            """{"jsonrpc":"2.0","result":{"ok":true}}""",
        )

        assertNull(response.id)
        assertTrue(response.result?.jsonObject?.get("ok")?.jsonPrimitive?.content?.toBoolean() == true)
        assertNull(response.error)
    }

    @Test
    fun rpcErrorRequiresCodeAndMessageButAllowsAnyJsonData() {
        val error = json.decodeFromString<JsonRpcError>(
            """{"code":-32000,"message":"server failure","data":[1,"two",true]}""",
        )

        assertEquals(-32000, error.code)
        assertEquals("server failure", error.message)
        assertEquals(3, error.data?.jsonArray?.size)
        assertEquals("two", error.data?.jsonArray?.get(1)?.jsonPrimitive?.content)
        assertFailsWith<SerializationException> {
            json.decodeFromString<JsonRpcError>("""{"message":"missing code"}""")
        }
        assertFailsWith<SerializationException> {
            json.decodeFromString<JsonRpcError>("""{"code":-1}""")
        }
    }

    @Test
    fun toolDefinitionPreservesDescriptionAndDeepJsonSchema() {
        val definition = json.decodeFromString<McpToolDefinition>(
            """{
                "name":"calendar.create",
                "description":"Create an event",
                "inputSchema":{
                    "type":"object",
                    "properties":{
                        "title":{"type":"string"},
                        "allDay":{"type":"boolean"},
                        "attendees":{"type":"array","items":{"type":"string"}}
                    },
                    "required":["title"]
                }
            }
            """.trimIndent(),
        )

        assertEquals("calendar.create", definition.name)
        assertEquals("Create an event", definition.description)
        assertEquals("object", definition.inputSchema?.get("type")?.jsonPrimitive?.content)
        assertEquals(3, definition.inputSchema?.get("properties")?.jsonObject?.size)
        assertEquals("title", definition.inputSchema?.get("required")?.jsonArray?.single()?.jsonPrimitive?.content)
    }

    @Test
    fun toolDefinitionAllowsOmittedOptionalMetadataButRequiresName() {
        val minimal = json.decodeFromString<McpToolDefinition>("""{"name":"ping"}""")

        assertEquals("ping", minimal.name)
        assertNull(minimal.description)
        assertNull(minimal.inputSchema)
        assertFailsWith<SerializationException> {
            json.decodeFromString<McpToolDefinition>("{}")
        }
    }

    @Test
    fun toolsResultPreservesOrderAndIndependentSchemas() {
        val result = json.decodeFromString<McpToolsResult>(
            """{
                "tools":[
                    {"name":"first","inputSchema":{"type":"object"}},
                    {"name":"second","description":"Second tool","inputSchema":{"type":"string"}}
                ],
                "futureCursor":"ignored"
            }
            """.trimIndent(),
        )

        assertEquals(listOf("first", "second"), result.tools.map { it.name })
        assertNull(result.tools.first().description)
        assertEquals("Second tool", result.tools.last().description)
        assertEquals("object", result.tools.first().inputSchema?.get("type")?.jsonPrimitive?.content)
        assertEquals("string", result.tools.last().inputSchema?.get("type")?.jsonPrimitive?.content)
    }

    @Test
    fun toolsResultDefaultsToValidEmptyList() {
        val result = json.decodeFromString<McpToolsResult>("{}")

        assertTrue(result.tools.isEmpty())
        assertEquals(
            buildJsonObject { put("tools", JsonArray(emptyList())) },
            json.encodeToJsonElement(McpToolsResult.serializer(), result),
        )
    }

    @Test
    fun callToolResultUsesWireIsErrorNameAndPreservesContentKinds() {
        val result = json.decodeFromString<McpCallToolResult>(
            """{
                "content":[
                    {"type":"text","text":"완료"},
                    {"type":"image","text":null}
                ],
                "isError":true
            }
            """.trimIndent(),
        )

        assertTrue(result.isError)
        assertEquals(listOf("text", "image"), result.content.map { it.type })
        assertEquals("완료", result.content.first().text)
        assertNull(result.content.last().text)
        val encoded = json.encodeToJsonElement(McpCallToolResult.serializer(), result).jsonObject
        assertTrue(encoded.containsKey("isError"))
        assertFalse(encoded.containsKey("is_error"))
    }

    @Test
    fun callToolResultDefaultsToNonErrorEmptyContent() {
        val result = json.decodeFromString<McpCallToolResult>("{}")

        assertFalse(result.isError)
        assertTrue(result.content.isEmpty())
    }

    @Test
    fun mcpContentRequiresTypeAndAllowsNullOrMissingText() {
        val explicitNull = json.decodeFromString<McpContent>("""{"type":"image","text":null}""")
        val omitted = json.decodeFromString<McpContent>("""{"type":"resource"}""")

        assertEquals("image", explicitNull.type)
        assertNull(explicitNull.text)
        assertEquals("resource", omitted.type)
        assertNull(omitted.text)
        assertFailsWith<SerializationException> {
            json.decodeFromString<McpContent>("""{"text":"missing type"}""")
        }
    }

    @Test
    fun serverConfigPreservesHeadersAndDisabledState() {
        val config = json.decodeFromString<McpServerConfig>(
            """{
                "id":"server-1",
                "name":"업무 MCP",
                "url":"https://mcp.example/sse?tenant=A%20B",
                "headers":{"Authorization":"Bearer secret","X-Tenant":"한글"},
                "isEnabled":false,
                "futureHealth":"ok"
            }
            """.trimIndent(),
        )

        assertEquals("server-1", config.id)
        assertEquals("업무 MCP", config.name)
        assertEquals("https://mcp.example/sse?tenant=A%20B", config.url)
        assertEquals("Bearer secret", config.headers["Authorization"])
        assertEquals("한글", config.headers["X-Tenant"])
        assertFalse(config.isEnabled)
        assertEquals(config, json.decodeFromString(json.encodeToString(McpServerConfig.serializer(), config)))
    }

    @Test
    fun serverConfigDefaultsHeadersAndEnabledButRequiresIdentityAndUrl() {
        val config = json.decodeFromString<McpServerConfig>(
            """{"id":"server","name":"Server","url":"https://example.com"}""",
        )

        assertTrue(config.headers.isEmpty())
        assertTrue(config.isEnabled)
        assertFailsWith<SerializationException> {
            json.decodeFromString<McpServerConfig>("""{"name":"Server","url":"https://example.com"}""")
        }
        assertFailsWith<SerializationException> {
            json.decodeFromString<McpServerConfig>("""{"id":"server","url":"https://example.com"}""")
        }
        assertFailsWith<SerializationException> {
            json.decodeFromString<McpServerConfig>("""{"id":"server","name":"Server"}""")
        }
    }
}
