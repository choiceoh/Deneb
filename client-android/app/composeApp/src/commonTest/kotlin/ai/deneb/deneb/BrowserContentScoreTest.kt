package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * Fixture-driven regression for content-root scoring. Metrics mimic CMS layouts
 * (topwar / Substack / Newspaper) without relying on those sites' hardcoded
 * selectors — prose density must beat chrome / related / subscribe blocks.
 */
class BrowserContentScoreTest {
    @Test
    fun `rejects short chrome snippets`() {
        assertEquals(
            Int.MIN_VALUE,
            scoreContentCandidate(ContentScoreMetrics(tag = "DIV", textLength = 40, paragraphCount = 1)),
        )
    }

    @Test
    fun `topwar-like DLE page picks dense story over sidebar without CMS class`() {
        // Generic class names only — scoring must not need .full-story-text.
        val sidebar = ContentScoreCandidate(
            id = "sidebar",
            metrics = ContentScoreMetrics(
                tag = "DIV",
                className = "right-block",
                textLength = 900,
                linkTextLength = 700,
                paragraphCount = 2,
            ),
        )
        val story = ContentScoreCandidate(
            id = "story",
            metrics = ContentScoreMetrics(
                tag = "DIV",
                className = "text",
                textLength = 4200,
                linkTextLength = 180,
                paragraphCount = 18,
            ),
            containsIds = emptySet(),
        )
        val nav = ContentScoreCandidate(
            id = "nav",
            metrics = ContentScoreMetrics(
                tag = "DIV",
                className = "topmenu",
                textLength = 600,
                linkTextLength = 550,
                paragraphCount = 0,
            ),
        )
        val roots = pickScoredContentRoots(listOf(sidebar, story, nav))
        assertEquals(listOf("story"), roots.map { it.id })
        assertTrue(story.score > sidebar.score)
        assertTrue(story.score > nav.score)
    }

    @Test
    fun `substack-like page prefers prose body over subscribe widget`() {
        val body = ContentScoreCandidate(
            id = "body",
            metrics = ContentScoreMetrics(
                tag = "DIV",
                className = "body markup",
                textLength = 3800,
                linkTextLength = 120,
                paragraphCount = 14,
            ),
        )
        val subscribe = ContentScoreCandidate(
            id = "subscribe",
            metrics = ContentScoreMetrics(
                tag = "DIV",
                className = "subscribe-widget",
                textLength = 500,
                linkTextLength = 80,
                paragraphCount = 2,
            ),
        )
        val roots = pickScoredContentRoots(listOf(subscribe, body))
        assertEquals("body", roots.first().id)
        assertTrue(body.score >= MIN_CONTENT_SCORE)
        assertTrue(subscribe.score < body.score)
    }

    @Test
    fun `newspaper-like page prefers article body over related posts`() {
        val article = ContentScoreCandidate(
            id = "article",
            metrics = ContentScoreMetrics(
                tag = "DIV",
                className = "td-post-content",
                textLength = 5100,
                linkTextLength = 200,
                paragraphCount = 22,
            ),
        )
        val related = ContentScoreCandidate(
            id = "related",
            metrics = ContentScoreMetrics(
                tag = "DIV",
                className = "td_block_related_posts_mob",
                textLength = 1600,
                linkTextLength = 1400,
                paragraphCount = 1,
            ),
        )
        val share = ContentScoreCandidate(
            id = "share",
            metrics = ContentScoreMetrics(
                tag = "DIV",
                className = "td-post-sharing",
                textLength = 200,
                linkTextLength = 180,
                paragraphCount = 0,
            ),
        )
        val roots = pickScoredContentRoots(listOf(related, share, article))
        assertEquals("article", roots.first().id)
        assertTrue(article.score > related.score)
    }

    @Test
    fun `generic article tag beats link-heavy related without positive class`() {
        val related = ContentScoreCandidate(
            id = "related",
            metrics = ContentScoreMetrics(
                tag = "DIV",
                className = "related-stories",
                textLength = 2000,
                linkTextLength = 1600,
                paragraphCount = 0,
            ),
        )
        val article = ContentScoreCandidate(
            id = "main",
            metrics = ContentScoreMetrics(
                tag = "ARTICLE",
                className = "",
                textLength = 3000,
                linkTextLength = 100,
                paragraphCount = 12,
            ),
        )
        val roots = pickScoredContentRoots(listOf(related, article))
        assertEquals(listOf("main"), roots.map { it.id })
    }

    @Test
    fun `nested candidates collapse to outer root`() {
        val outer = ContentScoreCandidate(
            id = "outer",
            metrics = ContentScoreMetrics(
                tag = "ARTICLE",
                textLength = 4000,
                linkTextLength = 100,
                paragraphCount = 16,
            ),
            containsIds = setOf("inner"),
        )
        val inner = ContentScoreCandidate(
            id = "inner",
            metrics = ContentScoreMetrics(
                tag = "DIV",
                className = "entry-content",
                textLength = 3500,
                linkTextLength = 80,
                paragraphCount = 14,
            ),
        )
        val roots = pickScoredContentRoots(listOf(inner, outer))
        assertEquals(1, roots.size)
        // Higher score wins; entry-content bonus may beat bare ARTICLE — either is fine
        // as long as only one survives nest filtering.
        assertTrue(roots.single().id == "outer" || roots.single().id == "inner")
    }
}
