package ai.deneb.network.tools

import kotlinx.coroutines.test.runTest
import kotlinx.serialization.ExperimentalSerializationApi
import kotlinx.serialization.MissingFieldException
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith

@OptIn(ExperimentalSerializationApi::class)
class ToolContractTest {

    private val json = Json {
        encodeDefaults = true
        explicitNulls = false
    }

    @Test
    fun toolSchemaSerializationRoundTripPreservesRawParameterContract() {
        val expected = ToolSchema(
            name = "lookup",
            description = "Look up a record",
            parameters = mapOf(
                "query" to ParameterSchema(
                    type = "string",
                    description = "Search query",
                    required = true,
                    rawSchema = buildJsonObject { put("minLength", 1) },
                ),
            ),
        )

        val encoded = json.encodeToString(expected)
        val decoded = json.decodeFromString<ToolSchema>(encoded)

        assertEquals(expected, decoded)
        assertEquals("1", decoded.parameters.getValue("query").rawSchema?.get("minLength").toString())
    }

    @Test
    fun toolSchemaInvalidPayloadRejectsMissingRequiredFields() {
        val failure = assertFailsWith<MissingFieldException> {
            json.decodeFromString<ToolSchema>("""{"name":"lookup","parameters":{}}""")
        }

        assertEquals(listOf("description"), failure.missingFields)
    }

    @Test
    fun toolDefaultTimeoutAndSuspendExecutionPreserveArguments() = runTest {
        val tool = RecordingTool()
        val arguments = mapOf<String, Any>("query" to "Deneb", "limit" to 2)

        val result = tool.execute(arguments)

        assertEquals(30, tool.timeout.inWholeSeconds)
        assertEquals(arguments, tool.received)
        assertEquals(mapOf("accepted" to true, "count" to 2), result)
    }

    private class RecordingTool : Tool {
        override val schema = ToolSchema("record", "Records calls", emptyMap())
        var received: Map<String, Any>? = null

        override suspend fun execute(args: Map<String, Any>): Any {
            received = args
            return mapOf("accepted" to true, "count" to args.size)
        }
    }
}
