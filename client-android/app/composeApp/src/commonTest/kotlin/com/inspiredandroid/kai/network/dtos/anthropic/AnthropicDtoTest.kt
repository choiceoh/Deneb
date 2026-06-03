package com.inspiredandroid.kai.network.dtos.anthropic

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertIs
import kotlin.test.assertTrue

class AnthropicDtoTest {

    private val json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
        explicitNulls = false
    }

    @Test
    fun `system splits at Context section for prompt caching`() {
        val sys = "You are Kai.\n\n## Tool Use\nUse tools.\n\n## Context\n- Local time: 2026-06-03T10:00:00+09:00\n"
        val content = anthropicSystemContent(sys)
        val arr = assertIs<JsonArray>(content)
        assertEquals(2, arr.size)
        val first = arr[0].jsonObject
        assertTrue(first["cache_control"] != null, "static prefix block must carry cache_control")
        assertFalse(
            first["text"]!!.jsonPrimitive.content.contains("## Context"),
            "the cached prefix must end before the volatile Context section",
        )
        assertTrue(
            arr[1].jsonObject["text"]!!.jsonPrimitive.content.contains("## Context"),
            "the uncached tail holds the per-request Context",
        )
    }

    @Test
    fun `system without a Context section stays a single string block`() {
        val content = anthropicSystemContent("just a prompt with no context section")
        assertIs<JsonPrimitive>(content)
    }

    @Test
    fun `request serialization produces valid Anthropic format`() {
        val request = AnthropicChatRequestDto(
            model = "claude-sonnet-4-20250514",
            messages = listOf(
                AnthropicChatRequestDto.Message(role = "user", content = JsonPrimitive("Hi")),
            ),
            max_tokens = 1,
        )
        val serialized = json.encodeToString(AnthropicChatRequestDto.serializer(), request)
        val parsed = json.parseToJsonElement(serialized)
        val obj = parsed as kotlinx.serialization.json.JsonObject
        assertEquals("\"claude-sonnet-4-20250514\"", obj["model"].toString())
        assertEquals("1", obj["max_tokens"].toString())
        // system and tools should not be present (explicitNulls = false)
        assertEquals(null, obj["system"])
        assertEquals(null, obj["tools"])
    }

    @Test
    fun `response deserialization handles text content`() {
        val responseJson = """
            {
                "id": "msg_123",
                "type": "message",
                "role": "assistant",
                "content": [
                    {"type": "text", "text": "Hello!"}
                ],
                "model": "claude-sonnet-4-20250514",
                "stop_reason": "end_turn",
                "stop_sequence": null,
                "usage": {"input_tokens": 10, "output_tokens": 5}
            }
        """.trimIndent()
        val response = json.decodeFromString(AnthropicChatResponseDto.serializer(), responseJson)
        assertEquals(1, response.content.size)
        assertEquals("text", response.content[0].type)
        assertEquals("Hello!", response.content[0].text)
        assertEquals("end_turn", response.stop_reason)
        assertEquals("Hello!", response.extractText())
    }

    @Test
    fun `response deserialization handles tool_use content`() {
        val responseJson = """
            {
                "id": "msg_456",
                "type": "message",
                "role": "assistant",
                "content": [
                    {"type": "text", "text": "Let me check that."},
                    {
                        "type": "tool_use",
                        "id": "toolu_abc",
                        "name": "get_weather",
                        "input": {"city": "London"}
                    }
                ],
                "model": "claude-sonnet-4-20250514",
                "stop_reason": "tool_use",
                "usage": {"input_tokens": 20, "output_tokens": 15}
            }
        """.trimIndent()
        val response = json.decodeFromString(AnthropicChatResponseDto.serializer(), responseJson)
        assertEquals(2, response.content.size)
        assertEquals("tool_use", response.content[1].type)
        assertEquals("toolu_abc", response.content[1].id)
        assertEquals("get_weather", response.content[1].name)
        assertEquals("tool_use", response.stop_reason)
        assertEquals("Let me check that.", response.extractText())
    }
}
