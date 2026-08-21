package ai.deneb.mcp

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * MCP 2026-07-28 ("MCP 2.0") request-shaping and result contracts: the pieces
 * [McpClient] relies on to speak the stateless revision without breaking the
 * handshake era.
 */
class McpProtocol2026Test {
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        encodeDefaults = true
        explicitNulls = false
    }

    @Test
    fun statelessMetaDeclaresVersionIdentityAndCapabilitiesPerRequest() {
        val meta = statelessMeta()

        assertEquals(PROTOCOL_VERSION_2026, meta[META_PROTOCOL_VERSION]?.jsonPrimitive?.content)
        assertEquals("Deneb", meta[META_CLIENT_INFO]?.jsonObject?.get("name")?.jsonPrimitive?.content)
        // Capabilities are per-request and servers must not infer them from an
        // earlier one: the empty object is the declaration, not an omission.
        val capabilities = meta[META_CLIENT_CAPABILITIES]?.jsonObject
        assertNotNull(capabilities)
        assertTrue(capabilities.isEmpty())
    }

    @Test
    fun statelessParamsGainMetadataWhileHandshakeEraParamsAreUntouched() {
        val original = buildJsonObject {
            put("name", JsonPrimitive("wiki_search"))
            put("arguments", buildJsonObject { put("query", JsonPrimitive("백업")) })
        }

        val stateless = withStatelessMeta(original, PROTOCOL_VERSION_2026)?.jsonObject
        assertNotNull(stateless)
        assertNotNull(stateless["_meta"])
        // Existing params survive the rewrite.
        assertEquals("wiki_search", stateless["name"]?.jsonPrimitive?.content)
        assertEquals(
            "백업",
            stateless["arguments"]?.jsonObject?.get("query")?.jsonPrimitive?.content,
        )

        // Older revisions must see byte-identical params.
        assertEquals(original, withStatelessMeta(original, HANDSHAKE_PROTOCOL_VERSION))
        assertEquals(original, withStatelessMeta(original, null))
    }

    @Test
    fun statelessRequestWithoutArgumentsStillCarriesRequiredMetadata() {
        // `_meta` is required on every stateless request, so a method with no
        // params of its own still needs a params object built for it.
        val params = withStatelessMeta(null, PROTOCOL_VERSION_2026)?.jsonObject

        assertNotNull(params)
        assertEquals(
            PROTOCOL_VERSION_2026,
            params["_meta"]?.jsonObject?.get(META_PROTOCOL_VERSION)?.jsonPrimitive?.content,
        )
        assertNull(withStatelessMeta(null, null))
    }

    @Test
    fun mcpNameHeaderSourceIsNameThenUri() {
        val byName = buildJsonObject { put("name", JsonPrimitive("wiki_search")) }
        val byUri = buildJsonObject { put("uri", JsonPrimitive("file:///a.json")) }
        val both = buildJsonObject {
            put("name", JsonPrimitive("wiki_search"))
            put("uri", JsonPrimitive("file:///a.json"))
        }
        val neither = buildJsonObject { put("cursor", JsonPrimitive("page2")) }

        assertEquals("wiki_search", mcpNameFor(byName))
        assertEquals("file:///a.json", mcpNameFor(byUri))
        assertEquals("wiki_search", mcpNameFor(both))
        assertNull(mcpNameFor(neither))
        assertNull(mcpNameFor(null))
        // A non-primitive name has nothing to mirror into a header.
        assertNull(mcpNameFor(buildJsonObject { put("name", buildJsonObject {}) }))
    }

    @Test
    fun headerValuesAreEncodedOnlyWhenPlainAsciiWouldBeUnsafe() {
        // Plain, already header-safe values pass through untouched.
        assertEquals("wiki_search", encodeHeaderValue("wiki_search"))
        assertEquals("file:///a.json", encodeHeaderValue("file:///a.json"))

        // Non-ASCII, whitespace, and control characters must be wrapped.
        for (unsafe in listOf("위키검색", " padded ", "line1\nline2", "")) {
            val encoded = encodeHeaderValue(unsafe)
            assertTrue(encoded.startsWith("=?base64?"), "not encoded: $unsafe -> $encoded")
            assertTrue(encoded.endsWith("?="), "not encoded: $unsafe -> $encoded")
            assertTrue(
                encoded.all { it.code in 0x21..0x7E },
                "encoded value is not header-safe: $encoded",
            )
        }

        // A literal that merely LOOKS like the sentinel must be encoded too, or
        // a server could not tell it from a real one.
        val lookalike = "=?base64?literal?="
        val encodedLookalike = encodeHeaderValue(lookalike)
        assertTrue(encodedLookalike.startsWith("=?base64?"))
        assertFalse(encodedLookalike == lookalike)
    }

    @Test
    fun discoverResultExposesSupportedVersionsAndCapabilities() {
        val decoded = json.decodeFromJsonElement<McpDiscoverResult>(
            json.parseToJsonElement(
                """{
                    "resultType":"complete",
                    "supportedVersions":["2026-07-28","2025-06-18"],
                    "capabilities":{"tools":{"listChanged":false}},
                    "instructions":"read-only memory surface",
                    "ttlMs":300000,
                    "cacheScope":"private",
                    "_meta":{"io.modelcontextprotocol/serverInfo":{"name":"deneb","version":"1"}}
                }""",
            ),
        )

        assertTrue(PROTOCOL_VERSION_2026 in decoded.supportedVersions)
        assertEquals("read-only memory surface", decoded.instructions)
        assertNotNull(decoded.capabilities?.get("tools"))
    }

    @Test
    fun discoverResultFromAServerSpeakingOnlyOlderRevisionsIsNotStateless() {
        val decoded = json.decodeFromJsonElement<McpDiscoverResult>(
            json.parseToJsonElement("""{"supportedVersions":["2025-06-18","2024-11-05"]}"""),
        )

        assertFalse(PROTOCOL_VERSION_2026 in decoded.supportedVersions)
    }

    @Test
    fun toolResultFromAnOlderServerIsTreatedAsComplete() {
        val decoded = json.decodeFromJsonElement<McpCallToolResult>(
            json.parseToJsonElement("""{"content":[{"type":"text","text":"결과"}]}"""),
        )

        // resultType is absent pre-2026-07-28; the spec says read that as
        // "complete", which null does.
        assertNull(decoded.resultType)
        assertFalse(decoded.isError)
        assertEquals("결과", decoded.content.single().text)
    }

    @Test
    fun multiRoundTripResultIsRecognizedAndNamesTheRequestedCapability() {
        val decoded = json.decodeFromJsonElement<McpCallToolResult>(
            json.parseToJsonElement(
                """{
                    "resultType":"input_required",
                    "requestState":"opaque",
                    "inputRequests":{
                        "b":{"method":"elicitation/create"},
                        "a":{"method":"sampling/createMessage"}
                    }
                }""",
            ),
        )

        assertEquals(RESULT_TYPE_INPUT_REQUIRED, decoded.resultType)
        assertTrue(decoded.content.isEmpty())
        // Sorted, so the failure message is stable across map iteration order.
        assertEquals(
            "elicitation/create, sampling/createMessage",
            describeInputRequests(decoded.inputRequests),
        )
    }

    @Test
    fun inputRequestDescriptionDegradesGracefullyWhenNothingIsDeclared() {
        assertEquals("no input requests declared", describeInputRequests(null))
        assertEquals("no input requests declared", describeInputRequests(emptyMap()))
        assertEquals(
            "no input requests declared",
            describeInputRequests(mapOf("a" to McpInputRequest(method = null))),
        )
        assertEquals(
            "no input requests declared",
            describeInputRequests(mapOf("a" to McpInputRequest(method = ""))),
        )
    }

    @Test
    fun handshakeOfferIsTheNewestRevisionThatStillHasInitialize() {
        // 2026-07-28's direct predecessor. Offering an older one would make a
        // server that only speaks 2025-11-25 negotiate down to nothing.
        assertEquals("2025-11-25", HANDSHAKE_PROTOCOL_VERSION)
        assertTrue(HANDSHAKE_PROTOCOL_VERSION in SUPPORTED_HANDSHAKE_VERSIONS)
        assertFalse(PROTOCOL_VERSION_2026 in SUPPORTED_HANDSHAKE_VERSIONS)
    }

    @Test
    fun onlyImplementedHandshakeRevisionsMayBeAdopted() {
        // The negotiated value rides MCP-Protocol-Version on every later
        // request, so adopting one we do not implement would put a lie there.
        for (supported in listOf("2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05")) {
            assertTrue(supported in SUPPORTED_HANDSHAKE_VERSIONS, "should be adoptable: $supported")
        }
        for (unsupported in listOf("2099-01-01", "2026-07-28", "", "garbage")) {
            assertFalse(unsupported in SUPPORTED_HANDSHAKE_VERSIONS, "should be rejected: $unsupported")
        }
    }

    @Test
    fun discoveryProbeIsBoundedWellBelowTheGeneralRequestTimeout() {
        // A handshake-era server may simply stay silent on an unknown method.
        // Without a dedicated cap that silence would spend the whole 60s
        // request budget before the real handshake started.
        assertTrue(
            DISCOVER_PROBE_TIMEOUT_MS in 1_000..15_000,
            "probe timeout $DISCOVER_PROBE_TIMEOUT_MS should be short but not flaky",
        )
    }

    @Test
    fun onlyCompleteAndAbsentResultTypesCountAsFinishedResults() {
        fun decode(raw: String) = json.decodeFromJsonElement<McpCallToolResult>(json.parseToJsonElement(raw))

        // Absent (pre-2026-07-28) and explicit "complete" are finished.
        assertNull(decode("""{"content":[{"type":"text","text":"결과"}]}""").resultType)
        assertEquals(RESULT_TYPE_COMPLETE, decode("""{"resultType":"complete","content":[]}""").resultType)

        // Anything else must be distinguishable so callTool can refuse it —
        // the dangerous shape is an unknown type carrying plausible content.
        val unknown = decode("""{"resultType":"partial","content":[{"type":"text","text":"절반만"}]}""")
        assertEquals("partial", unknown.resultType)
        assertFalse(unknown.resultType == RESULT_TYPE_COMPLETE || unknown.resultType == null)
    }

    @Test
    fun statelessToolCallParamsProduceMatchingBodyAndHeaderValues() {
        // The revision's central header rule: Mcp-Name must equal params.name,
        // so the two are derived from the same object rather than separately.
        val params = withStatelessMeta(
            buildJsonObject {
                put("name", JsonPrimitive("위키검색"))
                put("arguments", buildJsonObject {})
            },
            PROTOCOL_VERSION_2026,
        )
        val bodyName = (params as JsonObject)["name"]?.jsonPrimitive?.content
        val headerName = mcpNameFor(params)

        assertEquals(bodyName, headerName)
        // Korean tool names cannot ride a header as-is; the sentinel carries them.
        assertTrue(encodeHeaderValue(headerName!!).startsWith("=?base64?"))
    }
}
