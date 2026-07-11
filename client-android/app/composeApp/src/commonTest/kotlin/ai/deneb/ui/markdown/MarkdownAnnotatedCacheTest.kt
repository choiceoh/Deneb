package ai.deneb.ui.markdown

import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.ui.text.AnnotatedString
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertSame
import kotlin.test.assertTrue

class MarkdownAnnotatedCacheTest {

    @Test
    fun repeatedIdentityAndColorSchemeBuildOnlyOnce() {
        val nodes = Any()
        val colors = lightColorScheme()
        var builds = 0

        val first = cachedAnnotatedString(nodes, colors) {
            builds++
            AnnotatedString("first")
        }
        val second = cachedAnnotatedString(nodes, colors) {
            builds++
            AnnotatedString("second")
        }

        assertEquals(1, builds)
        assertSame(first, second)
        assertEquals("first", second.text)
    }

    @Test
    fun structurallyEqualNodeListsRemainDistinctIdentityKeys() {
        val firstNodes = listOf(Text("same"))
        val secondNodes = listOf(Text("same"))
        val colors = lightColorScheme()
        var builds = 0

        val first = cachedAnnotatedString(firstNodes, colors) {
            builds++
            AnnotatedString("first")
        }
        val second = cachedAnnotatedString(secondNodes, colors) {
            builds++
            AnnotatedString("second")
        }

        assertEquals(firstNodes, secondNodes)
        assertTrue(firstNodes !== secondNodes)
        assertEquals(2, builds)
        assertTrue(first !== second)
    }

    @Test
    fun structurallyEqualColorSchemesRemainDistinctIdentityKeys() {
        val nodes = Any()
        val firstColors = lightColorScheme()
        val secondColors = lightColorScheme()
        var builds = 0

        val first = cachedAnnotatedString(nodes, firstColors) {
            builds++
            AnnotatedString("first theme instance")
        }
        val second = cachedAnnotatedString(nodes, secondColors) {
            builds++
            AnnotatedString("second theme instance")
        }

        assertEquals(firstColors.primary, secondColors.primary)
        assertEquals(firstColors.background, secondColors.background)
        assertTrue(firstColors !== secondColors)
        assertEquals(2, builds)
        assertTrue(first !== second)
    }

    @Test
    fun oneNodeIdentityCanCacheIndependentThemes() {
        val nodes = Any()
        val light = lightColorScheme()
        val dark = darkColorScheme()
        var builds = 0

        val lightValue = cachedAnnotatedString(nodes, light) {
            builds++
            AnnotatedString("light")
        }
        val darkValue = cachedAnnotatedString(nodes, dark) {
            builds++
            AnnotatedString("dark")
        }

        assertSame(lightValue, cachedAnnotatedString(nodes, light) { error("light rebuilt") })
        assertSame(darkValue, cachedAnnotatedString(nodes, dark) { error("dark rebuilt") })
        assertEquals(2, builds)
    }

    @Test
    fun mutableNodesKeepTheirIdentityBasedEntryAfterMutation() {
        val nodes = mutableListOf(Text("before"))
        val colors = lightColorScheme()
        val original = cachedAnnotatedString(nodes, colors) { AnnotatedString("rendered before") }

        nodes[0] = Text("after")

        val cached = cachedAnnotatedString(nodes, colors) { AnnotatedString("rendered after") }
        assertSame(original, cached)
        assertEquals("rendered before", cached.text)
    }

    @Test
    fun builderFailureDoesNotPoisonTheCache() {
        val nodes = Any()
        val colors = darkColorScheme()
        var attempts = 0

        assertFailsWith<IllegalStateException> {
            cachedAnnotatedString(nodes, colors) {
                attempts++
                error("boom")
            }
        }

        val recovered = cachedAnnotatedString(nodes, colors) {
            attempts++
            AnnotatedString("recovered")
        }
        assertEquals(2, attempts)
        assertEquals("recovered", recovered.text)
        assertSame(recovered, cachedAnnotatedString(nodes, colors) { error("rebuilt") })
    }

    @Test
    fun cacheEvictsInFifoOrderAtTheCapacityBoundary() {
        val colors = lightColorScheme()
        val nodes = List(256) { Any() }
        var firstBuilds = 0

        val first = cachedAnnotatedString(nodes.first(), colors) {
            firstBuilds++
            AnnotatedString("first-$firstBuilds")
        }
        nodes.drop(1).forEachIndexed { index, node ->
            cachedAnnotatedString(node, colors) { AnnotatedString("entry-$index") }
        }

        assertSame(first, cachedAnnotatedString(nodes.first(), colors) { error("premature eviction") })
        val overflowNode = Any()
        cachedAnnotatedString(overflowNode, colors) { AnnotatedString("overflow") }

        val rebuilt = cachedAnnotatedString(nodes.first(), colors) {
            firstBuilds++
            AnnotatedString("first-$firstBuilds")
        }
        assertEquals(2, firstBuilds)
        assertEquals("first-2", rebuilt.text)
        assertTrue(first !== rebuilt)
    }

    @Test
    fun cacheHitDoesNotRefreshFifoPosition() {
        val colors = darkColorScheme()
        val nodes = List(256) { Any() }
        var firstBuilds = 0

        nodes.forEachIndexed { index, node ->
            cachedAnnotatedString(node, colors) {
                if (index == 0) firstBuilds++
                AnnotatedString("value-$index")
            }
        }
        cachedAnnotatedString(nodes.first(), colors) { error("unexpected miss") }
        cachedAnnotatedString(Any(), colors) { AnnotatedString("overflow") }

        cachedAnnotatedString(nodes.first(), colors) {
            firstBuilds++
            AnnotatedString("rebuilt")
        }
        assertEquals(2, firstBuilds)
    }
}
