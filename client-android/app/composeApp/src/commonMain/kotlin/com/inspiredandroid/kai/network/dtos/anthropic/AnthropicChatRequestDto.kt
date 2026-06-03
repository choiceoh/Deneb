package com.inspiredandroid.kai.network.dtos.anthropic

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.addJsonObject
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonObject

@Serializable
data class AnthropicChatRequestDto(
    val model: String,
    val messages: List<Message>,
    val max_tokens: Int = 8192,
    // String for a plain prompt, or an array of text blocks when a cache_control breakpoint is
    // inserted (see [anthropicSystemContent]). explicitNulls=false drops it when null.
    val system: JsonElement? = null,
    val tools: List<Tool>? = null,
) {
    @Serializable
    data class Message(
        val role: String,
        val content: JsonElement,
    )

    @Serializable
    data class Tool(
        val name: String,
        val description: String,
        val input_schema: InputSchema,
    )

    @Serializable
    data class InputSchema(
        val type: String = "object",
        val properties: Map<String, PropertySchema>,
        val required: List<String> = emptyList(),
    )

    @Serializable
    data class PropertySchema(
        val type: String,
        val description: String? = null,
        val enum: List<String>? = null,
        val items: PropertySchema? = null,
        val properties: Map<String, PropertySchema>? = null,
        val required: List<String>? = null,
    )
}

/**
 * Build the Anthropic `system` value with a prompt-cache breakpoint. The system prompt is split
 * at the "## Context" section (which carries the per-request timestamp): everything before it is
 * static enough to reuse, so it becomes a text block with cache_control=ephemeral; the volatile
 * Context tail follows as a second, uncached block. Falls back to a plain string block when
 * there's no Context marker. Prompt caching is GA on current Claude models, so no beta header is
 * required. Mirrors the static-first ordering that lets the OpenAI-compatible path benefit from
 * vLLM automatic prefix caching.
 */
internal fun anthropicSystemContent(system: String): JsonElement {
    val marker = "\n\n## Context\n"
    val idx = system.lastIndexOf(marker)
    if (idx <= 0) return JsonPrimitive(system)
    return buildJsonArray {
        addJsonObject {
            put("type", "text")
            put("text", system.substring(0, idx))
            putJsonObject("cache_control") { put("type", "ephemeral") }
        }
        addJsonObject {
            put("type", "text")
            put("text", system.substring(idx))
        }
    }
}
