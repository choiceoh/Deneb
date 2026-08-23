package ai.deneb.data

import ai.deneb.defaultUiScale
import com.russhwolf.settings.ExperimentalSettingsApi
import com.russhwolf.settings.Settings
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.serialization.Serializable
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString

enum class ThemeMode {
    System,
    Light,
    Dark,
    OledBlack,
}

@Serializable
private data class ComposerDraftEntry(
    val id: String,
    val text: String,
)

@Serializable
private data class ComposerDraftStore(
    val entries: List<ComposerDraftEntry> = emptyList(),
)

@Serializable
private data class TranscriptCacheEntry(
    val sessionKey: String,
    val payloadKey: String,
)

@Serializable
private data class TranscriptCacheManifest(
    val revision: Long = 0,
    val entries: List<TranscriptCacheEntry> = emptyList(),
)

private data class TranscriptManifestState(
    val present: Boolean,
    val manifest: TranscriptCacheManifest,
)

private fun nextTranscriptRevision(current: Long): Long = if (current == Long.MAX_VALUE) 1 else current + 1

class AppSettings(internal val settings: Settings) {
    private val transcriptCacheLock = SynchronousLock()

    // App open tracking
    fun trackAppOpen(): Int {
        val currentCount = settings.getInt(KEY_APP_OPENS, 0)
        val newCount = if (currentCount == Int.MAX_VALUE) Int.MAX_VALUE else currentCount + 1
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
    fun getHiddenMoreTiles(): Set<String> {
        // null = never customized → product defaults (console tiles stay in 설정).
        // A persisted empty string means the user turned every default back on.
        val raw = settings.getStringOrNull(KEY_HIDDEN_MORE_TILES) ?: return DEFAULT_HIDDEN_MORE_TILES
        return raw.split(',').filterTo(LinkedHashSet()) { it.isNotBlank() }
    }

    fun getComposerDraft(sessionId: String): String {
        if (sessionId.isBlank()) return ""
        loadComposerDrafts()[sessionId]?.let { return it }
        val legacy = settings.getStringOrNull(KEY_COMPOSER_DRAFT).orEmpty()
        if (legacy.isEmpty()) return ""
        setComposerDraft(sessionId, legacy)
        settings.remove(KEY_COMPOSER_DRAFT)
        return legacy.take(COMPOSER_DRAFT_MAX)
    }

    fun setComposerDraft(sessionId: String, text: String) {
        if (sessionId.isBlank()) return
        val drafts = loadComposerDrafts()
        val trimmed = text.take(COMPOSER_DRAFT_MAX)
        if (trimmed.isEmpty()) {
            drafts.remove(sessionId)
        } else {
            drafts.remove(sessionId)
            drafts[sessionId] = trimmed
            while (drafts.size > COMPOSER_DRAFT_MAX_SESSIONS) {
                drafts.remove(drafts.keys.first())
            }
        }
        persistComposerDrafts(drafts)
    }

    private fun loadComposerDrafts(): LinkedHashMap<String, String> {
        val raw = settings.getStringOrNull(KEY_COMPOSER_DRAFTS) ?: return LinkedHashMap()
        val store = runCatching { SharedJson.decodeFromString<ComposerDraftStore>(raw) }.getOrNull()
            ?: return LinkedHashMap()
        val drafts = LinkedHashMap<String, String>()
        for (entry in store.entries) {
            if (entry.id.isNotBlank() && entry.text.isNotEmpty()) {
                drafts[entry.id] = entry.text.take(COMPOSER_DRAFT_MAX)
            }
        }
        return drafts
    }

    private fun persistComposerDrafts(drafts: Map<String, String>) {
        if (drafts.isEmpty()) {
            settings.remove(KEY_COMPOSER_DRAFTS)
            return
        }
        val store = ComposerDraftStore(drafts.map { ComposerDraftEntry(it.key, it.value) })
        settings.putString(KEY_COMPOSER_DRAFTS, SharedJson.encodeToString(store))
    }

    // Drop keys that no longer exist in the 더보기 catalog (retired tiles). Does not
    // invent an allow-list of its own — the caller passes the live tile keys so
    // tests can still persist fake keys through get/set without a prune.
    fun pruneHiddenMoreTiles(knownKeys: Set<String>) {
        if (knownKeys.isEmpty()) return
        val raw = settings.getStringOrNull(KEY_HIDDEN_MORE_TILES) ?: return
        val stored = raw.split(',').filterTo(LinkedHashSet()) { it.isNotBlank() }
        val pruned = stored.filterTo(LinkedHashSet()) { it in knownKeys }
        if (pruned != stored) {
            settings.putString(KEY_HIDDEN_MORE_TILES, pruned.joinToString(","))
        }
    }

    fun setMoreTileHidden(key: String, hidden: Boolean) {
        if (key.isBlank()) return
        val next = LinkedHashSet(getHiddenMoreTiles())
        if (hidden) next.add(key) else next.remove(key)
        settings.putString(KEY_HIDDEN_MORE_TILES, next.joinToString(","))
    }

    // In-app browser bookmarks. Stored as JSON because each row carries URL + display title.
    fun getBrowserBookmarksJson(): String = settings.getString(KEY_BROWSER_BOOKMARKS, "[]")

    fun setBrowserBookmarksJson(json: String) {
        settings.putString(KEY_BROWSER_BOOKMARKS, json)
    }

    // Bounded in-app browser tab metadata. Platform WebView history/scroll stays
    // process-local; this durable store restores the active tab and tab URLs.
    fun getBrowserTabsJson(): String = settings.getString(KEY_BROWSER_TABS, "{}")

    fun setBrowserTabsJson(json: String) {
        settings.putString(KEY_BROWSER_TABS, json)
    }

    // Last page shown in the in-app translation browser. Restored when reopening
    // from More with an empty route URL (leave → re-enter resumes where you left).
    fun getBrowserLastUrl(): String = settings.getString(KEY_BROWSER_LAST_URL, "")

    fun setBrowserLastUrl(url: String) {
        val trimmed = url.trim()
        if (trimmed.isEmpty()) {
            settings.remove(KEY_BROWSER_LAST_URL)
        } else {
            settings.putString(KEY_BROWSER_LAST_URL, trimmed)
        }
    }

    // In-place DeepL toggle preference — remembered across browser opens.

    /**
     * Whether the notification permission prompt has been shown once.
     *
     * Android only allows one prompt: a second request after a denial is a silent
     * no-op, so re-asking on every launch would neither work nor be polite. The
     * flag is about having ASKED, not about the answer — the current grant state
     * is always read from the platform.
     */
    fun notificationPermissionAsked(): Boolean = settings.getBoolean(KEY_NOTIFICATION_PERMISSION_ASKED, false)

    fun setNotificationPermissionAsked(asked: Boolean) {
        settings.putBoolean(KEY_NOTIFICATION_PERMISSION_ASKED, asked)
    }

    fun isBrowserTranslateEnabled(): Boolean = settings.getBoolean(KEY_BROWSER_TRANSLATE_ENABLED, false)

    fun setBrowserTranslateEnabled(enabled: Boolean) {
        settings.putBoolean(KEY_BROWSER_TRANSLATE_ENABLED, enabled)
    }

    // Recent visits (newest first). JSON list of {url, title, visitedAtMs}.
    fun getBrowserHistoryJson(): String = settings.getString(KEY_BROWSER_HISTORY, "[]")

    fun setBrowserHistoryJson(json: String) {
        settings.putString(KEY_BROWSER_HISTORY, json)
    }

    // User-chosen start home for the in-app browser. Used when there is no nav
    // URL and no resumable last page (nav → last → home → blank).
    fun getBrowserHomeUrl(): String = settings.getString(KEY_BROWSER_HOME_URL, "")

    fun setBrowserHomeUrl(url: String) {
        val trimmed = url.trim()
        if (trimmed.isEmpty()) {
            settings.remove(KEY_BROWSER_HOME_URL)
        } else {
            settings.putString(KEY_BROWSER_HOME_URL, trimmed)
        }
    }

    // Network ad/tracker blocking for the in-app translation browser (default ON).
    fun isBrowserAdBlockEnabled(): Boolean = settings.getBoolean(KEY_BROWSER_ADBLOCK_ENABLED, true)

    fun setBrowserAdBlockEnabled(enabled: Boolean) {
        settings.putBoolean(KEY_BROWSER_ADBLOCK_ENABLED, enabled)
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

    // Active session, remembered across restarts so the app reopens the
    // conversation the user left. Defaults to the persistent home (client:main)
    // where proactive reports land.
    fun lastSession(): String = settings.getString(KEY_WORK_SESSION, "client:main")

    fun setLastSession(key: String) {
        settings.putString(KEY_WORK_SESSION, key)
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

    fun getCachedTranscript(sessionKey: String): String? = transcriptCacheLock.withLock {
        val state = readTranscriptManifestLocked()
        if (!state.present) return@withLock getLegacyTranscriptLocked(sessionKey)

        val entry = state.manifest.entries.firstOrNull { it.sessionKey == sessionKey }
            ?: return@withLock null
        val payload = settings.getStringOrNull(entry.payloadKey)
        if (payload == null) repairMissingTranscriptPayloadsLocked(state.manifest)
        payload
    }

    /**
     * Copy-on-write transcript update. A fresh payload blob is fully prepared
     * before the single manifest write publishes it; cleanup happens only after
     * that commit, so a write failure leaves the prior generation readable.
     */
    fun putCachedTranscript(sessionKey: String, json: String) = transcriptCacheLock.withLock {
        val state = readTranscriptManifestLocked()
        if (state.present) {
            putTransactionalTranscriptLocked(state.manifest, sessionKey, json)
        } else {
            migrateLegacyTranscriptsLocked(sessionKey to json)
        }
    }

    fun removeCachedTranscript(sessionKey: String) = transcriptCacheLock.withLock {
        val state = readTranscriptManifestLocked()
        if (state.present) {
            val next = state.manifest.copy(
                entries = state.manifest.entries.filterNot { it.sessionKey == sessionKey },
            )
            writeTranscriptManifestLocked(next)
            cleanupTranscriptStorageLocked(next, removeLegacy = true)
        } else {
            migrateLegacyTranscriptsLocked(extra = null, removedSessionKey = sessionKey)
        }
    }

    private fun readTranscriptManifestLocked(): TranscriptManifestState {
        val raw = settings.getStringOrNull(KEY_TX_CACHE_MANIFEST)
            ?: return TranscriptManifestState(false, TranscriptCacheManifest())
        val decoded = runCatching { SharedJson.decodeFromString<TranscriptCacheManifest>(raw) }.getOrNull()
        if (decoded != null && decoded.isValidTranscriptManifest()) {
            return TranscriptManifestState(true, decoded)
        }

        // A malformed committed manifest must fail closed. Replacing it with an
        // empty manifest before cleanup prevents stale legacy payloads from being
        // resurrected if a previous post-commit cleanup was interrupted.
        val empty = TranscriptCacheManifest()
        if (runCatching { writeTranscriptManifestLocked(empty) }.isSuccess) {
            cleanupTranscriptStorageLocked(empty, removeLegacy = true)
        }
        return TranscriptManifestState(true, empty)
    }

    private fun TranscriptCacheManifest.isValidTranscriptManifest(): Boolean = revision >= 0 &&
        entries.size <= TX_CACHE_MAX_SESSIONS &&
        entries.map { it.sessionKey }.distinct().size == entries.size &&
        entries.map { it.payloadKey }.distinct().size == entries.size &&
        entries.all { entry ->
            val suffix = entry.payloadKey.removePrefix(KEY_TX_CACHE_BLOB_PREFIX)
            entry.payloadKey.startsWith(KEY_TX_CACHE_BLOB_PREFIX) &&
                suffix.toLongOrNull()?.let { it > 0 } == true
        }

    private fun getLegacyTranscriptLocked(sessionKey: String): String? {
        val lru = legacyTranscriptKeysLocked()
        if (sessionKey !in lru) {
            runCatching { settings.remove(KEY_TX_CACHE_PREFIX + sessionKey) }
            return null
        }
        val payload = settings.getStringOrNull(KEY_TX_CACHE_PREFIX + sessionKey)
        if (payload == null) runCatching { writeLegacyTranscriptLruLocked(lru - sessionKey) }
        return payload
    }

    private fun putTransactionalTranscriptLocked(
        current: TranscriptCacheManifest,
        sessionKey: String,
        json: String,
    ) {
        val retained = current.entries
            .filterNot { it.sessionKey == sessionKey }
            .filter { settings.getStringOrNull(it.payloadKey) != null }
            .take(TX_CACHE_MAX_SESSIONS - 1)
        val (revision, payloadKey) = nextTranscriptPayloadKey(current.revision, retained)
        settings.putString(payloadKey, json)
        val next = TranscriptCacheManifest(
            revision = revision,
            entries = listOf(TranscriptCacheEntry(sessionKey, payloadKey)) + retained,
        )
        try {
            writeTranscriptManifestLocked(next)
        } catch (failure: Throwable) {
            runCatching { settings.remove(payloadKey) }
            throw failure
        }
        cleanupTranscriptStorageLocked(next, removeLegacy = true)
    }

    private fun migrateLegacyTranscriptsLocked(
        extra: Pair<String, String>?,
        removedSessionKey: String? = null,
    ) {
        val desired = buildList {
            if (extra != null) add(extra)
            legacyTranscriptKeysLocked()
                .asSequence()
                .filterNot { it == extra?.first || it == removedSessionKey }
                .mapNotNull { key -> settings.getStringOrNull(KEY_TX_CACHE_PREFIX + key)?.let { key to it } }
                .forEach(::add)
        }.take(TX_CACHE_MAX_SESSIONS)

        var revision = 0L
        val preparedKeys = mutableListOf<String>()
        val entries = mutableListOf<TranscriptCacheEntry>()
        try {
            desired.forEach { (sessionKey, json) ->
                revision = nextTranscriptRevision(revision)
                val payloadKey = KEY_TX_CACHE_BLOB_PREFIX + revision
                settings.putString(payloadKey, json)
                preparedKeys += payloadKey
                entries += TranscriptCacheEntry(sessionKey, payloadKey)
            }
            val manifest = TranscriptCacheManifest(revision, entries)
            writeTranscriptManifestLocked(manifest)
            cleanupTranscriptStorageLocked(manifest, removeLegacy = true)
        } catch (failure: Throwable) {
            preparedKeys.forEach { runCatching { settings.remove(it) } }
            throw failure
        }
    }

    private fun nextTranscriptPayloadKey(
        currentRevision: Long,
        retained: List<TranscriptCacheEntry>,
    ): Pair<Long, String> {
        val retainedKeys = retained.mapTo(mutableSetOf()) { it.payloadKey }
        var revision = currentRevision
        repeat(TX_CACHE_MAX_SESSIONS + 1) {
            revision = nextTranscriptRevision(revision)
            val key = KEY_TX_CACHE_BLOB_PREFIX + revision
            if (key !in retainedKeys) return revision to key
        }
        error("Unable to allocate transcript cache generation")
    }

    private fun repairMissingTranscriptPayloadsLocked(manifest: TranscriptCacheManifest) {
        val repaired = manifest.copy(
            entries = manifest.entries.filter { settings.getStringOrNull(it.payloadKey) != null },
        )
        if (repaired.entries == manifest.entries) return
        if (runCatching { writeTranscriptManifestLocked(repaired) }.isSuccess) {
            cleanupTranscriptStorageLocked(repaired, removeLegacy = true)
        }
    }

    private fun legacyTranscriptKeysLocked(): List<String> = settings.getStringOrNull(KEY_TX_CACHE_LRU)
        ?.split("\n")
        ?.filter { it.isNotBlank() }
        ?.distinct()
        ?.take(TX_CACHE_MAX_SESSIONS)
        ?: emptyList()

    private fun writeLegacyTranscriptLruLocked(keys: List<String>) {
        if (keys.isEmpty()) settings.remove(KEY_TX_CACHE_LRU) else settings.putString(KEY_TX_CACHE_LRU, keys.joinToString("\n"))
    }

    private fun writeTranscriptManifestLocked(manifest: TranscriptCacheManifest) {
        settings.putString(KEY_TX_CACHE_MANIFEST, SharedJson.encodeToString(manifest))
    }

    @OptIn(ExperimentalSettingsApi::class)
    private fun cleanupTranscriptStorageLocked(
        manifest: TranscriptCacheManifest,
        removeLegacy: Boolean,
    ) {
        val retained = manifest.entries.mapTo(mutableSetOf()) { it.payloadKey }
        settings.keys
            .filter { key ->
                (key.startsWith(KEY_TX_CACHE_BLOB_PREFIX) && key !in retained) ||
                    (removeLegacy && (key.startsWith(KEY_TX_CACHE_PREFIX) || key == KEY_TX_CACHE_LRU))
            }
            .forEach { runCatching { settings.remove(it) } }
    }

    // Default inbox mail-list cache (single key — only the no-query inbox view is
    // cached, for instant mail-tab render). Encrypted at rest like the transcript cache.
    fun getCachedMailList(): String? = settings.getStringOrNull(KEY_MAIL_CACHE)

    fun putCachedMailList(json: String) {
        settings.putString(KEY_MAIL_CACHE, json)
    }

    fun removeCachedMailList() {
        settings.remove(KEY_MAIL_CACHE)
    }

    // Work-feed (업무 home) cache (single key — the recent feed, for an instant feed
    // render and, crucially, an offline-first launcher home: the feed shows the
    // last-known briefing when the gateway is unreachable. Owner-fingerprinted like
    // the mail cache so a prior account's feed can't render under new credentials.
    fun getCachedWorkFeed(): String? = settings.getStringOrNull(KEY_WORK_FEED_CACHE)

    fun putCachedWorkFeed(json: String) {
        settings.putString(KEY_WORK_FEED_CACHE, json)
    }

    fun removeCachedWorkFeed() {
        settings.remove(KEY_WORK_FEED_CACHE)
    }

    // Upcoming-calendar cache (single key — the now-anchored look-ahead list, for an
    // instant calendar render and an offline next-meeting glance on the launcher home).
    // Owner-fingerprinted like the mail/feed caches.
    fun getCachedCalendar(): String? = settings.getStringOrNull(KEY_CALENDAR_CACHE)

    fun putCachedCalendar(json: String) {
        settings.putString(KEY_CALENDAR_CACHE, json)
    }

    fun removeCachedCalendar() {
        settings.remove(KEY_CALENDAR_CACHE)
    }

    // Default approvals-list cache (single key — folder=total first page, for an
    // instant 결재 render after process death). Owner-fingerprinted like mail/feed.
    fun getCachedApprovalsList(): String? = settings.getStringOrNull(KEY_APPROVALS_CACHE)

    fun putCachedApprovalsList(json: String) {
        settings.putString(KEY_APPROVALS_CACHE, json)
    }

    fun removeCachedApprovalsList() {
        settings.remove(KEY_APPROVALS_CACHE)
    }

    // Section snapshot cache (one key per browse surface — 카테고리·사람·연락처·일기·
    // 노트북·현황·조직도·위키 목록·달력 월그리드), the disk backing behind the client's
    // SessionCache slots (DenebClientSessionCache.kt). Owner-fingerprinted envelopes
    // like mail/feed, so a prior account's snapshot can't render under new credentials.
    fun getCachedSection(key: String): String? = settings.getStringOrNull(KEY_SECTION_CACHE_PREFIX + key)

    fun putCachedSection(key: String, json: String) {
        settings.putString(KEY_SECTION_CACHE_PREFIX + key, json)
    }

    fun removeCachedSection(key: String) {
        settings.remove(KEY_SECTION_CACHE_PREFIX + key)
    }

    /**
     * Purge ALL cached private content (every transcript + the inbox list). Called
     * when the gateway URL or client token changes: those cache keys are global, so
     * without this the prior gateway/account's chat and mail would render under the
     * new credentials on the next cold start (before any authenticated RPC).
     *
     * Deletes by key PREFIX rather than walking the manifest, because an interrupted
     * pre-commit write can leave an unpublished payload blob behind. Prefix deletion
     * catches both those blobs and legacy `tx_cache:<session>` entries.
     */
    @OptIn(ExperimentalSettingsApi::class)
    fun clearCachedContent() = transcriptCacheLock.withLock {
        settings.keys
            .filter {
                it.startsWith(KEY_TX_CACHE_PREFIX) ||
                    it.startsWith(KEY_TX_CACHE_BLOB_PREFIX) ||
                    it.startsWith(KEY_SECTION_CACHE_PREFIX) ||
                    it == KEY_TX_CACHE_LRU ||
                    it == KEY_TX_CACHE_MANIFEST ||
                    it == KEY_MAIL_CACHE ||
                    it == KEY_WORK_FEED_CACHE ||
                    it == KEY_CALENDAR_CACHE ||
                    it == KEY_APPROVALS_CACHE
            }
            .forEach { settings.remove(it) }
    }

    companion object {
        const val KEY_APP_OPENS = "app_opens"

        const val KEY_FEED_SEEN_IDS = "feed_seen_ids"
        const val KEY_HIDDEN_MORE_TILES = "hidden_more_tiles"
        const val KEY_COMPOSER_DRAFT = "composer_draft"
        const val KEY_COMPOSER_DRAFTS = "composer_drafts"
        const val COMPOSER_DRAFT_MAX = 8_000
        const val COMPOSER_DRAFT_MAX_SESSIONS = 20
        const val COMPOSER_DRAFT_NEW = "__new__"

        fun composerDraftSessionKey(conversationId: String?): String = conversationId?.takeIf { it.isNotBlank() } ?: COMPOSER_DRAFT_NEW

        // 더보기 기본 숨김: 매일 안 여는 콘솔/메타 표면. 설정 → 더보기 표시 항목에서 다시 켤 수 있다.
        // Keys must exist in [ai.deneb.deneb.moreGroups] — retired tiles do not belong here.
        val DEFAULT_HIDDEN_MORE_TILES: Set<String> = setOf(
            "deneb_rsi",
            "deneb_usage",
            "deneb_org",
            "deneb_dashboard",
        )
        const val KEY_BROWSER_BOOKMARKS = "browser_bookmarks"
        const val KEY_BROWSER_TABS = "browser_tabs_v1"
        const val KEY_BROWSER_LAST_URL = "browser_last_url"
        const val KEY_BROWSER_TRANSLATE_ENABLED = "browser_translate_enabled"
        const val KEY_NOTIFICATION_PERMISSION_ASKED = "notification_permission_asked"
        const val KEY_BROWSER_HISTORY = "browser_history"
        const val KEY_BROWSER_HOME_URL = "browser_home_url"
        const val KEY_BROWSER_ADBLOCK_ENABLED = "browser_adblock_enabled"
        const val KEY_CONVERSATIONS = "conversations_json"
        const val KEY_CURRENT_CONVERSATION_ID = "current_conversation_id"
        const val KEY_CURRENT_CONVERSATION_MIGRATED = "current_conversation_migrated"
        const val KEY_ENCRYPTION_KEY = "encryption_key"
        const val KEY_MIGRATION_COMPLETE = "migration_complete_v1"
        const val KEY_TOOL_PREFIX = "tool_enabled_"
        const val KEY_SOUL = "soul_text"
        const val KEY_MEMORY_ENABLED = "memory_enabled"
        const val KEY_WORK_SESSION = "workspace_session_work"
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
        const val KEY_APPROVALS_CACHE = "approvals_list_cache"
        const val KEY_SECTION_CACHE_PREFIX = "section_cache:"
        const val KEY_TX_CACHE_PREFIX = "tx_cache:"
        const val KEY_TX_CACHE_LRU = "tx_cache_lru"
        const val KEY_TX_CACHE_BLOB_PREFIX = "tx_cache_blob:"
        const val KEY_TX_CACHE_MANIFEST = "tx_cache_manifest_v2"
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
