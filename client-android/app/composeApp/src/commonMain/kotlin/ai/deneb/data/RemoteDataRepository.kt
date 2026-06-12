@file:OptIn(ExperimentalEncodingApi::class, ExperimentalTime::class, ExperimentalUuidApi::class)

package ai.deneb.data

import ai.deneb.SandboxController
import ai.deneb.compressImageBytes
import ai.deneb.currentPlatform
import ai.deneb.data.providers.buildAnthropicMessages
import ai.deneb.data.providers.buildOpenAIMessages
import ai.deneb.email.EmailPoller
import ai.deneb.getAvailableTools
import ai.deneb.getPlatformToolDefinitions
import ai.deneb.mcp.McpServerConfig
import ai.deneb.mcp.McpServerManager
import ai.deneb.network.AllServicesFailedException
import ai.deneb.network.ContextWindowExceededException
import ai.deneb.network.FileTooLargeException
import ai.deneb.network.NoServiceConfiguredException
import ai.deneb.network.OpenAICompatibleEmptyResponseException
import ai.deneb.network.Requests
import ai.deneb.network.ServiceCredentials
import ai.deneb.network.UnsupportedFileTypeException
import ai.deneb.network.dtos.anthropic.extractText
import ai.deneb.network.dtos.gemini.extractText
import ai.deneb.network.toUiError
import ai.deneb.network.tools.Tool
import ai.deneb.network.tools.ToolInfo
import ai.deneb.sms.SmsPoller
import ai.deneb.sms.SmsReader
import ai.deneb.sms.SmsSendResult
import ai.deneb.sms.SmsSender
import ai.deneb.tools.CommonTools
import ai.deneb.tools.SmsPermissionController
import ai.deneb.tools.SmsSendPermissionController
import ai.deneb.ui.chat.History
import ai.deneb.ui.chat.toGeminiMessageDto
import ai.deneb.ui.settings.SettingsModel
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.default_soul
import io.github.vinceglb.filekit.PlatformFile
import io.github.vinceglb.filekit.mimeType
import io.github.vinceglb.filekit.name
import io.github.vinceglb.filekit.readBytes
import io.github.vinceglb.filekit.size
import kotlinx.collections.immutable.persistentListOf
import kotlinx.collections.immutable.toImmutableList
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.withContext
import kotlinx.datetime.TimeZone
import kotlinx.datetime.offsetAt
import kotlinx.datetime.toLocalDateTime
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.add
import kotlinx.serialization.json.jsonObject
import org.jetbrains.compose.resources.getString
import kotlin.io.encoding.Base64
import kotlin.io.encoding.ExperimentalEncodingApi
import kotlin.time.Clock
import kotlin.time.ExperimentalTime
import kotlin.uuid.ExperimentalUuidApi
import kotlin.uuid.Uuid

private const val MAX_HEARTBEAT_MESSAGES = 50

class RemoteDataRepository(
    internal val requests: Requests,
    internal val appSettings: AppSettings,
    private val conversationStorage: ConversationStorage,
    internal val toolExecutor: ToolExecutor,
    private val memoryStore: MemoryStore,
    private val taskStore: TaskStore,
    private val heartbeatManager: HeartbeatManager,
    private val emailStore: EmailStore,
    private val emailPoller: EmailPoller,
    private val smsStore: SmsStore,
    private val smsPoller: SmsPoller,
    private val smsReader: SmsReader,
    private val smsPermissionController: SmsPermissionController,
    private val smsSendPermissionController: SmsSendPermissionController,
    private val smsSender: SmsSender,
    private val smsDraftStore: SmsDraftStore,
    private val mcpServerManager: McpServerManager,
    private val sandboxController: SandboxController,
) : DataRepository {

    private val prettyJson = Json { prettyPrint = true }

    // Per-instance model storage: instanceId -> models flow
    private val modelsByInstance: MutableMap<String, MutableStateFlow<List<SettingsModel>>> = mutableMapOf()

    /** Build credentials from per-instance settings */
    internal fun instanceCredentials(instanceId: String, service: Service): ServiceCredentials = ServiceCredentials(
        apiKey = appSettings.getInstanceApiKey(instanceId),
        modelId = appSettings.getInstanceModelId(instanceId).ifEmpty { appSettings.getSelectedModelId(service) },
        baseUrl = appSettings.getInstanceBaseUrl(instanceId).ifEmpty { appSettings.getBaseUrl(service) },
    )

    override val chatHistory: MutableStateFlow<List<History>> = MutableStateFlow(emptyList())

    internal val _currentConversationId = MutableStateFlow<String?>(null)
    override val currentConversationId: StateFlow<String?> = _currentConversationId

    private val _fallbackStatus = MutableStateFlow<FallbackStatus?>(null)
    override val fallbackStatus: StateFlow<FallbackStatus?> = _fallbackStatus

    override val savedConversations: StateFlow<List<Conversation>> = conversationStorage.conversations

    override fun getConfiguredServiceInstances(): List<ServiceInstance> = appSettings.getConfiguredServiceInstances()

    override fun addConfiguredService(serviceId: String): ServiceInstance {
        val instanceId = appSettings.generateInstanceId(serviceId)
        val instance = ServiceInstance(instanceId = instanceId, serviceId = serviceId)
        val current = appSettings.getConfiguredServiceInstances().toMutableList()
        current.add(instance)
        appSettings.setConfiguredServiceInstances(current)
        return instance
    }

    override fun removeConfiguredService(instanceId: String) {
        val current = appSettings.getConfiguredServiceInstances().toMutableList()
        current.removeAll { it.instanceId == instanceId }
        appSettings.setConfiguredServiceInstances(current)
        appSettings.removeInstanceSettings(instanceId)
        modelsByInstance.remove(instanceId)
    }

    override fun reorderConfiguredServices(orderedInstanceIds: List<String>) {
        val current = appSettings.getConfiguredServiceInstances()
        val byId = current.associateBy { it.instanceId }
        val reordered = orderedInstanceIds.mapNotNull { byId[it] }
        appSettings.setConfiguredServiceInstances(reordered)
    }

    override fun getServiceEntries(): List<ServiceEntry> = getConfiguredServiceInstances().map { instance ->
        val service = Service.fromId(instance.serviceId)
        val modelId = appSettings.getInstanceModelId(instance.instanceId).ifEmpty {
            appSettings.getSelectedModelId(service)
        }
        ServiceEntry(
            instanceId = instance.instanceId,
            serviceId = service.id,
            serviceName = service.displayName,
            modelId = modelId,
            icon = service.icon,
        )
    }

    // Per-instance settings
    override fun getInstanceApiKey(instanceId: String): String = appSettings.getInstanceApiKey(instanceId)

    override fun updateInstanceApiKey(instanceId: String, apiKey: String) {
        appSettings.setInstanceApiKey(instanceId, apiKey)
    }

    override fun getInstanceBaseUrl(instanceId: String, service: Service): String {
        val url = appSettings.getInstanceBaseUrl(instanceId)
        return url.ifBlank { if (service is Service.OpenAICompatible) Service.DEFAULT_OPENAI_COMPATIBLE_BASE_URL else "" }
    }

    override fun updateInstanceBaseUrl(instanceId: String, baseUrl: String) {
        appSettings.setInstanceBaseUrl(instanceId, baseUrl)
    }

    override fun getInstanceModels(instanceId: String, service: Service): StateFlow<List<SettingsModel>> = modelsByInstance.getOrPut(instanceId) {
        val selectedModelId = appSettings.getInstanceModelId(instanceId)
        val defaultSettingsModels = service.defaultModels.map {
            SettingsModel(
                id = it.id,
                subtitle = it.subtitle,
                descriptionRes = it.descriptionRes,
                isSelected = it.id == selectedModelId,
            )
        }
        val models = if (selectedModelId.isNotEmpty() && defaultSettingsModels.none { it.id == selectedModelId }) {
            listOf(SettingsModel(id = selectedModelId, subtitle = "", isSelected = true)) + defaultSettingsModels
        } else {
            defaultSettingsModels
        }
        MutableStateFlow(models)
    }

    override fun updateInstanceSelectedModel(instanceId: String, service: Service, modelId: String) {
        appSettings.setInstanceModelId(instanceId, modelId)
        modelsByInstance[instanceId]?.update { models ->
            models.map { it.copy(isSelected = it.id == modelId) }
        }
    }

    override fun clearInstanceModels(instanceId: String, service: Service) {
        modelsByInstance[instanceId]?.update { emptyList() }
    }

    override suspend fun validateConnection(service: Service, instanceId: String) {
        val creds = instanceCredentials(instanceId, service)
        when (service) {
            Service.OpenRouter -> {
                requests.validateOpenRouterApiKey(creds).getOrThrow()
                fetchInstanceModels(service, instanceId)
            }

            else -> fetchInstanceModels(service, instanceId)
        }
    }

    private suspend fun fetchInstanceModels(service: Service, instanceId: String) {
        when (service) {
            Service.Gemini -> fetchGeminiModelsForInstance(instanceId)

            Service.Anthropic -> fetchAnthropicModelsForInstance(instanceId)

            else -> {
                if (service.modelsUrl != null) {
                    fetchOpenAICompatibleModelsForInstance(service, instanceId)
                } else if (service.defaultModels.isNotEmpty()) {
                    val selectedModelId = appSettings.getInstanceModelId(instanceId)
                    val models = service.defaultModels.map {
                        SettingsModel(
                            id = it.id,
                            subtitle = it.subtitle,
                            descriptionRes = it.descriptionRes,
                            isSelected = it.id == selectedModelId,
                        )
                    }
                    updateModelsForInstance(instanceId, models, service)
                }
            }
        }
    }

    private suspend fun fetchAnthropicModelsForInstance(instanceId: String) {
        val creds = instanceCredentials(instanceId, Service.Anthropic)
        val response = requests.getAnthropicModels(creds).getOrThrow()
        val selectedModelId = appSettings.getInstanceModelId(instanceId)
        val models = mapAnthropicModels(response.data, selectedModelId)
        updateModelsForInstance(instanceId, models)
    }

    private suspend fun fetchGeminiModelsForInstance(instanceId: String) {
        val creds = instanceCredentials(instanceId, Service.Gemini)
        val response = requests.getGeminiModels(creds).getOrThrow()
        val selectedModelId = appSettings.getInstanceModelId(instanceId)
        val models = mapGeminiModels(response.models, selectedModelId)
        updateModelsForInstance(instanceId, models)
    }

    private suspend fun fetchOpenAICompatibleModelsForInstance(service: Service, instanceId: String) {
        val creds = instanceCredentials(instanceId, service)
        val response = requests.getOpenAICompatibleModels(service, creds).getOrThrow()
        val selectedModelId = appSettings.getInstanceModelId(instanceId)
        val models = mapOpenAICompatibleModels(response.data, service, selectedModelId)
        updateModelsForInstance(instanceId, models)
    }

    private fun updateModelsForInstance(instanceId: String, models: List<SettingsModel>, service: Service? = null) {
        val flow = modelsByInstance.getOrPut(instanceId) { MutableStateFlow(emptyList()) }
        flow.update { models }
        if (models.isNotEmpty() && models.none { it.isSelected }) {
            val default = pickDefaultModel(models, service)
            if (default != null) {
                appSettings.setInstanceModelId(instanceId, default.id)
                flow.update { m -> m.map { it.copy(isSelected = it.id == default.id) } }
            }
        }
    }

    private fun pickDefaultModel(models: List<SettingsModel>, service: Service? = null): SettingsModel? {
        val defaultModel = service?.defaultModel
        if (defaultModel != null) {
            models.firstOrNull { it.id == defaultModel }?.let { return it }
        }
        return models.firstOrNull { it.id.contains("kimi-k2.5", ignoreCase = true) }
            ?: models.firstOrNull()
    }

    private fun hasValidInstanceApiKey(instanceId: String, service: Service): Boolean {
        if (!service.requiresApiKey && !service.supportsOptionalApiKey) return true
        if (service.requiresApiKey) return appSettings.getInstanceApiKey(instanceId).isNotBlank()
        return true // Optional API key services are always valid
    }

    private data class FallbackEntry(val instanceId: String, val service: Service)

    private fun getOrderedFallbackEntries(): List<FallbackEntry> {
        val instances = getConfiguredServiceInstances()
        val entries = instances.map { FallbackEntry(instanceId = it.instanceId, service = Service.fromId(it.serviceId)) }
        return entries
    }

    override suspend fun ask(question: String?, files: List<PlatformFile>, uiSubmission: UiSubmission?) {
        // Allocate a conversation id immediately for fresh chats. Without this,
        // the very first tool call lands here with _currentConversationId.value
        // still null, so per-conversation routing (e.g. the sandbox shell)
        // falls through to a shared default — which both makes the new chat
        // invisible in the Terminal session picker and lets unrelated callers
        // collide on the same shell mutex. Persistence is deferred to the
        // existing saveCurrentConversation() flow that runs after the response.
        if (_currentConversationId.value == null) {
            setCurrentConversationId(Uuid.random().toString())
        }
        // Process every attached file: classify, compress/encode, and build an Attachment.
        // readBytes() is suspend, so this happens before the StateFlow.update block.
        val attachments = files.map { file ->
            val fileMimeType = file.mimeType()?.toString()
            val fileName = file.name

            val category = classifyFile(fileMimeType, fileName)
            if (category == FileCategory.UNSUPPORTED) throw UnsupportedFileTypeException()

            // Reject oversized files by stat size before readBytes(), which would otherwise
            // allocate a ByteArray large enough to OOM the process on multi-GB inputs.
            val rawSizeLimit = when (category) {
                FileCategory.TEXT -> MAX_TEXT_FILE_BYTES.toLong()
                FileCategory.PDF -> MAX_PDF_BYTES.toLong()
                FileCategory.IMAGE -> MAX_RAW_IMAGE_BYTES.toLong()
                FileCategory.UNSUPPORTED -> 0L
            }
            if (file.size() > rawSizeLimit) throw FileTooLargeException()

            val rawBytes = file.readBytes()

            when (category) {
                FileCategory.IMAGE -> {
                    val compressed = compressImageBytes(rawBytes, fileMimeType ?: "image/jpeg")
                    // compressImageBytes can fall back to the original bytes on failure or on
                    // platforms without compression — guard against Base64 OOM for oversized input.
                    if (compressed.size > MAX_IMAGE_BYTES) throw FileTooLargeException()
                    Attachment(
                        data = Base64.encode(compressed),
                        mimeType = "image/jpeg",
                        fileName = null,
                    )
                }

                FileCategory.TEXT -> Attachment(
                    data = Base64.encode(rawBytes),
                    mimeType = fileMimeType ?: "text/plain",
                    fileName = fileName,
                )

                FileCategory.PDF -> Attachment(
                    data = Base64.encode(rawBytes),
                    mimeType = "application/pdf",
                    fileName = fileName,
                )

                FileCategory.UNSUPPORTED -> throw UnsupportedFileTypeException()
            }
        }.toImmutableList()

        if (question != null) {
            chatHistory.update {
                it.toMutableList().apply {
                    add(
                        History(
                            role = History.Role.USER,
                            content = question,
                            attachments = attachments,
                            uiSubmission = uiSubmission,
                        ),
                    )
                }
            }
        }

        compactHistoryIfNeeded()

        val messages = chatHistory.value
        val systemPrompt = getActiveSystemPrompt()

        val fallbackEntries = getOrderedFallbackEntries().filter { hasValidInstanceApiKey(it.instanceId, it.service) }
        if (fallbackEntries.isEmpty()) throw NoServiceConfiguredException()

        val historyChars = messages.sumOf { it.content.length } + (systemPrompt?.length ?: 0)

        var lastException: Exception? = null
        var fallbackServiceName: String? = null

        try {
            for ((index, entry) in fallbackEntries.withIndex()) {
                // Skip fallback services whose context window is too small for the current history.
                val creds = instanceCredentials(entry.instanceId, entry.service)
                val entryWindowChars = ModelCatalog.estimateContextWindow(creds.modelId) * ESTIMATED_CHARS_PER_TOKEN
                if (historyChars > entryWindowChars) {
                    lastException = ContextWindowExceededException()
                    _fallbackStatus.value = FallbackStatus(
                        serviceName = entry.service.displayName,
                        errorReason = ContextWindowExceededException().toUiError(),
                        nextServiceName = fallbackEntries.getOrNull(index + 1)?.service?.displayName,
                    )
                    continue
                }

                val turn = try {
                    retryApiCall {
                        askWithService(entry.service, messages, systemPrompt, entry.instanceId)
                    }
                } catch (e: Exception) {
                    if (e is kotlinx.coroutines.CancellationException) throw e
                    lastException = e
                    _fallbackStatus.value = FallbackStatus(
                        serviceName = entry.service.displayName,
                        errorReason = e.toUiError(),
                        nextServiceName = fallbackEntries.getOrNull(index + 1)?.service?.displayName,
                    )
                    continue
                }
                if (index > 0) {
                    fallbackServiceName = entry.service.displayName
                }
                chatHistory.update {
                    it.toMutableList().apply {
                        add(
                            History(
                                role = History.Role.ASSISTANT,
                                content = turn.content,
                                reasoningContent = turn.reasoningContent,
                                fallbackServiceName = fallbackServiceName,
                            ),
                        )
                    }
                }
                saveCurrentConversation()
                return
            }

            throw if (fallbackEntries.size > 1 && lastException != null) {
                AllServicesFailedException()
            } else {
                lastException ?: OpenAICompatibleEmptyResponseException()
            }
        } finally {
            _fallbackStatus.value = null
        }
    }

    private suspend fun saveCurrentConversation() {
        val history = trimToRecentExchanges(chatHistory.value, 20)
        if (history.isEmpty()) return

        val now = Clock.System.now().toEpochMilliseconds()
        val conversationId = _currentConversationId.value ?: Uuid.random().toString().also {
            setCurrentConversationId(it)
        }

        val existingConversation = savedConversations.value.find { it.id == conversationId }

        val title = existingConversation?.title?.ifEmpty { null }
            ?: deriveTitle(history)
        val conversation = Conversation(
            id = conversationId,
            messages = history
                .filter { it.role != History.Role.TOOL_EXECUTING }
                .map { h ->
                    Conversation.Message(
                        id = h.id,
                        role = when (h.role) {
                            History.Role.USER -> "user"
                            History.Role.ASSISTANT -> "assistant"
                            History.Role.TOOL -> "tool"
                            History.Role.TOOL_EXECUTING -> "tool" // Should not happen due to filter
                        },
                        content = h.content,
                        attachments = h.attachments,
                        uiSubmission = h.uiSubmission,
                        isThinking = h.isThinking,
                        reasoningContent = h.reasoningContent,
                    )
                },
            createdAt = existingConversation?.createdAt ?: now,
            updatedAt = now,
            title = title,
            type = existingConversation?.type ?: if (interactiveModeFlag) Conversation.TYPE_INTERACTIVE else Conversation.TYPE_CHAT,
        )

        conversationStorage.saveConversation(conversation)
    }

    override fun clearHistory() {
        chatHistory.update {
            emptyList()
        }
    }

    override fun supportedFileExtensions(): List<String> {
        val service = currentService()
        val base = if (service.supportsImages) supportedFileExtensions else supportedFileExtensions - imageExtensions
        return if (service.supportsPdf) base + "pdf" else base
    }

    override fun currentService(): Service {
        val instances = getConfiguredServiceInstances()
        return instances.firstOrNull()?.let { Service.fromId(it.serviceId) } ?: Service.OpenAI
    }

    private fun setCurrentConversationId(id: String?) {
        _currentConversationId.value = id
        appSettings.setCurrentConversationId(id)
    }

    // Conversation management
    override fun loadConversations() {
        conversationStorage.loadConversations()
    }

    override fun loadConversation(id: String) {
        val conversation = savedConversations.value.find { it.id == id } ?: return

        setCurrentConversationId(id)
        chatHistory.value = conversation.messages.map { m ->
            // Prefer the modern `attachments` field. Fall back to the legacy single-file
            // fields for conversations saved before multi-attachment support.
            val attachments = when {
                m.attachments.isNotEmpty() -> m.attachments.toImmutableList()

                m.data != null && m.mimeType != null ->
                    persistentListOf(Attachment(data = m.data, mimeType = m.mimeType, fileName = m.fileName))

                else -> persistentListOf()
            }
            History(
                id = m.id,
                role = when (m.role) {
                    "user" -> History.Role.USER
                    "tool" -> History.Role.TOOL
                    else -> History.Role.ASSISTANT
                },
                content = m.content,
                attachments = attachments,
                uiSubmission = m.uiSubmission,
                isThinking = m.isThinking,
                reasoningContent = m.reasoningContent,
            )
        }
    }

    override suspend fun deleteConversation(id: String) {
        if (_currentConversationId.value == id) {
            setCurrentConversationId(null)
            chatHistory.value = emptyList()
        }
        conversationStorage.deleteConversation(id)
        // Drop the per-conversation shell session so a future conversation reusing
        // this id (very unlikely — random uuids) doesn't inherit stale state, and
        // memory is freed.
        sandboxController.closeSession(id)
    }

    override fun regenerate() {
        chatHistory.update { history ->
            val lastUserIndex = history.indexOfLast { it.role == History.Role.USER }
            if (lastUserIndex >= 0) {
                history.subList(0, lastUserIndex + 1)
            } else {
                history
            }
        }
    }

    override fun startNewChat() {
        setCurrentConversationId(null)
        chatHistory.value = emptyList()
    }

    override fun popLastExchange() {
        chatHistory.update { history ->
            val lastUserIndex = history.indexOfLast { it.role == History.Role.USER }
            if (lastUserIndex >= 0) history.take(lastUserIndex) else history
        }
    }

    override fun truncateFrom(messageId: String) {
        chatHistory.update { history ->
            val index = history.indexOfFirst { it.id == messageId }
            if (index >= 0) history.take(index) else history
        }
    }

    override fun restoreCurrentConversation() {
        // One-time migration for existing users: pin the latest conversation as the new
        // "current" pointer so the upgrade is non-disruptive.
        if (!appSettings.isCurrentConversationMigrated()) {
            val latest = savedConversations.value.maxByOrNull { it.updatedAt }
            if (latest != null) {
                loadConversation(latest.id)
            }
            appSettings.markCurrentConversationMigrated()
            return
        }

        // Already-loaded guard (covers re-entry from refreshSettings)
        val currentId = _currentConversationId.value
        if (currentId != null && chatHistory.value.isNotEmpty() &&
            savedConversations.value.any { it.id == currentId }
        ) {
            return
        }

        val persistedId = appSettings.getCurrentConversationId()
        if (persistedId != null && savedConversations.value.any { it.id == persistedId }) {
            loadConversation(persistedId)
        }
        // else: null id or stale id → leave history empty (this is the new-empty-chat state)
    }

    // Tool management
    override fun getToolDefinitions(): List<ToolInfo> = getPlatformToolDefinitions()
        .filter { it.id !in CommonTools.masterToggleControlledToolIds }
        .map { it.copy(isEnabled = appSettings.isToolEnabled(it.id, defaultEnabled = it.isEnabled)) }

    override fun setToolEnabled(toolId: String, enabled: Boolean) {
        appSettings.setToolEnabled(toolId, enabled)
    }

    // MCP servers
    override fun getMcpServers(): List<McpServerConfig> = mcpServerManager.getServers()

    override suspend fun addMcpServer(name: String, url: String, headers: Map<String, String>): McpServerConfig = mcpServerManager.addServer(name, url, headers)

    override fun removeMcpServer(serverId: String) {
        mcpServerManager.removeServer(serverId)
    }

    override fun setMcpServerEnabled(serverId: String, enabled: Boolean) {
        mcpServerManager.setServerEnabled(serverId, enabled)
    }

    override suspend fun connectMcpServer(serverId: String): Result<List<ToolInfo>> {
        val result = mcpServerManager.connectAndDiscoverTools(serverId)
        return result.map { mcpServerManager.getToolsForServer(serverId) }
    }

    override fun getMcpToolsForServer(serverId: String): List<ToolInfo> = mcpServerManager.getToolsForServer(serverId)

    override fun isMcpServerConnected(serverId: String): Boolean = mcpServerManager.isConnected(serverId)

    override suspend fun connectEnabledMcpServers() {
        mcpServerManager.connectEnabledServers()
    }

    // Soul (system prompt)
    override fun getSoulText(): String = appSettings.getSoulText()

    override fun setSoulText(text: String) {
        appSettings.setSoulText(text)
    }

    override suspend fun getActiveSystemPrompt(variant: SystemPromptVariant): String? {
        val soul = appSettings.getSoulText().ifEmpty { getString(Res.string.default_soul) }
        val memoryEnabled = appSettings.isMemoryEnabled()
        val schedulingEnabled = appSettings.isSchedulingEnabled()

        val memoryInstructions = if (memoryEnabled) {
            appSettings.getMemoryInstructions().ifEmpty { null }
        } else {
            null
        }

        val memories = if (memoryEnabled) memoryStore.getAllMemories() else emptyList()
        val byCategory = memories.groupBy { it.category }

        val tasksSplit = if (schedulingEnabled) taskStore.getPendingTasksPartitioned() else PendingTaskPartition(emptyList(), emptyList())
        val pendingTasks = tasksSplit.scheduled
        val heartbeatAdditions = tasksSplit.heartbeatAdditions

        // Surface connected email accounts so the AI knows they exist in regular chat,
        // not just during heartbeats. Only the remote variant uses this — email tools
        // aren't in the local allowlist. Gated on the email toggle: if the user has email
        // off, the AI shouldn't reference the accounts.
        val emailAccounts = if (variant == SystemPromptVariant.CHAT_REMOTE && appSettings.isEmailEnabled()) {
            emailStore.getAccounts().map { account ->
                val state = emailStore.getSyncState(account.id)
                EmailAccountSummary(
                    email = account.email,
                    unreadCount = state.unreadCount,
                    lastSyncEpochMs = state.lastSyncEpochMs,
                    lastError = state.lastError,
                )
            }
        } else {
            emptyList()
        }

        val service = currentService()
        val modelId = appSettings.getSelectedModelId(service)
        val now = Clock.System.now()
        val timeZone = TimeZone.currentSystemDefault()
        val localDateTime = now.toLocalDateTime(timeZone)
        val offset = timeZone.offsetAt(now)
        val runtime = ChatPromptRuntimeContext(
            nowLocalIsoWithOffset = "$localDateTime$offset",
            timeZoneId = timeZone.id,
            nowUtcIsoString = now.toString(),
            platform = currentPlatform.displayName,
            modelId = modelId,
            providerName = service.displayName,
        )

        val isLimited = !supportsTools(modelId)
        val uiMode = when {
            interactiveModeFlag -> ChatPromptUiMode.INTERACTIVE_UI
            appSettings.isDynamicUiEnabled() && !isLimited -> ChatPromptUiMode.DYNAMIC_UI
            else -> ChatPromptUiMode.NONE
        }

        // Tool-use guidance is only worth sending when the model is actually given tools.
        // The remote variant uses the full tool set (when the model supports tools); the
        // CHAT_LOCAL variant is no longer produced for any configured service, so it never
        // carries tools.
        val hasTools = when (variant) {
            SystemPromptVariant.CHAT_REMOTE -> !isLimited && getAvailableTools().isNotEmpty()
            SystemPromptVariant.CHAT_LOCAL -> false
        }

        return buildChatSystemPrompt(
            variant = variant,
            soul = soul,
            hasTools = hasTools,
            memoryEnabled = memoryEnabled,
            schedulingEnabled = schedulingEnabled,
            memoryInstructions = memoryInstructions,
            generalMemories = byCategory[MemoryCategory.GENERAL].orEmpty(),
            preferenceMemories = byCategory[MemoryCategory.PREFERENCE].orEmpty(),
            learningMemories = byCategory[MemoryCategory.LEARNING].orEmpty(),
            errorMemories = byCategory[MemoryCategory.ERROR].orEmpty(),
            pendingTasks = pendingTasks,
            heartbeatAdditions = heartbeatAdditions,
            emailAccounts = emailAccounts,
            runtime = runtime,
            uiMode = uiMode,
        ).ifEmpty { null }
    }

    override fun isDynamicUiEnabled(): Boolean = appSettings.isDynamicUiEnabled()

    override fun setDynamicUiEnabled(enabled: Boolean) {
        appSettings.setDynamicUiEnabled(enabled)
    }

    override fun getThemeMode(): ThemeMode = appSettings.getThemeMode()

    override fun setThemeMode(mode: ThemeMode) {
        appSettings.setThemeMode(mode)
    }

    internal var interactiveModeFlag = appSettings.getCurrentInteractiveMode()

    override fun setInteractiveMode(enabled: Boolean) {
        interactiveModeFlag = enabled
        appSettings.setCurrentInteractiveMode(enabled)
    }

    override fun isInteractiveModeActive(): Boolean = interactiveModeFlag

    override fun isMemoryEnabled(): Boolean = appSettings.isMemoryEnabled()

    override fun setMemoryEnabled(enabled: Boolean) {
        appSettings.setMemoryEnabled(enabled)
    }

    override fun getMemories(): List<MemoryEntry> = memoryStore.getAllMemories()

    override suspend fun deleteMemory(key: String) {
        memoryStore.forget(key)
    }

    override suspend fun updateMemoryContent(key: String, content: String) {
        memoryStore.updateContent(key, content)
    }

    override fun isSchedulingEnabled(): Boolean = appSettings.isSchedulingEnabled()

    override fun setSchedulingEnabled(enabled: Boolean) {
        appSettings.setSchedulingEnabled(enabled)
    }

    override fun getScheduledTasks(): List<ScheduledTask> = taskStore.getAllTasks()

    override suspend fun cancelScheduledTask(id: String) {
        taskStore.removeTask(id)
    }

    override fun isDaemonEnabled(): Boolean = appSettings.isDaemonEnabled()

    override fun setDaemonEnabled(enabled: Boolean) {
        appSettings.setDaemonEnabled(enabled)
    }

    override fun isSandboxEnabled(): Boolean = appSettings.isSandboxEnabled()

    override fun setSandboxEnabled(enabled: Boolean) {
        appSettings.setSandboxEnabled(enabled)
    }

    override fun getHeartbeatConfig(): HeartbeatConfig = heartbeatManager.getConfig()

    override fun setHeartbeatEnabled(enabled: Boolean) {
        val config = heartbeatManager.getConfig()
        heartbeatManager.saveConfig(config.copy(enabled = enabled))
    }

    override fun setHeartbeatIntervalMinutes(minutes: Int) {
        val config = heartbeatManager.getConfig()
        heartbeatManager.saveConfig(config.copy(intervalMinutes = minutes))
    }

    override fun setHeartbeatActiveHours(start: Int, end: Int) {
        val config = heartbeatManager.getConfig()
        heartbeatManager.saveConfig(config.copy(activeHoursStart = start, activeHoursEnd = end))
    }

    override fun getHeartbeatPrompt(): String = appSettings.getHeartbeatPrompt()

    override fun setHeartbeatPrompt(text: String) {
        appSettings.setHeartbeatPrompt(text)
    }

    override fun getHeartbeatLog(): List<HeartbeatLogEntry> = heartbeatManager.getHeartbeatLog()

    override fun getHeartbeatInstanceId(): String? = heartbeatManager.getConfig().heartbeatInstanceId

    override fun setHeartbeatInstanceId(instanceId: String?) {
        val config = heartbeatManager.getConfig()
        heartbeatManager.saveConfig(config.copy(heartbeatInstanceId = instanceId))
    }

    override fun isEmailEnabled(): Boolean = appSettings.isEmailEnabled()

    override fun setEmailEnabled(enabled: Boolean) {
        appSettings.setEmailEnabled(enabled)
    }

    override fun getEmailAccounts(): List<EmailAccount> = emailStore.getAccounts()

    override suspend fun removeEmailAccount(id: String) {
        emailStore.removeAccount(id)
    }

    override fun getEmailPollIntervalMinutes(): Int = appSettings.getEmailPollIntervalMinutes()

    override fun getPendingEmailCount(): Int = emailStore.getPending().size

    override fun getEmailSyncStates(): Map<String, EmailSyncState> = emailStore.getAllSyncStates()

    override suspend fun pollEmailAccount(accountId: String) {
        val account = emailStore.getAccount(accountId) ?: return
        emailPoller.poll(account)
    }

    override fun setEmailPollIntervalMinutes(minutes: Int) {
        appSettings.setEmailPollIntervalMinutes(minutes)
    }

    override fun isSmsEnabled(): Boolean = appSettings.isSmsEnabled()

    override fun setSmsEnabled(enabled: Boolean) {
        appSettings.setSmsEnabled(enabled)
    }

    override fun getSmsPollIntervalMinutes(): Int = appSettings.getSmsPollIntervalMinutes()

    override fun setSmsPollIntervalMinutes(minutes: Int) {
        appSettings.setSmsPollIntervalMinutes(minutes)
    }

    override fun getPendingSmsCount(): Int = smsStore.getPending().size

    override fun getSmsSyncState(): SmsSyncState = smsStore.getSyncState()

    override fun hasSmsPermission(): Boolean = smsReader.hasPermission()

    override suspend fun requestSmsPermission(): Boolean = smsPermissionController.requestPermission()

    override suspend fun pollSms() {
        smsPoller.poll()
    }

    override fun isSmsSendEnabled(): Boolean = appSettings.isSmsSendEnabled()

    override fun setSmsSendEnabled(enabled: Boolean) {
        appSettings.setSmsSendEnabled(enabled)
    }

    override fun hasSmsSendPermission(): Boolean = smsSender.hasPermission()

    override suspend fun requestSmsSendPermission(): Boolean = smsSendPermissionController.requestPermission()

    override val smsDrafts: StateFlow<List<SmsDraft>> = smsDraftStore.drafts

    // Flips the draft to SENDING, delegates to SmsSender, then updates to SENT/FAILED.
    // Explicit user-triggered (never AI-triggered) — the banner is the gate.
    override suspend fun sendSmsDraft(draftId: String): Boolean {
        val draft = smsDraftStore.getDraft(draftId) ?: return false
        if (draft.status != SmsDraftStatus.PENDING) return false
        smsDraftStore.updateStatus(draftId, SmsDraftStatus.SENDING)
        return when (val result = smsSender.send(draft.address, draft.body)) {
            is SmsSendResult.Success -> {
                smsDraftStore.updateStatus(draftId, SmsDraftStatus.SENT)
                true
            }

            is SmsSendResult.Failure -> {
                smsDraftStore.updateStatus(draftId, SmsDraftStatus.FAILED, result.message)
                false
            }
        }
    }

    override suspend fun discardSmsDraft(draftId: String) {
        smsDraftStore.removeDraft(draftId)
    }

    override fun getUiScale(): Float = appSettings.getUiScale()

    override fun setUiScale(scale: Float) {
        appSettings.setUiScale(scale)
    }

    override fun exportSettingsToJson(sections: Set<ImportSection>): String {
        val toolIds = getPlatformToolDefinitions().map { it.id }
        val jsonObject = appSettings.exportToJson(toolIds, sections)
        return prettyJson.encodeToString(JsonObject.serializer(), jsonObject)
    }

    override fun getExportPreview(): Map<ImportSection, String?> {
        val toolIds = getPlatformToolDefinitions().map { it.id }
        val jsonObject = appSettings.exportToJson(toolIds)
        return detectExportableSections(jsonObject)
    }

    override fun importSettingsFromJson(json: String, sections: Set<ImportSection>, replace: Boolean): Int {
        val jsonObject = SharedJson.parseToJsonElement(json).jsonObject
        val toolIds = getPlatformToolDefinitions().map { it.id }
        return appSettings.importFromJson(jsonObject, toolIds, sections, replace)
    }

    override suspend fun askWithTools(prompt: String, instanceId: String?, conversationIdOverride: String?): String {
        // Selection: explicit instance > first configured instance.
        val instances = getConfiguredServiceInstances()
        val targetInstance = instanceId?.let { id -> instances.find { it.instanceId == id } }
            ?: instances.firstOrNull()
            ?: return ""
        val service = Service.fromId(targetInstance.serviceId)
        val messages = listOf(History(role = History.Role.USER, content = prompt))
        val systemPrompt = getActiveSystemPrompt()
        // Use a local history to avoid polluting the current conversation's chatHistory
        val localHistory = MutableStateFlow(messages)
        // When a conversation override is set (heartbeat / scheduled tasks), bind any
        // tool calls in this run to that conversation's sandbox session via the
        // coroutine context — otherwise tool dispatch would inherit `_currentConversationId`
        // (the chat the user is viewing), routing the heartbeat's shell commands into
        // that chat's persistent bash session.
        return if (conversationIdOverride != null) {
            withContext(ConversationIdElement(conversationIdOverride)) {
                askWithService(service, messages, systemPrompt, targetInstance.instanceId, localHistory).content
            }
        } else {
            askWithService(service, messages, systemPrompt, targetInstance.instanceId, localHistory).content
        }
    }

    override suspend fun askSilently(question: String): String {
        val service = currentService()
        val firstInstance = getConfiguredServiceInstances().firstOrNull() ?: return ""
        val messages = listOf(History(role = History.Role.USER, content = question))

        val systemPrompt = getActiveSystemPrompt()

        val creds = instanceCredentials(firstInstance.instanceId, service)

        val responseText = when (service) {
            Service.Gemini -> {
                val geminiMessages = messages.map { it.toGeminiMessageDto() }
                val response = requests.geminiChat(creds, geminiMessages, systemInstruction = systemPrompt).getOrThrow()
                response.extractText()
            }

            Service.Anthropic -> {
                val anthropicMessages = buildAnthropicMessages(messages)
                val response = requests.anthropicChat(creds, anthropicMessages, systemInstruction = systemPrompt).getOrThrow()
                response.extractText()
            }

            else -> {
                val openAIMessages = buildOpenAIMessages(service, messages, systemPrompt)
                val response = requests.openAICompatibleChat(service, creds, openAIMessages).getOrThrow()
                response.choices.firstOrNull()?.message?.effectiveContent ?: ""
            }
        }

        return responseText
    }

    override suspend fun askSilentlyWithInstance(instanceId: String, prompt: String, timeoutMs: Long): String {
        val instance = getConfiguredServiceInstances().find { it.instanceId == instanceId }
            ?: return askSilently(prompt)
        val service = Service.fromId(instance.serviceId)
        val messages = listOf(History(role = History.Role.USER, content = prompt))

        val creds = instanceCredentials(instanceId, service)
        val reqTimeout = if (timeoutMs > 0) timeoutMs else null

        return when (service) {
            Service.Gemini -> {
                val geminiMessages = messages.map { it.toGeminiMessageDto() }
                val response = requests.geminiChat(creds, geminiMessages, requestTimeoutMs = reqTimeout).getOrThrow()
                response.extractText()
            }

            Service.Anthropic -> {
                val anthropicMessages = buildAnthropicMessages(messages)
                val response = requests.anthropicChat(creds, anthropicMessages, requestTimeoutMs = reqTimeout).getOrThrow()
                response.extractText()
            }

            else -> {
                val openAIMessages = buildOpenAIMessages(service, messages, null)
                val response = requests.openAICompatibleChat(service, creds, openAIMessages, requestTimeoutMs = reqTimeout).getOrThrow()
                response.choices.firstOrNull()?.message?.effectiveContent ?: ""
            }
        }
    }

    private val _hasUnreadHeartbeat = MutableStateFlow(false)
    override val hasUnreadHeartbeat: StateFlow<Boolean> = _hasUnreadHeartbeat

    override fun clearUnreadHeartbeat() {
        _hasUnreadHeartbeat.value = false
    }

    private val _openHeartbeatRequested = MutableStateFlow(false)
    override val openHeartbeatRequested: StateFlow<Boolean> = _openHeartbeatRequested

    override fun requestOpenHeartbeat() {
        _openHeartbeatRequested.value = true
    }

    override fun consumeOpenHeartbeatRequest() {
        _openHeartbeatRequested.value = false
    }

    private val _openWorkTopicRequested = MutableStateFlow(false)
    override val openWorkTopicRequested: StateFlow<Boolean> = _openWorkTopicRequested

    override fun requestOpenWorkTopic() {
        _openWorkTopicRequested.value = true
    }

    override fun consumeOpenWorkTopicRequest() {
        _openWorkTopicRequested.value = false
    }

    private val _hasUnreadWorkReport = MutableStateFlow(false)
    override val hasUnreadWorkReport: StateFlow<Boolean> = _hasUnreadWorkReport

    override fun clearUnreadWorkReport() {
        _hasUnreadWorkReport.value = false
    }

    // Base behavior: raise the unread badge. The gateway client overrides this to
    // live-refresh the home transcript when the user is already viewing it, and
    // only falls back to this badge when they are looking elsewhere.
    override fun onProactiveReportForeground() {
        _hasUnreadWorkReport.value = true
    }

    override suspend fun addAssistantMessage(content: String) {
        val now = Clock.System.now().toEpochMilliseconds()

        val existing = savedConversations.value.find { it.type == Conversation.TYPE_HEARTBEAT }
        val heartbeatId = existing?.id ?: getOrCreateHeartbeatConversationId()

        val newMessage = Conversation.Message(
            id = Uuid.random().toString(),
            role = "assistant",
            content = content,
        )

        val messages = ((existing?.messages ?: emptyList()) + newMessage).takeLast(MAX_HEARTBEAT_MESSAGES)

        val conversation = Conversation(
            id = heartbeatId,
            messages = messages,
            createdAt = existing?.createdAt ?: now,
            updatedAt = now,
            type = Conversation.TYPE_HEARTBEAT,
        )

        _hasUnreadHeartbeat.value = true
        conversationStorage.saveConversation(conversation)
    }

    override suspend fun getOrCreateHeartbeatConversationId(): String {
        val existing = savedConversations.value.find { it.type == Conversation.TYPE_HEARTBEAT }
        if (existing != null) return existing.id
        val now = Clock.System.now().toEpochMilliseconds()
        val id = Uuid.random().toString()
        conversationStorage.saveConversation(
            Conversation(
                id = id,
                messages = emptyList(),
                createdAt = now,
                updatedAt = now,
                type = Conversation.TYPE_HEARTBEAT,
            ),
        )
        return id
    }

    private fun deriveTitle(history: List<History>): String {
        val firstUserMessage = history.firstOrNull { it.role == History.Role.USER }?.content ?: return ""
        return if (firstUserMessage.length <= 50) {
            firstUserMessage
        } else {
            val truncated = firstUserMessage.take(50)
            val lastSpace = truncated.lastIndexOf(' ')
            if (lastSpace > 20) truncated.substring(0, lastSpace) + "..." else truncated + "..."
        }
    }
}
