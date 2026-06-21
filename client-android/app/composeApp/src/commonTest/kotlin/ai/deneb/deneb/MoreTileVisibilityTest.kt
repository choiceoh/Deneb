package ai.deneb.deneb

import ai.deneb.data.AppSettings
import com.russhwolf.settings.MapSettings
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * The 더보기 ("자체앱 그리드") tile-hiding logic: the pure [visibleMoreEntries] filter (chatMode
 * gate ∪ user-hidden gate, with 설정 pinned) and the [AppSettings] hidden-set round-trip.
 */
class MoreTileVisibilityTest {

    private val allEntries: List<MoreEntry> = moreGroups.flatMap { it.second }

    @Test
    fun `nothing hidden in work mode shows every tile`() {
        val visible = visibleMoreEntries(allEntries, chatMode = false, hidden = emptySet())
        assertEquals(allEntries.map { it.key }, visible.map { it.key })
    }

    @Test
    fun `hidden keys are removed`() {
        val visible = visibleMoreEntries(allEntries, chatMode = false, hidden = setOf("deneb_search", "deneb_files"))
        assertFalse(visible.any { it.key == "deneb_search" })
        assertFalse(visible.any { it.key == "deneb_files" })
        // Untouched tiles remain.
        assertTrue(visible.any { it.key == "deneb_notebooks" })
    }

    @Test
    fun `settings tile is never hidden even if its key is in the hidden set`() {
        // alwaysShown pins 설정 — guards against locking out the un-hide control.
        val visible = visibleMoreEntries(allEntries, chatMode = false, hidden = setOf("deneb_config"))
        assertTrue(visible.any { it.key == "deneb_config" })
    }

    @Test
    fun `settings is not offered as a hideable entry`() {
        assertFalse(hideableMoreEntries.any { it.key == "deneb_config" })
        // Everything else hideable is present (e.g. browser, files, search).
        assertTrue(hideableMoreEntries.any { it.key == "deneb_browser" })
        assertTrue(hideableMoreEntries.any { it.key == "deneb_files" })
    }

    @Test
    fun `chat-mode and user-hidden gates compose`() {
        // chatMode drops 업무 데이터 tiles; the hidden set drops a non-work tile too. Both apply.
        val visible = visibleMoreEntries(allEntries, chatMode = true, hidden = setOf("deneb_browser"))
        // 업무 데이터 (workData=true) gone via chatMode.
        assertFalse(visible.any { it.key == "deneb_search" })
        assertFalse(visible.any { it.key == "deneb_dashboard" })
        assertFalse(visible.any { it.key == "deneb_files" })
        // user-hidden non-work tile gone via the hidden set.
        assertFalse(visible.any { it.key == "deneb_browser" })
        // 설정 (alwaysShown) survives both gates.
        assertTrue(visible.any { it.key == "deneb_config" })
    }

    @Test
    fun `every entry key is unique and non-blank`() {
        val keys = allEntries.map { it.key }
        assertEquals(keys.size, keys.toSet().size, "tile keys must be unique")
        assertTrue(keys.all { it.isNotBlank() })
    }

    @Test
    fun `hidden-tile set round-trips through AppSettings`() {
        val s = AppSettings(MapSettings())
        assertEquals(emptySet(), s.getHiddenMoreTiles())

        s.setMoreTileHidden("deneb_search", hidden = true)
        s.setMoreTileHidden("deneb_files", hidden = true)
        assertEquals(setOf("deneb_search", "deneb_files"), s.getHiddenMoreTiles())

        // Un-hiding removes only that key.
        s.setMoreTileHidden("deneb_search", hidden = false)
        assertEquals(setOf("deneb_files"), s.getHiddenMoreTiles())

        // Idempotent: hiding an already-hidden key keeps the set stable.
        s.setMoreTileHidden("deneb_files", hidden = true)
        assertEquals(setOf("deneb_files"), s.getHiddenMoreTiles())

        // Blank keys are ignored.
        s.setMoreTileHidden("", hidden = true)
        assertEquals(setOf("deneb_files"), s.getHiddenMoreTiles())
    }
}
