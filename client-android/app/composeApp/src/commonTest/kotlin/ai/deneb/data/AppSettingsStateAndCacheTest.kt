package ai.deneb.data

import app.cash.turbine.test
import com.russhwolf.settings.ExperimentalSettingsApi
import com.russhwolf.settings.MapSettings
import com.russhwolf.settings.Settings
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.test.runTest
import kotlin.io.encoding.Base64
import kotlin.io.encoding.ExperimentalEncodingApi
import kotlin.test.Test
import kotlin.test.assertContentEquals
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class AppSettingsStateAndCacheTest {

    private class FaultSettings(
        private val delegate: Settings = MapSettings(),
    ) : Settings by delegate {
        var failPutKey: String? = null

        override fun putString(key: String, value: String) {
            if (key == failPutKey) error("put failed: $key")
            delegate.putString(key, value)
        }
    }

    private data class Fixture(val raw: MapSettings, val settings: AppSettings)

    private fun fixture(block: MapSettings.() -> Unit = {}): Fixture {
        val raw = MapSettings().apply(block)
        return Fixture(raw, AppSettings(raw))
    }

    @Test
    fun themeFlowStartsFromPersistedMode() = runTest {
        val (_, settings) = fixture { putString(AppSettings.KEY_THEME_MODE, ThemeMode.Dark.name) }

        settings.themeModeFlow.test {
            assertEquals(ThemeMode.Dark, awaitItem())
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun settingThemePersistsAndEmitsTheNewMode() = runTest {
        val (raw, settings) = fixture()

        settings.themeModeFlow.test {
            assertEquals(ThemeMode.System, awaitItem())
            settings.setThemeMode(ThemeMode.OledBlack)
            assertEquals(ThemeMode.OledBlack, awaitItem())
            assertEquals("OledBlack", raw.getString(AppSettings.KEY_THEME_MODE, ""))
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun settingSameThemeDoesNotProduceDuplicateStateFlowEmission() = runTest {
        val (_, settings) = fixture { putString(AppSettings.KEY_THEME_MODE, ThemeMode.Light.name) }

        settings.themeModeFlow.test {
            assertEquals(ThemeMode.Light, awaitItem())
            settings.setThemeMode(ThemeMode.Light)
            expectNoEvents()
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun invalidStoredThemeDoesNotRewriteRawPreference() {
        val (raw, settings) = fixture { putString(AppSettings.KEY_THEME_MODE, "Neon") }

        assertEquals(ThemeMode.System, settings.getThemeMode())
        assertEquals("Neon", raw.getString(AppSettings.KEY_THEME_MODE, ""))
    }

    @Test
    fun explicitThemeWinsOverLegacyOledFlag() {
        val (_, settings) = fixture {
            putString(AppSettings.KEY_THEME_MODE, ThemeMode.Light.name)
            putBoolean(AppSettings.KEY_OLED_MODE_ENABLED, true)
        }

        assertEquals(ThemeMode.Light, settings.getThemeMode())
    }

    @Test
    fun uiScaleFlowStartsFromPersistedFloat() = runTest {
        val (_, settings) = fixture { putFloat(AppSettings.KEY_UI_SCALE, 1.25f) }

        settings.uiScaleFlow.test {
            assertEquals(1.25f, awaitItem())
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun settingUiScaleUpdatesStorageAndObserversTogether() = runTest {
        val (raw, settings) = fixture()

        settings.uiScaleFlow.test {
            awaitItem()
            settings.setUiScale(1.5f)
            assertEquals(1.5f, awaitItem())
            assertEquals(1.5f, raw.getFloat(AppSettings.KEY_UI_SCALE, 0f))
            assertEquals(1.5f, settings.getUiScale())
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun currentConversationNullRemovesRatherThanStoresSentinelText() {
        val (raw, settings) = fixture()
        settings.setCurrentConversationId("client:main:alpha")

        settings.setCurrentConversationId(null)

        assertNull(settings.getCurrentConversationId())
        assertFalse(raw.hasKey(AppSettings.KEY_CURRENT_CONVERSATION_ID))
    }

    @Test
    fun emptyConversationIdRemainsDistinctFromAbsent() {
        val (raw, settings) = fixture()

        settings.setCurrentConversationId("")

        assertEquals("", settings.getCurrentConversationId())
        assertTrue(raw.hasKey(AppSettings.KEY_CURRENT_CONVERSATION_ID))
    }

    @Test
    fun feedSeenParserDropsBlankSegmentsAndDeduplicatesInFirstSeenOrder() {
        val (_, settings) = fixture {
            putString(AppSettings.KEY_FEED_SEEN_IDS, ",a,,b,a, ,c,")
        }

        assertEquals(listOf("a", "b", "c"), settings.getFeedSeenIds().toList())
    }

    @Test
    fun feedSeenSetIsBoundedToNewestFiveHundredIds() {
        val (_, settings) = fixture()

        for (index in 0 until 505) settings.markFeedSeen("id-$index")

        val seen = settings.getFeedSeenIds().toList()
        assertEquals(500, seen.size)
        assertEquals("id-5", seen.first())
        assertEquals("id-504", seen.last())
    }

    @Test
    fun duplicateSeenIdDoesNotGrowOrReorderSet() {
        val (_, settings) = fixture()
        settings.markFeedSeen("a")
        settings.markFeedSeen("b")

        settings.markFeedSeen("a")

        assertEquals(listOf("a", "b"), settings.getFeedSeenIds().toList())
    }

    @Test
    fun hiddenTileTogglesAreIdempotentAndPreserveOtherKeys() {
        val (_, settings) = fixture()
        settings.setMoreTileHidden("mail", true)
        settings.setMoreTileHidden("calendar", true)
        settings.setMoreTileHidden("mail", true)

        settings.setMoreTileHidden("mail", false)

        assertEquals(setOf("calendar"), settings.getHiddenMoreTiles())
    }

    @Test
    fun blankHiddenTileKeyIsIgnoredForBothHideAndShow() {
        val (raw, settings) = fixture()

        settings.setMoreTileHidden("", true)
        settings.setMoreTileHidden("   ", false)

        assertFalse(raw.hasKey(AppSettings.KEY_HIDDEN_MORE_TILES))
    }

    @Test
    fun transcriptCacheRefreshMovesSessionToMostRecentPosition() {
        val (_, settings) = fixture()
        val max = AppSettings.TX_CACHE_MAX_SESSIONS
        for (index in 0 until max) settings.putCachedTranscript("s$index", "body$index")

        settings.putCachedTranscript("s0", "refreshed")
        settings.putCachedTranscript("overflow", "new")

        assertEquals("refreshed", settings.getCachedTranscript("s0"))
        assertNull(settings.getCachedTranscript("s1"))
        assertEquals("new", settings.getCachedTranscript("overflow"))
    }

    @Test
    fun updatingCachedSessionReplacesPayloadWithoutDuplicatingLruSlot() {
        val (raw, settings) = fixture()

        repeat(5) { settings.putCachedTranscript("same", "value-$it") }

        assertEquals("value-4", settings.getCachedTranscript("same"))
        assertTrue(raw.hasKey(AppSettings.KEY_TX_CACHE_MANIFEST))
        assertFalse(raw.hasKey(AppSettings.KEY_TX_CACHE_LRU))
    }

    @Test
    fun removingCachedSessionAlsoRemovesItFromLruIndex() {
        val (raw, settings) = fixture()
        settings.putCachedTranscript("a", "A")
        settings.putCachedTranscript("b", "B")

        settings.removeCachedTranscript("a")

        assertNull(settings.getCachedTranscript("a"))
        assertEquals("B", settings.getCachedTranscript("b"))

        settings.removeCachedTranscript("b")

        assertFalse(raw.hasKey(AppSettings.KEY_TX_CACHE_LRU))
        assertTrue(raw.hasKey(AppSettings.KEY_TX_CACHE_MANIFEST))
    }

    @Test
    fun corruptBlankAndDuplicateLruLinesAreRepairedDuringNextInsertion() {
        val (raw, settings) = fixture {
            putString(AppSettings.KEY_TX_CACHE_LRU, "\nold\nold\n\n")
            putString(AppSettings.KEY_TX_CACHE_PREFIX + "old", "old-body")
        }

        settings.putCachedTranscript("new", "new-body")

        assertFalse(raw.hasKey(AppSettings.KEY_TX_CACHE_LRU))
        assertFalse(raw.hasKey(AppSettings.KEY_TX_CACHE_PREFIX + "old"))
        assertEquals("old-body", settings.getCachedTranscript("old"))
        assertEquals("new-body", settings.getCachedTranscript("new"))
    }

    @Test
    fun failedManifestCommitKeepsPriorTranscriptGenerationReadable() {
        val raw = FaultSettings()
        val settings = AppSettings(raw)
        settings.putCachedTranscript("same", "before")
        raw.failPutKey = AppSettings.KEY_TX_CACHE_MANIFEST

        assertFailsWith<IllegalStateException> {
            settings.putCachedTranscript("same", "after")
        }

        raw.failPutKey = null
        assertEquals("before", settings.getCachedTranscript("same"))
        assertFalse(raw.hasKey(AppSettings.KEY_TX_CACHE_BLOB_PREFIX + "2"))
    }

    @Test
    fun failedLegacyMigrationLeavesTheLegacyGenerationReadable() {
        val raw = FaultSettings().apply {
            putString(AppSettings.KEY_TX_CACHE_LRU, "old")
            putString(AppSettings.KEY_TX_CACHE_PREFIX + "old", "legacy")
        }
        val settings = AppSettings(raw)
        raw.failPutKey = AppSettings.KEY_TX_CACHE_MANIFEST

        assertFailsWith<IllegalStateException> {
            settings.putCachedTranscript("new", "fresh")
        }

        raw.failPutKey = null
        assertEquals("legacy", settings.getCachedTranscript("old"))
        assertNull(settings.getCachedTranscript("new"))
    }

    @Test
    fun malformedManifestFailsClosedAndRemovesStaleLegacyData() {
        val (raw, settings) = fixture {
            putString(AppSettings.KEY_TX_CACHE_MANIFEST, "not-json")
            putString(AppSettings.KEY_TX_CACHE_LRU, "same")
            putString(AppSettings.KEY_TX_CACHE_PREFIX + "same", "stale")
        }

        assertNull(settings.getCachedTranscript("same"))

        assertTrue(raw.hasKey(AppSettings.KEY_TX_CACHE_MANIFEST))
        assertFalse(raw.hasKey(AppSettings.KEY_TX_CACHE_LRU))
        assertFalse(raw.hasKey(AppSettings.KEY_TX_CACHE_PREFIX + "same"))
    }

    @OptIn(ExperimentalSettingsApi::class)
    @Test
    fun successfulMutationCleansUnpublishedTranscriptBlobs() {
        val (raw, settings) = fixture()
        settings.putCachedTranscript("a", "A")
        val orphan = AppSettings.KEY_TX_CACHE_BLOB_PREFIX + "999"
        raw.putString(orphan, "orphan")

        settings.putCachedTranscript("b", "B")

        assertFalse(raw.hasKey(orphan))
        assertEquals(2, raw.keys.count { it.startsWith(AppSettings.KEY_TX_CACHE_BLOB_PREFIX) })
    }

    @OptIn(ExperimentalSettingsApi::class)
    @Test
    fun missingCommittedPayloadIsRemovedFromManifestOnRead() {
        val (raw, settings) = fixture()
        settings.putCachedTranscript("missing", "body")
        val payloadKey = raw.keys.single { it.startsWith(AppSettings.KEY_TX_CACHE_BLOB_PREFIX) }
        raw.remove(payloadKey)

        assertNull(settings.getCachedTranscript("missing"))
        settings.putCachedTranscript("healthy", "value")

        assertNull(settings.getCachedTranscript("missing"))
        assertEquals("value", settings.getCachedTranscript("healthy"))
    }

    @Test
    fun concurrentTranscriptWritesShareOneCommittedManifest() = runTest {
        val settings = AppSettings(MapSettings())

        (0 until AppSettings.TX_CACHE_MAX_SESSIONS)
            .map { index ->
                async(Dispatchers.Default) {
                    settings.putCachedTranscript("s$index", "body$index")
                }
            }
            .awaitAll()

        for (index in 0 until AppSettings.TX_CACHE_MAX_SESSIONS) {
            assertEquals("body$index", settings.getCachedTranscript("s$index"))
        }
    }

    @Test
    fun clearCachedContentDeletesOrphanTranscriptNotPresentInLru() {
        val (raw, settings) = fixture {
            putString(AppSettings.KEY_TX_CACHE_PREFIX + "orphan", "private")
            putString(AppSettings.KEY_TX_CACHE_LRU, "different")
        }

        settings.clearCachedContent()

        assertFalse(raw.hasKey(AppSettings.KEY_TX_CACHE_PREFIX + "orphan"))
        assertFalse(raw.hasKey(AppSettings.KEY_TX_CACHE_LRU))
    }

    @Test
    fun clearCachedContentRemovesAllFiveCacheFamilies() {
        val (_, settings) = fixture()
        settings.putCachedTranscript("session", "chat")
        settings.putCachedMailList("mail")
        settings.putCachedWorkFeed("feed")
        settings.putCachedCalendar("calendar")
        settings.putCachedApprovalsList("approvals")

        settings.clearCachedContent()

        assertNull(settings.getCachedTranscript("session"))
        assertNull(settings.getCachedMailList())
        assertNull(settings.getCachedWorkFeed())
        assertNull(settings.getCachedCalendar())
        assertNull(settings.getCachedApprovalsList())
    }

    @Test
    fun clearCachedContentDoesNotDeleteNearPrefixOrOrdinaryPreferences() {
        val (raw, settings) = fixture {
            putString("tx_cache", "near-prefix")
            putString("tx-cache:session", "different-prefix")
            putString(AppSettings.KEY_SOUL, "soul")
            putString(AppSettings.KEY_EMAIL_PENDING, "pending")
        }

        settings.clearCachedContent()

        assertEquals("near-prefix", raw.getString("tx_cache", ""))
        assertEquals("different-prefix", raw.getString("tx-cache:session", ""))
        assertEquals("soul", settings.getSoulText())
        assertEquals("pending", settings.getEmailPendingJson())
    }

    @OptIn(ExperimentalEncodingApi::class)
    @Test
    fun encryptionKeyPreservesArbitraryBinaryBytes() {
        val bytes = byteArrayOf(Byte.MIN_VALUE, -1, 0, 1, Byte.MAX_VALUE)
        val (_, settings) = fixture {
            putString(AppSettings.KEY_ENCRYPTION_KEY, Base64.encode(bytes))
        }

        assertContentEquals(bytes, settings.getEncryptionKey())
    }

    @Test
    fun emptyEncodedEncryptionKeyDecodesToAnEmptyKeyNotAbsence() {
        val (_, settings) = fixture { putString(AppSettings.KEY_ENCRYPTION_KEY, "") }

        assertContentEquals(byteArrayOf(), settings.getEncryptionKey())
    }

    @Test
    fun modelContextValuesAreIsolatedByExactModelId() {
        val (_, settings) = fixture()
        settings.setModelContextTokens("vendor/model", 32_768)
        settings.setModelContextTokens("vendor/model-long", 131_072)

        assertEquals(32_768, settings.getModelContextTokens("vendor/model"))
        assertEquals(131_072, settings.getModelContextTokens("vendor/model-long"))
        assertEquals(0, settings.getModelContextTokens("model"))
    }

    @Test
    fun appOpenCounterSaturatesInsteadOfWrappingNegative() {
        val (raw, settings) = fixture { putInt(AppSettings.KEY_APP_OPENS, Int.MAX_VALUE) }

        assertEquals(Int.MAX_VALUE, settings.trackAppOpen())
        assertEquals(Int.MAX_VALUE, settings.trackAppOpen())
        assertEquals(Int.MAX_VALUE, raw.getInt(AppSettings.KEY_APP_OPENS, 0))
    }

    @Test
    fun independentInstancesObservePersistedScalarValues() {
        val raw = MapSettings()
        val first = AppSettings(raw)
        first.setDaemonEnabled(true)
        first.setSchedulingEnabled(false)
        first.setLastSession("client:main:topic")

        val second = AppSettings(raw)

        assertTrue(second.isDaemonEnabled())
        assertFalse(second.isSchedulingEnabled())
        assertEquals("client:main:topic", second.lastSession())
    }
}
