package ai.deneb.mcp

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class McpToolSchemaTest {

    private fun schema(raw: String): JsonObject = Json.parseToJsonElement(raw).jsonObject

    @Test
    fun nullAndMissingPropertiesProduceEmptyParameters() {
        assertEquals(emptyMap(), McpTool.convertInputSchema(null))
        assertEquals(emptyMap(), McpTool.convertInputSchema(schema("{}")))
        assertEquals(emptyMap(), McpTool.convertInputSchema(schema("{\"type\":\"object\"}")))
    }

    @Test
    fun convertsTypeDescriptionAndRequiredMembership() {
        val input = schema(
            """
            {
              "type": "object",
              "properties": {
                "query": {"type": "string", "description": "Search phrase"},
                "limit": {"type": "integer", "description": "Maximum rows"},
                "verbose": {"type": "boolean"}
              },
              "required": ["query", "limit"]
            }
            """.trimIndent(),
        )

        val converted = McpTool.convertInputSchema(input)

        assertEquals(listOf("query", "limit", "verbose"), converted.keys.toList())
        assertEquals("string", converted.getValue("query").type)
        assertEquals("Search phrase", converted.getValue("query").description)
        assertTrue(converted.getValue("query").required)
        assertEquals("integer", converted.getValue("limit").type)
        assertTrue(converted.getValue("limit").required)
        assertEquals("boolean", converted.getValue("verbose").type)
        assertFalse(converted.getValue("verbose").required)
    }

    @Test
    fun omittedTypeAndDescriptionUseSafeDefaults() {
        val converted = McpTool.convertInputSchema(
            schema("""{"properties":{"value":{}}}"""),
        ).getValue("value")

        assertEquals("string", converted.type)
        assertEquals("", converted.description)
        assertFalse(converted.required)
    }

    @Test
    fun rawPropertySchemaIsPreservedForRicherConsumers() {
        val input = schema(
            """
            {
              "properties": {
                "mode": {
                  "type": "string",
                  "enum": ["fast", "safe"],
                  "default": "safe",
                  "minimum": 1
                }
              }
            }
            """.trimIndent(),
        )

        val parameter = McpTool.convertInputSchema(input).getValue("mode")

        assertEquals(input["properties"]?.jsonObject?.get("mode"), parameter.rawSchema)
        assertEquals("safe", parameter.rawSchema?.get("default").toString().trim('"'))
        assertTrue(parameter.rawSchema?.containsKey("enum") == true)
    }

    @Test
    fun malformedIndividualPropertiesAreSkippedWithoutDroppingValidPeers() {
        val input = schema(
            """
            {
              "properties": {
                "good": {"type":"number"},
                "nullValue": null,
                "stringValue": "bad",
                "arrayValue": []
              },
              "required": ["good", "nullValue"]
            }
            """.trimIndent(),
        )

        val converted = McpTool.convertInputSchema(input)

        assertEquals(setOf("good"), converted.keys)
        assertEquals("number", converted.getValue("good").type)
        assertTrue(converted.getValue("good").required)
    }

    @Test
    fun malformedTopLevelPropertiesReturnEmptyInsteadOfThrowing() {
        for (raw in listOf(
            "{\"properties\":null}",
            "{\"properties\":[]}",
            "{\"properties\":\"not-an-object\"}",
            "{\"properties\":42}",
        )) {
            assertEquals(emptyMap(), McpTool.convertInputSchema(schema(raw)), raw)
        }
    }

    @Test
    fun malformedRequiredFieldMakesEveryParameterOptional() {
        for (required in listOf("null", "{}", "\"query\"", "[1,true,{}]")) {
            val input = schema("""{"properties":{"query":{"type":"string"}},"required":$required}""")
            val parameter = McpTool.convertInputSchema(input).getValue("query")
            assertFalse(parameter.required, required)
        }
    }

    @Test
    fun unknownRequiredNamesDoNotCreatePhantomParameters() {
        val input = schema(
            """{"properties":{"known":{"type":"string"}},"required":["missing"]}""",
        )

        val converted = McpTool.convertInputSchema(input)

        assertEquals(setOf("known"), converted.keys)
        assertFalse(converted.getValue("known").required)
        assertNull(converted["missing"])
    }

    @Test
    fun duplicateRequiredNamesRemainASimpleBooleanMembership() {
        val input = schema(
            """{"properties":{"query":{"type":"string"}},"required":["query","query"]}""",
        )

        val converted = McpTool.convertInputSchema(input)

        assertEquals(setOf("query"), converted.keys)
        assertTrue(converted.getValue("query").required)
    }

    @Test
    fun nestedArrayAndObjectConstraintsRemainInRawSchema() {
        val input = schema(
            """
            {
              "properties": {
                "filters": {
                  "type": "array",
                  "items": {
                    "type": "object",
                    "properties": {"field": {"type": "string"}}
                  },
                  "minItems": 1
                }
              }
            }
            """.trimIndent(),
        )

        val parameter = McpTool.convertInputSchema(input).getValue("filters")

        assertEquals("array", parameter.type)
        assertEquals("1", parameter.rawSchema?.get("minItems").toString())
        assertTrue(parameter.rawSchema?.get("items").toString().contains("properties"))
    }

    @Test
    fun toolIdIsDeterministicAndKeepsServerAndToolIdentity() {
        assertEquals("mcp_files_search", McpTool.toolId("files", "search"))
        assertEquals("mcp_server-a_tool/b", McpTool.toolId("server-a", "tool/b"))
        assertEquals("mcp__", McpTool.toolId("", ""))
        assertEquals(McpTool.toolId("s", "t"), McpTool.toolId("s", "t"))
    }
}
