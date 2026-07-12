package ai.deneb.ui.dynamicui

import kotlin.test.Test
import kotlin.test.assertNotNull
import kotlin.test.assertNull

class DenebUiIconCatalogTest {

    @Test
    fun `common model-authored icon names resolve`() {
        // Names the model reaches for in real cards; unresolved names render
        // blank, so the catalog covers frequent synonyms.
        val names = listOf(
            "compare", "vs", "video", "movie", "film", "message", "chat",
            "comment", "document", "file", "description", "folder",
            "history", "help", "question",
        )
        for (name in names) {
            assertNotNull(resolveIcon(name), "icon '$name' should resolve")
        }
    }

    @Test
    fun `unknown names still resolve to nothing`() {
        assertNull(resolveIcon("definitely-not-an-icon"))
    }
}
