@file:OptIn(ExperimentalEncodingApi::class, ExperimentalTime::class, ExperimentalUuidApi::class)

package ai.deneb.data

import ai.deneb.data.providers.buildAnthropicMessages
import ai.deneb.data.providers.buildOpenAIMessages
import ai.deneb.getAvailableTools
import ai.deneb.network.AnthropicInsufficientCreditsException
import ai.deneb.network.OpenAICompatibleEmptyResponseException
import ai.deneb.network.OpenAICompatibleQuotaExhaustedException
import ai.deneb.network.ServiceCredentials
import ai.deneb.network.dtos.anthropic.extractText
import ai.deneb.network.dtos.gemini.extractText
import ai.deneb.network.dtos.openaicompatible.extractInlineToolCalls
import ai.deneb.network.tools.Tool
import ai.deneb.ui.chat.History
import ai.deneb.ui.chat.ToolCallInfo
import ai.deneb.ui.chat.toGeminiMessageDto
import io.github.vinceglb.filekit.name
import io.github.vinceglb.filekit.size
import kotlinx.collections.immutable.toImmutableList
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.add
import kotlin.io.encoding.ExperimentalEncodingApi
import kotlin.time.Clock
import kotlin.time.Duration.Companion.milliseconds
import kotlin.time.Duration.Companion.seconds
import kotlin.time.ExperimentalTime
import kotlin.uuid.ExperimentalUuidApi
import kotlin.uuid.Uuid

/**
 * Local (upstream) chat pipeline of [RemoteDataRepository]: the per-provider
 * tool loops (OpenAI-compatible / Gemini / Anthropic), retry with backoff,
 * context-window trimming, and LLM history compaction. Extensions split from
 * RemoteDataRepository.kt so the repository file keeps the DataRepository
 * surface while this engine lives in one place. The Deneb gateway client
 * overrides ask(), so this path runs only for directly-configured providers.
 */

private const val MAX_TOOL_ITERATIONS = 15
private const val MIN_TOOL_DISPLAY_MS = 2000L
private const val MAX_REPEATED_TOOL_CALLS = 3
private const val MAX_API_RETRIES = 2

internal const val ESTIMATED_CHARS_PER_TOKEN = 4

private const val COMPACTION_THRESHOLD = 0.7 // Compact when history exceeds 70% of context window
private const val COMPACTION_KEEP_RECENT = 4 // Number of recent user exchanges to keep verbatim

private data class LoopChatResult(
    val textContent: String,
    val reasoningContent: String? = null,
    val isThinkingContent: Boolean = false,
    val toolCalls: List<ToolCallInfo>,
)

/** Final answer from a single assistant turn — text and (optionally) the reasoning trace
 * that produced it. Returned from [askWithService] so the caller can persist both. */
internal data class AssistantTurn(
    val content: String,
    val reasoningContent: String? = null,
)

private enum class BailoutReason { LIMIT_REACHED, REPEATING }

private interface ToolLoopStrategy {
    suspend fun chat(history: List<History>, systemPrompt: String?): LoopChatResult
    suspend fun bailout(history: List<History>, systemPrompt: String?, reason: BailoutReason): String
    fun trimAfterToolResults(history: List<History>, systemPrompt: String?): List<History> = history
}

internal suspend fun RemoteDataRepository.askWithService(
    service: Service,
    messages: List<History>,
    systemPrompt: String?,
    instanceId: String,
    history: MutableStateFlow<List<History>> = chatHistory,
): AssistantTurn {
    val creds = instanceCredentials(instanceId, service)
    val tools = if (supportsTools(creds.modelId)) getAvailableTools() else emptyList()

    return when (service) {
        Service.Gemini -> {
            if (tools.isNotEmpty()) {
                handleGeminiChatWithTools(creds, messages, tools, systemPrompt, history)
            } else {
                val geminiMessages = messages.map { it.toGeminiMessageDto() }
                val response = requests.geminiChat(creds, geminiMessages, systemInstruction = systemPrompt).getOrThrow()
                AssistantTurn(response.extractText())
            }
        }

        Service.Anthropic -> {
            if (tools.isNotEmpty()) {
                handleAnthropicChatWithTools(creds, messages, tools, systemPrompt, history)
            } else {
                val anthropicMessages = buildAnthropicMessages(messages)
                val response = requests.anthropicChat(creds, anthropicMessages, systemInstruction = systemPrompt).getOrThrow()
                AssistantTurn(response.extractText())
            }
        }

        else -> {
            if (tools.isNotEmpty()) {
                handleOpenAICompatibleChatWithTools(service, creds, messages, tools, systemPrompt, history)
            } else {
                // No tools on this request — strip any historic tool_calls so Groq's strict
                // validator doesn't see calls to tools we no longer declare.
                val openAIMessages = buildOpenAIMessages(service, messages, systemPrompt, declaredToolNames = emptySet())
                val message = requests.openAICompatibleChat(service, creds, openAIMessages).getOrThrow()
                    .choices.firstOrNull()?.message ?: throw OpenAICompatibleEmptyResponseException()
                val content = message.effectiveContent ?: throw OpenAICompatibleEmptyResponseException()
                AssistantTurn(content, message.reasoningTraceFor(content))
            }
        }
    }
}

private suspend fun RemoteDataRepository.handleOpenAICompatibleChatWithTools(
    service: Service,
    credentials: ServiceCredentials,
    @Suppress("UNUSED_PARAMETER") messages: List<History>,
    tools: List<Tool>,
    systemPrompt: String? = null,
    history: MutableStateFlow<List<History>> = chatHistory,
): AssistantTurn {
    val contextWindowTokens = ModelCatalog.estimateContextWindow(credentials.modelId)
    val declaredToolNames = tools.map { it.schema.name }.toSet()
    val strategy = object : ToolLoopStrategy {
        override suspend fun chat(history: List<History>, systemPrompt: String?): LoopChatResult {
            val msgs = trimMessagesForContext(buildOpenAIMessages(service, history, systemPrompt, declaredToolNames), contextWindowTokens)
            val response = retryApiCall {
                requests.openAICompatibleChat(
                    service,
                    credentials,
                    msgs,
                    tools,
                ).getOrThrow()
            }
            val message = response.choices.firstOrNull()?.message ?: throw OpenAICompatibleEmptyResponseException()
            var calls = message.toolCalls.orEmpty().map { tc ->
                ToolCallInfo(id = tc.id, name = tc.function.name, arguments = tc.function.arguments)
            }
            var textContent = message.effectiveContent ?: ""
            if (calls.isEmpty() && textContent.contains("<tool_call>")) {
                val extracted = extractInlineToolCalls(textContent, tools)
                if (extracted.calls.isNotEmpty()) {
                    textContent = extracted.cleanedText
                    calls = extracted.calls.map {
                        ToolCallInfo(
                            id = "inline-${Uuid.random()}",
                            name = it.name,
                            arguments = it.arguments,
                        )
                    }
                }
            }
            return LoopChatResult(
                textContent = textContent,
                reasoningContent = message.reasoningTraceFor(textContent),
                isThinkingContent = message.isContentFromReasoning,
                toolCalls = calls,
            )
        }

        override suspend fun bailout(history: List<History>, systemPrompt: String?, reason: BailoutReason): String {
            // Bailout sends no tools — strip historic tool_calls to satisfy strict validators.
            val msgs = trimMessagesForContext(buildOpenAIMessages(service, history, systemPrompt, declaredToolNames = emptySet()), contextWindowTokens)
            return makeFinalCallWithoutTools(service, credentials, msgs)
        }
    }
    return runToolLoop(strategy, systemPrompt, history)
}

private suspend fun RemoteDataRepository.handleGeminiChatWithTools(
    credentials: ServiceCredentials,
    @Suppress("UNUSED_PARAMETER") messages: List<History>,
    tools: List<Tool>,
    systemPrompt: String? = null,
    history: MutableStateFlow<List<History>> = chatHistory,
): AssistantTurn {
    val contextWindowTokens = ModelCatalog.estimateContextWindow(credentials.modelId)
    val strategy = object : ToolLoopStrategy {
        override suspend fun chat(history: List<History>, systemPrompt: String?): LoopChatResult {
            val geminiMessages = history.map { it.toGeminiMessageDto() }
            val response = retryApiCall {
                requests.geminiChat(
                    credentials = credentials,
                    messages = geminiMessages,
                    tools = tools,
                    systemInstruction = systemPrompt,
                ).getOrThrow()
            }
            val parts = response.candidates.firstOrNull()?.content?.parts.orEmpty()
            val partsWithFunctionCalls = parts.filter { it.functionCall != null }
            val toolCallInfos = partsWithFunctionCalls.map { part ->
                val fc = part.functionCall!!
                val argsJson = fc.args?.let { JsonObject(it).toString() } ?: "{}"
                ToolCallInfo(
                    id = "gemini-${Uuid.random()}",
                    name = fc.name,
                    arguments = argsJson,
                    thoughtSignature = part.thoughtSignature,
                )
            }
            val textContent = parts.filterNot { it.isThought }.mapNotNull { it.text }.joinToString("\n")
            return LoopChatResult(textContent = textContent, toolCalls = toolCallInfos)
        }

        override suspend fun bailout(history: List<History>, systemPrompt: String?, reason: BailoutReason): String {
            val prefix = when (reason) {
                BailoutReason.LIMIT_REACHED -> "You have reached the tool call limit. Please respond with the best answer you have so far based on the information gathered."
                BailoutReason.REPEATING -> "You are repeating the same tool calls. Please respond with the best answer you have so far."
            }
            val geminiMessages = history.map { it.toGeminiMessageDto() }
            val bailoutResponse = retryApiCall {
                requests.geminiChat(
                    credentials = credentials,
                    messages = geminiMessages,
                    systemInstruction = "$prefix $systemPrompt",
                ).getOrThrow()
            }
            return bailoutResponse.extractText()
        }

        override fun trimAfterToolResults(history: List<History>, systemPrompt: String?): List<History> = trimHistoryForContext(history, systemPrompt?.length ?: 0, contextWindowTokens)
    }
    return runToolLoop(strategy, systemPrompt, history)
}

private suspend fun RemoteDataRepository.handleAnthropicChatWithTools(
    credentials: ServiceCredentials,
    @Suppress("UNUSED_PARAMETER") messages: List<History>,
    tools: List<Tool>,
    systemPrompt: String? = null,
    history: MutableStateFlow<List<History>> = chatHistory,
): AssistantTurn {
    val contextWindowTokens = ModelCatalog.estimateContextWindow(credentials.modelId)
    val strategy = object : ToolLoopStrategy {
        override suspend fun chat(history: List<History>, systemPrompt: String?): LoopChatResult {
            val msgs = buildAnthropicMessages(history)
            val response = retryApiCall {
                requests.anthropicChat(
                    credentials = credentials,
                    messages = msgs,
                    tools = tools,
                    systemInstruction = systemPrompt,
                ).getOrThrow()
            }
            val toolUseBlocks = response.content.filter { it.type == "tool_use" }
            val toolCallInfos = toolUseBlocks.map { block ->
                val argsJson = block.input?.toString() ?: "{}"
                ToolCallInfo(
                    id = block.id ?: "anthropic-${Uuid.random()}",
                    name = block.name ?: "unknown",
                    arguments = argsJson,
                )
            }
            val textContent = response.content.filter { it.type == "text" }.mapNotNull { it.text }.joinToString("\n")
            return LoopChatResult(textContent = textContent, toolCalls = toolCallInfos)
        }

        override suspend fun bailout(history: List<History>, systemPrompt: String?, reason: BailoutReason): String {
            val prefix = when (reason) {
                BailoutReason.LIMIT_REACHED -> "You have reached the tool call limit. Please respond with the best answer you have so far based on the information gathered."
                BailoutReason.REPEATING -> "You are repeating the same tool calls. Please respond with the best answer you have so far."
            }
            val bailoutResponse = retryApiCall {
                requests.anthropicChat(
                    credentials = credentials,
                    messages = buildAnthropicMessages(history),
                    systemInstruction = "$prefix $systemPrompt",
                ).getOrThrow()
            }
            return bailoutResponse.extractText()
        }

        override fun trimAfterToolResults(history: List<History>, systemPrompt: String?): List<History> = trimHistoryForContext(history, systemPrompt?.length ?: 0, contextWindowTokens)
    }
    return runToolLoop(strategy, systemPrompt, history)
}

private suspend fun RemoteDataRepository.runToolLoop(
    strategy: ToolLoopStrategy,
    systemPrompt: String?,
    history: MutableStateFlow<List<History>>,
): AssistantTurn {
    var iteration = 0
    val recentSignatures = mutableListOf<String>()
    while (true) {
        iteration++
        val visible = history.value.filter { it.role != History.Role.TOOL_EXECUTING }
        if (iteration > MAX_TOOL_ITERATIONS) {
            return AssistantTurn(strategy.bailout(visible, systemPrompt, BailoutReason.LIMIT_REACHED))
        }
        val result = strategy.chat(visible, systemPrompt)
        if (result.toolCalls.isEmpty()) {
            // For thinking-only turns, the reasoning text already became the content via
            // `isContentFromReasoning`, so don't surface it again as a reasoning trace.
            val reasoning = result.reasoningContent?.takeIf { !result.isThinkingContent }
            return AssistantTurn(result.textContent, reasoning)
        }

        val signatures = result.toolCalls.map { "${it.name}:${it.arguments.hashCode()}" }
        if (isRepeatingToolCalls(recentSignatures, signatures)) {
            return AssistantTurn(strategy.bailout(visible, systemPrompt, BailoutReason.REPEATING))
        }
        recentSignatures.addAll(signatures)

        history.update {
            it.toMutableList().apply {
                add(
                    History(
                        role = History.Role.ASSISTANT,
                        content = result.textContent,
                        isThinking = result.isThinkingContent,
                        toolCalls = result.toolCalls.toImmutableList(),
                        reasoningContent = result.reasoningContent,
                    ),
                )
            }
        }

        val toolResults = executeToolCallsInParallel(
            result.toolCalls.map { Triple(it.id, it.name, it.arguments) },
        )

        history.update { h ->
            val merged = buildList(h.size + toolResults.size) {
                for (entry in h) {
                    if (entry.role != History.Role.TOOL_EXECUTING) add(entry)
                }
                for ((callId, name, content) in toolResults) {
                    add(
                        History(
                            role = History.Role.TOOL,
                            content = content,
                            toolCallId = callId,
                            toolName = name,
                        ),
                    )
                }
            }
            strategy.trimAfterToolResults(merged, systemPrompt)
        }
    }
}

/**
 * Detects if the current batch of tool calls is repeating a recent pattern.
 */
private fun RemoteDataRepository.isRepeatingToolCalls(recentSignatures: List<String>, currentSignatures: List<String>): Boolean {
    if (currentSignatures.isEmpty()) return false
    // Count how many consecutive times the same signature set appeared at the tail
    val batchSize = currentSignatures.size
    var consecutiveCount = 0
    var i = recentSignatures.size - batchSize
    while (i >= 0) {
        val slice = recentSignatures.subList(i, i + batchSize)
        if (slice == currentSignatures) {
            consecutiveCount++
            i -= batchSize
        } else {
            break
        }
    }
    // +1 for the current batch that's about to be executed
    return consecutiveCount + 1 >= MAX_REPEATED_TOOL_CALLS
}

/**
 * Makes a final OpenAI-compatible API call without tools, asking the model to summarize.
 */
private suspend fun RemoteDataRepository.makeFinalCallWithoutTools(
    service: Service,
    credentials: ServiceCredentials,
    messages: List<ai.deneb.network.dtos.openaicompatible.OpenAICompatibleChatRequestDto.Message>,
): String {
    val bailoutMessages = messages.toMutableList().apply {
        add(
            ai.deneb.network.dtos.openaicompatible.OpenAICompatibleChatRequestDto.Message(
                role = "user",
                content = JsonPrimitive("You have reached the tool call limit. Please respond with the best answer you have so far based on the information gathered."),
            ),
        )
    }
    val response = retryApiCall {
        requests.openAICompatibleChat(service, credentials, bailoutMessages).getOrThrow()
    }
    return response.choices.firstOrNull()?.message?.effectiveContent ?: ""
}

/**
 * Executes tool calls in parallel, showing TOOL_EXECUTING indicators in the UI.
 * Returns a list of (callId, toolName, result).
 */
private suspend fun RemoteDataRepository.executeToolCallsInParallel(
    toolCalls: List<Triple<String, String, String>>,
): List<Triple<String, String, String>> {
    // Add all TOOL_EXECUTING indicators first
    val executingIds = toolCalls.map { Uuid.random().toString() }
    for ((index, toolCall) in toolCalls.withIndex()) {
        val (_, name, _) = toolCall
        val toolDisplayName = toolExecutor.getToolDisplayName(name)
        chatHistory.update {
            it.toMutableList().apply {
                add(
                    History(
                        id = executingIds[index],
                        role = History.Role.TOOL_EXECUTING,
                        content = name,
                        toolName = toolDisplayName,
                    ),
                )
            }
        }
    }

    // Execute all tools concurrently, ensuring indicators show for at least 2 seconds.
    // Snapshot the conversation id once so all parallel tool calls in this batch
    // see a stable value even if the user switches conversations mid-flight.
    // Prefer an explicit coroutine-context id (set by askWithTools for heartbeat /
    // scheduled runs) over the globally active chat id, so background runs don't
    // leak shell commands into the chat the user is currently viewing.
    val conversationIdSnapshot = currentConversationIdOrNull() ?: _currentConversationId.value
    val startTime = Clock.System.now().toEpochMilliseconds()
    val results = coroutineScope {
        toolCalls.map { (callId, name, arguments) ->
            async {
                val result = toolExecutor.executeTool(name, arguments, conversationIdSnapshot)
                Triple(callId, name, result)
            }
        }.awaitAll()
    }
    val elapsed = Clock.System.now().toEpochMilliseconds() - startTime
    if (elapsed < MIN_TOOL_DISPLAY_MS) {
        delay((MIN_TOOL_DISPLAY_MS - elapsed).milliseconds)
    }

    // Remove all TOOL_EXECUTING indicators
    chatHistory.update { history ->
        history.filter { h -> h.id !in executingIds }
    }

    return results
}

private fun RemoteDataRepository.isNonRetryableException(e: Exception): Boolean = e is AnthropicInsufficientCreditsException || e is OpenAICompatibleQuotaExhaustedException

/**
 * Retries an API call with simple exponential backoff.
 */
internal suspend fun <T> RemoteDataRepository.retryApiCall(block: suspend () -> T): T {
    var lastException: Exception? = null
    for (attempt in 0..MAX_API_RETRIES) {
        try {
            return block()
        } catch (e: Exception) {
            if (e is kotlinx.coroutines.CancellationException) throw e
            if (isNonRetryableException(e)) throw e
            lastException = e
            if (attempt < MAX_API_RETRIES) {
                delay((attempt + 1).seconds)
            }
        }
    }
    throw lastException!!
}

private fun RemoteDataRepository.estimateMessageChars(msg: ai.deneb.network.dtos.openaicompatible.OpenAICompatibleChatRequestDto.Message): Int {
    val contentChars = when (val content = msg.content) {
        is JsonArray -> {
            // Vision messages: only count text parts, not base64 image data
            content.sumOf { element ->
                val obj = element as? JsonObject
                val type = (obj?.get("type") as? JsonPrimitive)?.content
                if (type == "text") {
                    (obj["text"] as? JsonPrimitive)?.content?.length ?: 0
                } else {
                    100 // Fixed small cost for image references
                }
            }
        }

        is JsonPrimitive -> content.content.length

        else -> content?.toString()?.length ?: 0
    }
    return contentChars + msg.role.length
}

/**
 * Trims messages to fit within the estimated context window by dropping oldest messages
 * (keeping the system prompt and most recent messages).
 */
private fun RemoteDataRepository.trimMessagesForContext(
    messages: List<ai.deneb.network.dtos.openaicompatible.OpenAICompatibleChatRequestDto.Message>,
    contextWindowTokens: Int = ModelCatalog.DEFAULT_CONTEXT_WINDOW_TOKENS,
): List<ai.deneb.network.dtos.openaicompatible.OpenAICompatibleChatRequestDto.Message> {
    val maxChars = contextWindowTokens * ESTIMATED_CHARS_PER_TOKEN
    val totalChars = messages.sumOf { estimateMessageChars(it) }
    if (totalChars <= maxChars) return messages

    // Keep system prompt (first message if role is "system") and trim from oldest non-system
    val systemMessages = messages.takeWhile { it.role == "system" }
    val nonSystemMessages = messages.drop(systemMessages.size)

    val systemChars = systemMessages.sumOf { estimateMessageChars(it) }
    val availableChars = maxChars - systemChars

    // Group each assistant tool-call turn together with the tool responses that follow it so
    // trimming never strands one without the other. Strict OpenAI-compatible providers (e.g.
    // DeepSeek via OpenCode Zen) reject an assistant `tool_calls` message that isn't followed
    // by its tool responses, and a `tool` message without a preceding `tool_calls`.
    val groups = mutableListOf<List<ai.deneb.network.dtos.openaicompatible.OpenAICompatibleChatRequestDto.Message>>()
    var index = 0
    while (index < nonSystemMessages.size) {
        val msg = nonSystemMessages[index]
        if (msg.role == "assistant" && !msg.tool_calls.isNullOrEmpty()) {
            var end = index + 1
            while (end < nonSystemMessages.size && nonSystemMessages[end].role == "tool") {
                end++
            }
            groups.add(nonSystemMessages.subList(index, end).toList())
            index = end
        } else {
            groups.add(listOf(msg))
            index++
        }
    }

    // Keep whole groups from the end until we exceed the budget.
    val kept = mutableListOf<ai.deneb.network.dtos.openaicompatible.OpenAICompatibleChatRequestDto.Message>()
    var usedChars = 0
    for (group in groups.asReversed()) {
        val groupChars = group.sumOf { estimateMessageChars(it) }
        if (usedChars + groupChars > availableChars) break
        kept.addAll(0, group)
        usedChars += groupChars
    }

    return systemMessages + kept
}

/**
 * Trims History entries to fit within the estimated context window by dropping oldest messages
 * (keeping the most recent). Used by Gemini and Anthropic tool loops where the system prompt
 * is sent separately (not as a message).
 */
private fun RemoteDataRepository.trimHistoryForContext(
    history: List<History>,
    systemPromptChars: Int = 0,
    contextWindowTokens: Int = ModelCatalog.DEFAULT_CONTEXT_WINDOW_TOKENS,
): List<History> {
    val maxChars = contextWindowTokens * ESTIMATED_CHARS_PER_TOKEN
    val totalChars = history.sumOf { it.content.length } + systemPromptChars
    if (totalChars <= maxChars) return history

    val availableChars = maxChars - systemPromptChars

    // Keep messages from the end until we exceed the budget
    val kept = mutableListOf<History>()
    var usedChars = 0
    for (msg in history.reversed()) {
        val msgChars = msg.content.length
        if (usedChars + msgChars > availableChars) break
        kept.add(0, msg)
        usedChars += msgChars
    }

    return kept
}

/**
 * Compacts chat history by summarizing older messages via an LLM call when the history
 * exceeds a percentage of the context window. Keeps recent exchanges verbatim and replaces
 * older ones with a single summary. Falls back to simple drop-oldest trimming on failure.
 */
internal suspend fun RemoteDataRepository.compactHistoryIfNeeded() {
    // Use primary service's context window for compaction decisions
    val firstInstance = getConfiguredServiceInstances().firstOrNull() ?: return
    val service = Service.fromId(firstInstance.serviceId)
    val modelId = appSettings.getSelectedModelId(service)
    val contextWindowTokens = ModelCatalog.estimateContextWindow(modelId)

    val history = chatHistory.value.filter { it.role != History.Role.TOOL_EXECUTING }
    val systemPromptChars = getActiveSystemPrompt()?.length ?: 0
    val totalChars = history.sumOf { it.content.length } + systemPromptChars
    val maxChars = contextWindowTokens * ESTIMATED_CHARS_PER_TOKEN
    if (totalChars <= (maxChars * COMPACTION_THRESHOLD).toInt()) return

    // Split history: older messages to summarize, recent to keep verbatim
    val userIndices = history.mapIndexedNotNull { index, h ->
        if (h.role == History.Role.USER) index else null
    }
    if (userIndices.size <= COMPACTION_KEEP_RECENT) return
    val cutoffIndex = userIndices[userIndices.size - COMPACTION_KEEP_RECENT]
    val olderMessages = history.subList(0, cutoffIndex)
    val recentMessages = history.subList(cutoffIndex, history.size)

    if (olderMessages.isEmpty()) return

    // Build a transcript of the older messages for summarization
    val transcript = buildString {
        for (msg in olderMessages) {
            if (msg.role == History.Role.USER || msg.role == History.Role.ASSISTANT) {
                val role = if (msg.role == History.Role.USER) "User" else "Assistant"
                appendLine("$role: ${msg.content}")
            }
        }
    }

    val summaryPrompt = "Summarize this conversation concisely, preserving key facts, decisions, and any information the assistant would need to continue helping. Be brief but complete:\n\n$transcript"

    val summary = try {
        askSilently(summaryPrompt)
    } catch (_: Exception) {
        // Summarization failed — fall back to dropping old messages
        chatHistory.value = recentMessages
        return
    }

    val summaryEntry = History(
        role = History.Role.ASSISTANT,
        content = "[Conversation summary: $summary]",
    )

    chatHistory.value = listOf(summaryEntry) + recentMessages
}

internal fun RemoteDataRepository.trimToRecentExchanges(history: List<History>, maxExchanges: Int): List<History> {
    val userIndices = history.mapIndexedNotNull { index, h ->
        if (h.role == History.Role.USER) index else null
    }
    if (userIndices.size <= maxExchanges) return history
    val cutoffIndex = userIndices[userIndices.size - maxExchanges]
    return history.subList(cutoffIndex, history.size)
}
