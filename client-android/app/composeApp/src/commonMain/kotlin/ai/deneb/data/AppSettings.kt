package ai.deneb.data

import ai.deneb.defaultUiScale
import com.russhwolf.settings.ExperimentalSettingsApi
import com.russhwolf.settings.Settings
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlin.time.Clock
import kotlin.time.ExperimentalTime
import kotlin.uuid.ExperimentalUuidApi
import kotlin.uuid.Uuid

enum class ThemeMode {
    System,
    Light,
    Dark,
    OledBlack,
}

class AppSettings(internal val settings: Settings) {

    // App open tracking
    fun trackAppOpen(): Int {
        val currentCount = settings.getInt(KEY_APP_OPENS, 0)
        val newCount = currentCount + 1
        settings.putInt(KEY_APP_OPENS, newCount)
        return newCount
    }

    // Tool enable/disable settings
    fun isToolEnabled(toolId: String, defaultEnabled: Boolean = true): Boolean = settings.getBoolean("$KEY_TOOL_PREFIX$toolId", defaultEnabled)

    fun setToolEnabled(toolId: String, enabled: Boolean) {
        settings.putBoolean("$KEY_TOOL_PREFIX$toolId", enabled)
    }

    fun getConversationsJson(): String? = settings.getStringOrNull(KEY_CONVERSATIONS)

    fun setConversationsJson(json: String) {
        settings.putString(KEY_CONVERSATIONS, json)
    }

    // Feed "읽음" (seen) state — the work-feed item ids the user has opened in the
    // 피드 screen, persisted as a comma-joined string (ids never contain commas).
    // Distinct from the server-side ack/done that the action buttons perform;
    // "seen" only moves a row into the read section.
    fun getFeedSeenIds(): Set<String> = settings.getStringOrNull(KEY_FEED_SEEN_IDS)
        ?.split(',')
        ?.filterTo(LinkedHashSet()) { it.isNotBlank() }
        ?: emptySet()

    fun markFeedSeen(id: String) {
        if (id.isBlank()) return
        val next = LinkedHashSet(getFeedSeenIds()).apply { add(id) }
        // Bound the set so a long-running install can't grow it without limit.
        val bounded = if (next.size > 500) next.toList().takeLast(500).toSet() else next
        settings.putString(KEY_FEED_SEEN_IDS, bounded.joinToString(","))
    }

    // Hidden 더보기 ("자체앱") tiles — the stable tile keys (destination @SerialName) the user
    // chose to hide in 설정 → "자체앱 표시 항목", persisted as a comma-joined string (keys never
    // contain commas). Stored in the plain settings store like every other UI preference
    // (theme, recall): a visibility preference, unlike the gateway token, is non-critical, so
    // the DurableMirrorSettings whitelist would be overkill — at worst an Android OTA prefs
    // wipe resets tiles to "all shown", a harmless reappearance the user can redo.
    fun getHiddenMoreTiles(): Set<String> = settings.getStringOrNull(KEY_HIDDEN_MORE_TILES)
        ?.split(',')
        ?.filterTo(LinkedHashSet()) { it.isNotBlank() }
        ?: emptySet()

    fun setMoreTileHidden(key: String, hidden: Boolean) {
        if (key.isBlank()) return
        val next = LinkedHashSet(getHiddenMoreTiles())
        if (hidden) next.add(key) else next.remove(key)
        settings.putString(KEY_HIDDEN_MORE_TILES, next.joinToString(","))
    }

    fun getCurrentConversationId(): String? = settings.getStringOrNull(KEY_CURRENT_CONVERSATION_ID)

    fun setCurrentConversationId(id: String?) {
        if (id == null) {
            settings.remove(KEY_CURRENT_CONVERSATION_ID)
        } else {
            settings.putString(KEY_CURRENT_CONVERSATION_ID, id)
        }
    }

    fun isCurrentConversationMigrated(): Boolean = settings.getBoolean(KEY_CURRENT_CONVERSATION_MIGRATED, false)

    fun markCurrentConversationMigrated() {
        settings.putBoolean(KEY_CURRENT_CONVERSATION_MIGRATED, true)
    }

    fun getEncryptionKey(): ByteArray? {
        val encoded = settings.getStringOrNull(KEY_ENCRYPTION_KEY) ?: return null
        return try {
            @OptIn(kotlin.io.encoding.ExperimentalEncodingApi::class)
            kotlin.io.encoding.Base64.decode(encoded)
        } catch (_: Exception) {
            null
        }
    }

    // Geofences (집/직장) — stored as a JSON string; the sensing layer encodes/decodes
    // (encodeGeofences/decodeGeofences) so AppSettings stays decoupled from the model.
    fun getGeofencesJson(): String = settings.getString(KEY_GEOFENCES, "[]")

    fun setGeofencesJson(json: String) {
        settings.putString(KEY_GEOFENCES, json)
    }

    // Soul (system prompt)
    fun getSoulText(): String = settings.getString(KEY_SOUL, "")

    fun setSoulText(text: String) {
        settings.putString(KEY_SOUL, text)
    }

    // Memory
    fun isMemoryEnabled(): Boolean = settings.getBoolean(KEY_MEMORY_ENABLED, true)

    fun setMemoryEnabled(enabled: Boolean) {
        settings.putBoolean(KEY_MEMORY_ENABLED, enabled)
    }

    // Recall: the gateway's long-term-memory recall (hindsight/wiki/diary). The
    // "focused chat / memory off" top-bar toggle. On (default) = full recall;
    // off = the gateway skips recall AND retain for the turn. Persona unchanged.
    fun isRecallEnabled(): Boolean = settings.getBoolean(KEY_RECALL_ENABLED, true)

    fun setRecallEnabled(enabled: Boolean) {
        settings.putBoolean(KEY_RECALL_ENABLED, enabled)
    }

    // Per-workspace active session. 업무(work) and 챗봇(chat) keep SEPARATE session
    // lists, so each remembers its own last-open conversation across restarts and
    // pill switches. 업무 defaults to its persistent home (client:main) where
    // proactive reports land; 챗봇 has no home — each chat is an independent
    // chat:<uuid> — so an unset last session returns "" and the caller mints a
    // fresh one (DenebGatewayClient.newSessionKey).
    fun lastSession(work: Boolean): String = settings.getString(if (work) KEY_WORK_SESSION else KEY_CHAT_SESSION, if (work) "client:main" else "")

    fun setLastSession(work: Boolean, key: String) {
        settings.putString(if (work) KEY_WORK_SESSION else KEY_CHAT_SESSION, key)
    }

    fun getMemoryInstructions(): String = settings.getString(KEY_MEMORY_INSTRUCTIONS, DEFAULT_MEMORY_INSTRUCTIONS)

    // Agent memories
    fun getMemoriesJson(): String = settings.getString(KEY_AGENT_MEMORIES, "[]")

    fun setMemoriesJson(json: String) {
        settings.putString(KEY_AGENT_MEMORIES, json)
    }

    // Scheduling
    fun isSchedulingEnabled(): Boolean = settings.getBoolean(KEY_SCHEDULING_ENABLED, true)

    fun setSchedulingEnabled(enabled: Boolean) {
        settings.putBoolean(KEY_SCHEDULING_ENABLED, enabled)
    }

    // Dynamic UI
    fun isDynamicUiEnabled(): Boolean = settings.getBoolean(KEY_DYNAMIC_UI_ENABLED, true)

    fun setDynamicUiEnabled(enabled: Boolean) {
        settings.putBoolean(KEY_DYNAMIC_UI_ENABLED, enabled)
    }

    private val _themeModeFlow = MutableStateFlow(loadInitialThemeMode())
    val themeModeFlow: StateFlow<ThemeMode> = _themeModeFlow

    fun getThemeMode(): ThemeMode = _themeModeFlow.value

    fun setThemeMode(mode: ThemeMode) {
        settings.putString(KEY_THEME_MODE, mode.name)
        _themeModeFlow.value = mode
    }

    private fun loadInitialThemeMode(): ThemeMode {
        val raw = settings.getString(KEY_THEME_MODE, "")
        if (raw.isNotEmpty()) {
            return try {
                ThemeMode.valueOf(raw)
            } catch (_: IllegalArgumentException) {
                ThemeMode.System
            }
        }
        // Migrate the legacy boolean OLED toggle: true → OledBlack, false → System.
        return if (settings.getBoolean(KEY_OLED_MODE_ENABLED, false)) ThemeMode.OledBlack else ThemeMode.System
    }

    // Daemon mode
    fun isDaemonEnabled(): Boolean = settings.getBoolean(KEY_DAEMON_ENABLED, false)

    fun setDaemonEnabled(enabled: Boolean) {
        settings.putBoolean(KEY_DAEMON_ENABLED, enabled)
    }

    // Linux Sandbox
    fun isSandboxEnabled(): Boolean = settings.getBoolean(KEY_SANDBOX_ENABLED, true)

    fun setSandboxEnabled(enabled: Boolean) {
        settings.putBoolean(KEY_SANDBOX_ENABLED, enabled)
    }

    fun getScheduledTasksJson(): String = settings.getString(KEY_SCHEDULED_TASKS, "[]")

    fun setScheduledTasksJson(json: String) {
        settings.putString(KEY_SCHEDULED_TASKS, json)
    }

    // Heartbeat config
    fun getHeartbeatConfigJson(): String = settings.getString(KEY_HEARTBEAT_CONFIG, "")

    fun setHeartbeatConfigJson(json: String) {
        settings.putString(KEY_HEARTBEAT_CONFIG, json)
    }

    // Heartbeat log
    fun getHeartbeatLogJson(): String = settings.getString(KEY_HEARTBEAT_LOG, "")

    fun setHeartbeatLogJson(json: String) {
        settings.putString(KEY_HEARTBEAT_LOG, json)
    }

    // Heartbeat prompt
    fun getHeartbeatPrompt(): String = settings.getString(KEY_HEARTBEAT_PROMPT, "")

    fun setHeartbeatPrompt(text: String) {
        settings.putString(KEY_HEARTBEAT_PROMPT, text)
    }

    // MCP Servers
    fun getMcpServersJson(): String = settings.getString(KEY_MCP_SERVERS, "")

    fun setMcpServersJson(json: String) {
        settings.putString(KEY_MCP_SERVERS, json)
    }

    // UI Scale
    private val _uiScaleFlow = MutableStateFlow(settings.getFloat(KEY_UI_SCALE, defaultUiScale))
    val uiScaleFlow: StateFlow<Float> = _uiScaleFlow

    fun getUiScale(): Float = _uiScaleFlow.value

    fun setUiScale(scale: Float) {
        settings.putFloat(KEY_UI_SCALE, scale)
        _uiScaleFlow.value = scale
    }

    // Email
    fun isEmailEnabled(): Boolean = settings.getBoolean(KEY_EMAIL_ENABLED, true)

    fun setEmailEnabled(enabled: Boolean) {
        settings.putBoolean(KEY_EMAIL_ENABLED, enabled)
    }

    fun getEmailAccountsJson(): String = settings.getString(KEY_EMAIL_ACCOUNTS, "")

    fun setEmailAccountsJson(json: String) {
        settings.putString(KEY_EMAIL_ACCOUNTS, json)
    }

    fun getEmailPassword(accountId: String): String = settings.getString("${KEY_EMAIL_PASSWORD_PREFIX}$accountId", "")

    fun setEmailPassword(accountId: String, password: String) {
        settings.putString("${KEY_EMAIL_PASSWORD_PREFIX}$accountId", password)
    }

    fun removeEmailPassword(accountId: String) {
        settings.remove("${KEY_EMAIL_PASSWORD_PREFIX}$accountId")
    }

    fun getEmailSyncStateJson(accountId: String): String = settings.getString("${KEY_EMAIL_SYNC_PREFIX}$accountId", "")

    fun setEmailSyncStateJson(accountId: String, json: String) {
        settings.putString("${KEY_EMAIL_SYNC_PREFIX}$accountId", json)
    }

    fun getEmailPollIntervalMinutes(): Int = settings.getInt(KEY_EMAIL_POLL_INTERVAL, 15)

    fun setEmailPollIntervalMinutes(minutes: Int) {
        settings.putInt(KEY_EMAIL_POLL_INTERVAL, minutes)
    }

    fun getEmailPendingJson(): String = settings.getString(KEY_EMAIL_PENDING, "")

    fun setEmailPendingJson(json: String) {
        settings.putString(KEY_EMAIL_PENDING, json)
    }

    // SMS (FOSS-only, Android-only — settings layer is platform-agnostic, feature gate
    // is enforced by the READ_SMS permission being declared only in foss/AndroidManifest.xml)
    fun isSmsEnabled(): Boolean = settings.getBoolean(KEY_SMS_ENABLED, false)

    fun setSmsEnabled(enabled: Boolean) {
        settings.putBoolean(KEY_SMS_ENABLED, enabled)
    }

    fun getSmsPollIntervalMinutes(): Int = settings.getInt(KEY_SMS_POLL_INTERVAL, 15)

    fun setSmsPollIntervalMinutes(minutes: Int) {
        settings.putInt(KEY_SMS_POLL_INTERVAL, minutes)
    }

    fun getSmsPendingJson(): String = settings.getString(KEY_SMS_PENDING, "")

    fun setSmsPendingJson(json: String) {
        settings.putString(KEY_SMS_PENDING, json)
    }

    fun getSmsSyncStateJson(): String = settings.getString(KEY_SMS_SYNC_STATE, "")

    fun setSmsSyncStateJson(json: String) {
        settings.putString(KEY_SMS_SYNC_STATE, json)
    }

    fun isSmsSendEnabled(): Boolean = settings.getBoolean(KEY_SMS_SEND_ENABLED, false)

    fun setSmsSendEnabled(enabled: Boolean) {
        settings.putBoolean(KEY_SMS_SEND_ENABLED, enabled)
    }

    fun getSmsDraftsJson(): String = settings.getString(KEY_SMS_DRAFTS, "")

    fun setSmsDraftsJson(json: String) {
        settings.putString(KEY_SMS_DRAFTS, json)
    }

    // Local model context size
    fun getModelContextTokens(modelId: String): Int = settings.getInt("$KEY_MODEL_CONTEXT_PREFIX$modelId", 0)

    fun setModelContextTokens(modelId: String, contextTokens: Int) {
        settings.putInt("$KEY_MODEL_CONTEXT_PREFIX$modelId", contextTokens)
    }

    // --- Transcript cache (cache-then-network) -----------------------------
    // Per-session chat transcript JSON, persisted in the encrypted settings store
    // so a reopened session renders instantly while the network fetch revalidates.
    // Bounded by a small LRU so it never grows without limit (transcripts are
    // private work content — kept encrypted, capped, and evicted, not archived).

    fun getCachedTranscript(sessionKey: String): String? = settings.getStringOrNull(KEY_TX_CACHE_PREFIX + sessionKey)

    fun putCachedTranscript(sessionKey: String, json: String) {
        settings.putString(KEY_TX_CACHE_PREFIX + sessionKey, json)
        // Most-recent-first LRU; evict overflow keys' payloads so the store is bounded.
        val next = (listOf(sessionKey) + txCacheLru().filterNot { it == sessionKey }).take(TX_CACHE_MAX_SESSIONS)
        txCacheLru().filterNot { it in next }.forEach { settings.remove(KEY_TX_CACHE_PREFIX + it) }
        settings.putString(KEY_TX_CACHE_LRU, next.joinToString("\n"))
    }

    fun removeCachedTranscript(sessionKey: String) {
        settings.remove(KEY_TX_CACHE_PREFIX + sessionKey)
        settings.putString(KEY_TX_CACHE_LRU, txCacheLru().filterNot { it == sessionKey }.joinToString("\n"))
    }

    // sessionKeys never contain a newline, so "\n" is a safe list separator.
    private fun txCacheLru(): List<String> = settings.getStringOrNull(KEY_TX_CACHE_LRU)?.split("\n")?.filter { it.isNotBlank() } ?: emptyList()

    // Default inbox mail-list cache (single key — only the no-query inbox view is
    // cached, for instant mail-tab render). Encrypted at rest like the transcript cache.
    fun getCachedMailList(): String? = settings.getStringOrNull(KEY_MAIL_CACHE)

    fun putCachedMailList(json: String) {
        settings.putString(KEY_MAIL_CACHE, json)
    }

    // Work-feed (업무 home) cache (single key — the recent feed, for an instant feed
    // render and, crucially, an offline-first launcher home: the feed shows the
    // last-known briefing when the gateway is unreachable. Owner-fingerprinted like
    // the mail cache so a prior account's feed can't render under new credentials.
    fun getCachedWorkFeed(): String? = settings.getStringOrNull(KEY_WORK_FEED_CACHE)

    fun putCachedWorkFeed(json: String) {
        settings.putString(KEY_WORK_FEED_CACHE, json)
    }

    // Upcoming-calendar cache (single key — the now-anchored look-ahead list, for an
    // instant calendar render and an offline next-meeting glance on the launcher home).
    // Owner-fingerprinted like the mail/feed caches.
    fun getCachedCalendar(): String? = settings.getStringOrNull(KEY_CALENDAR_CACHE)

    fun putCachedCalendar(json: String) {
        settings.putString(KEY_CALENDAR_CACHE, json)
    }

    /**
     * Purge ALL cached private content (every transcript + the inbox list). Called
     * when the gateway URL or client token changes: those cache keys are global, so
     * without this the prior gateway/account's chat and mail would render under the
     * new credentials on the next cold start (before any authenticated RPC).
     *
     * Deletes by key PREFIX rather than walking the LRU index, because the payload
     * and the LRU list are separate settings entries: a crash between
     * putCachedTranscript and its LRU update can orphan a `tx_cache:<session>` key
     * that the LRU never lists, and a stable session key (client:main) could then
     * still load it after a credential switch. Prefix deletion catches those orphans.
     */
    @OptIn(ExperimentalSettingsApi::class)
    fun clearCachedContent() {
        settings.keys
            .filter { it.startsWith(KEY_TX_CACHE_PREFIX) || it == KEY_TX_CACHE_LRU || it == KEY_MAIL_CACHE || it == KEY_WORK_FEED_CACHE || it == KEY_CALENDAR_CACHE }
            .forEach { settings.remove(it) }
    }

    companion object {
        const val KEY_APP_OPENS = "app_opens"

        const val KEY_FEED_SEEN_IDS = "feed_seen_ids"
        const val KEY_HIDDEN_MORE_TILES = "hidden_more_tiles"
        const val KEY_CONVERSATIONS = "conversations_json"
        const val KEY_CURRENT_CONVERSATION_ID = "current_conversation_id"
        const val KEY_CURRENT_CONVERSATION_MIGRATED = "current_conversation_migrated"
        const val KEY_ENCRYPTION_KEY = "encryption_key"
        const val KEY_MIGRATION_COMPLETE = "migration_complete_v1"
        const val KEY_TOOL_PREFIX = "tool_enabled_"
        const val KEY_SOUL = "soul_text"
        const val KEY_MEMORY_ENABLED = "memory_enabled"
        const val KEY_RECALL_ENABLED = "recall_enabled"
        const val KEY_WORK_SESSION = "workspace_session_work"
        const val KEY_CHAT_SESSION = "workspace_session_chat"
        const val KEY_MEMORY_INSTRUCTIONS = "memory_instructions"
        const val KEY_AGENT_MEMORIES = "agent_memories"
        const val KEY_SCHEDULED_TASKS = "scheduled_tasks"
        const val KEY_GEOFENCES = "geofences"
        const val KEY_SCHEDULING_ENABLED = "scheduling_enabled"
        const val KEY_DYNAMIC_UI_ENABLED = "dynamic_ui_enabled"
        const val KEY_OLED_MODE_ENABLED = "oled_mode_enabled"
        const val KEY_THEME_MODE = "theme_mode"
        const val KEY_DAEMON_ENABLED = "daemon_enabled"
        const val KEY_HEARTBEAT_CONFIG = "heartbeat_config"
        const val KEY_HEARTBEAT_PROMPT = "heartbeat_prompt"
        const val KEY_HEARTBEAT_LOG = "heartbeat_log"

        const val KEY_EMAIL_ENABLED = "email_enabled"
        const val KEY_EMAIL_ACCOUNTS = "email_accounts"
        const val KEY_EMAIL_PASSWORD_PREFIX = "email_password_"
        const val KEY_EMAIL_SYNC_PREFIX = "email_sync_"
        const val KEY_EMAIL_POLL_INTERVAL = "email_poll_interval"
        const val KEY_EMAIL_PENDING = "email_pending"

        const val KEY_SMS_ENABLED = "sms_enabled"
        const val KEY_SMS_POLL_INTERVAL = "sms_poll_interval"
        const val KEY_SMS_PENDING = "sms_pending"
        const val KEY_SMS_SYNC_STATE = "sms_sync_state"
        const val KEY_SMS_SEND_ENABLED = "sms_send_enabled"
        const val KEY_SMS_DRAFTS = "sms_drafts"

        const val KEY_UI_SCALE = "ui_scale"
        const val KEY_MCP_SERVERS = "mcp_servers"

        const val KEY_MODEL_CONTEXT_PREFIX = "model_context_"

        const val KEY_SANDBOX_ENABLED = "sandbox_enabled"

        const val KEY_MAIL_CACHE = "mail_list_cache"
        const val KEY_WORK_FEED_CACHE = "work_feed_cache"
        const val KEY_CALENDAR_CACHE = "calendar_cache"
        const val KEY_TX_CACHE_PREFIX = "tx_cache:"
        const val KEY_TX_CACHE_LRU = "tx_cache_lru"
        const val TX_CACHE_MAX_SESSIONS = 12

        // Basic memory guidance shared by every chat variant. The advanced `## Structured
        // Learning` block lives in `ChatSystemPromptBuilder.DEFAULT_STRUCTURED_LEARNING_SECTION`
        // and is composed in only for the remote variant.
        const val DEFAULT_MEMORY_INSTRUCTIONS =
            "You have persistent memory across conversations. " +
                "All your stored memories are listed in the system prompt grouped by category.\n\n" +
                "When you learn important information about the user (name, preferences, projects, goals, etc.), " +
                "proactively use the memory_store tool to save it.\n" +
                "Use the memory_forget tool to remove outdated or incorrect memories.\n" +
                "Do not store trivial or transient information."
    }
}
