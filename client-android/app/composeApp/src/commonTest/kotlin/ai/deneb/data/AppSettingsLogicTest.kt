package ai.deneb.data

import com.russhwolf.settings.MapSettings
import kotlin.io.encoding.Base64
import kotlin.io.encoding.ExperimentalEncodingApi
import kotlin.test.Test
import kotlin.test.assertContentEquals
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * AppSettings behavior beyond the plain get/put passthroughs: the transcript-cache
 * LRU bound + eviction, credential-switch cache purge, the theme-mode init/migration,
 * feed-seen set hygiene, and encryption-key decoding. Migrations and hidden-tile
 * round-trips are covered elsewhere (AppSettingsMigrationsTest, MoreTileVisibilityTest).
 */
class AppSettingsLogicTest {

    private fun fresh() = AppSettings(MapSettings())

    // --- transcript cache LRU --------------------------------------------

    @Test
    fun `transcript cache keeps only the most-recent sessions and evicts the rest`() {
        val s = fresh()
        val total = AppSettings.TX_CACHE_MAX_SESSIONS + 2
        for (i in 0 until total) s.putCachedTranscript("sess$i", "body$i")

        // The two oldest were evicted; their payloads are gone.
        assertNull(s.getCachedTranscript("sess0"))
        assertNull(s.getCachedTranscript("sess1"))
        // The most-recent MAX all survive and return their bodies.
        for (i in 2 until total) assertEquals("body$i", s.getCachedTranscript("sess$i"))
    }

    @Test
    fun `removeCachedTranscript drops a single session`() {
        val s = fresh()
        s.putCachedTranscript("a", "x")
        s.putCachedTranscript("b", "y")
        s.removeCachedTranscript("a")
        assertNull(s.getCachedTranscript("a"))
        assertEquals("y", s.getCachedTranscript("b"))
    }

    // --- credential-switch purge -----------------------------------------

    @Test
    fun `clearCachedContent purges every cache but keeps ordinary preferences`() {
        val s = fresh()
        s.putCachedTranscript("main", "chat")
        s.putCachedMailList("mail")
        s.putCachedWorkFeed("feed")
        s.putCachedCalendar("cal")
        s.putCachedApprovalsList("approvals")
        s.setSoulText("keep me")
        s.setThemeMode(ThemeMode.Dark)

        s.clearCachedContent()

        assertNull(s.getCachedTranscript("main"))
        assertNull(s.getCachedMailList())
        assertNull(s.getCachedWorkFeed())
        assertNull(s.getCachedCalendar())
        assertNull(s.getCachedApprovalsList())
        // Non-cache preferences are untouched.
        assertEquals("keep me", s.getSoulText())
        assertEquals(ThemeMode.Dark, s.getThemeMode())
    }

    // --- theme mode init + legacy migration ------------------------------

    @Test
    fun `theme mode initializes from a stored valid value`() {
        val store = MapSettings().apply { putString(AppSettings.KEY_THEME_MODE, "Dark") }
        assertEquals(ThemeMode.Dark, AppSettings(store).getThemeMode())
    }

    @Test
    fun `an unrecognized stored theme falls back to System`() {
        val store = MapSettings().apply { putString(AppSettings.KEY_THEME_MODE, "Neon") }
        assertEquals(ThemeMode.System, AppSettings(store).getThemeMode())
    }

    @Test
    fun `the legacy OLED boolean migrates to OledBlack when no theme mode is set`() {
        val oled = MapSettings().apply { putBoolean(AppSettings.KEY_OLED_MODE_ENABLED, true) }
        assertEquals(ThemeMode.OledBlack, AppSettings(oled).getThemeMode())
        // No legacy flag and no theme mode → System.
        assertEquals(ThemeMode.System, AppSettings(MapSettings()).getThemeMode())
    }

    // --- feed seen set ---------------------------------------------------

    @Test
    fun `markFeedSeen dedupes, preserves insertion order, and ignores blanks`() {
        val s = fresh()
        s.markFeedSeen("a")
        s.markFeedSeen("b")
        s.markFeedSeen("")
        s.markFeedSeen("a") // duplicate
        assertEquals(listOf("a", "b"), s.getFeedSeenIds().toList())
    }

    // --- encryption key --------------------------------------------------

    @OptIn(ExperimentalEncodingApi::class)
    @Test
    fun `encryption key decodes valid base64 and rejects garbage`() {
        assertNull(fresh().getEncryptionKey()) // absent
        val bytes = byteArrayOf(1, 2, 3, 4, 5)
        val good = AppSettings(MapSettings().apply { putString(AppSettings.KEY_ENCRYPTION_KEY, Base64.encode(bytes)) })
        assertContentEquals(bytes, good.getEncryptionKey())
        val bad = AppSettings(MapSettings().apply { putString(AppSettings.KEY_ENCRYPTION_KEY, "!!!not-base64!!!") })
        assertNull(bad.getEncryptionKey())
    }

    // --- app-open counter ------------------------------------------------

    @Test
    fun `trackAppOpen increments and persists`() {
        val s = fresh()
        assertEquals(1, s.trackAppOpen())
        assertEquals(2, s.trackAppOpen())
        assertTrue(s.trackAppOpen() == 3)
    }

    // --- browser last URL ------------------------------------------------

    @Test
    fun `browser last url persists and clears on blank`() {
        val s = fresh()
        assertEquals("", s.getBrowserLastUrl())
        s.setBrowserLastUrl(" https://example.com/article ")
        assertEquals("https://example.com/article", s.getBrowserLastUrl())
        s.setBrowserLastUrl("   ")
        assertEquals("", s.getBrowserLastUrl())
    }

    @Test
    fun `browser translate preference and history json persist`() {
        val s = fresh()
        assertFalse(s.isBrowserTranslateEnabled())
        s.setBrowserTranslateEnabled(true)
        assertTrue(s.isBrowserTranslateEnabled())
        s.setBrowserTranslateEnabled(false)
        assertFalse(s.isBrowserTranslateEnabled())

        assertEquals("[]", s.getBrowserHistoryJson())
        s.setBrowserHistoryJson("""[{"url":"https://example.com","title":"Ex","visitedAtMs":1}]""")
        assertTrue(s.getBrowserHistoryJson().contains("example.com"))

        assertEquals("{}", s.getBrowserTabsJson())
        s.setBrowserTabsJson("""{"activeId":"tab-1","tabs":[]}""")
        assertTrue(s.getBrowserTabsJson().contains("tab-1"))
    }

    @Test
    fun `browser home url persists and clears on blank`() {
        val s = fresh()
        assertEquals("", s.getBrowserHomeUrl())
        s.setBrowserHomeUrl(" https://home.example/ ")
        assertEquals("https://home.example/", s.getBrowserHomeUrl())
        s.setBrowserHomeUrl("   ")
        assertEquals("", s.getBrowserHomeUrl())
    }

    @Test
    fun `browser adblock preference defaults on and persists`() {
        val s = fresh()
        assertTrue(s.isBrowserAdBlockEnabled())
        s.setBrowserAdBlockEnabled(false)
        assertFalse(s.isBrowserAdBlockEnabled())
        s.setBrowserAdBlockEnabled(true)
        assertTrue(s.isBrowserAdBlockEnabled())
    }
}
