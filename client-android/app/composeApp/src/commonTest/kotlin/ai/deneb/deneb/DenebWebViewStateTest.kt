package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class DenebWebViewStateTest {

    @Test
    fun constructorSeedsRequestedAndCurrentUrl() {
        val state = DenebWebViewState("https://example.com/start")

        assertEquals("https://example.com/start", state.url)
        assertEquals("https://example.com/start", state.currentUrl)
        assertEquals("", state.pageTitle)
        assertFalse(state.loading)
        assertEquals(0, state.progress)
        assertFalse(state.canGoBack)
        assertFalse(state.canGoForward)
        assertFalse(state.translateEnabled)
    }

    @Test
    fun constructorCanSeedTranslateEnabled() {
        val state = DenebWebViewState("https://example.com", translateEnabled = true)
        assertTrue(state.translateEnabled)
    }

    @Test
    fun constructorDefaultsAdBlockEnabledAndCanSeedOff() {
        assertTrue(DenebWebViewState("https://example.com").adBlockEnabled)
        assertFalse(DenebWebViewState("https://example.com", adBlockEnabled = false).adBlockEnabled)
        assertEquals(0, DenebWebViewState("https://example.com").adBlockedCount)
    }

    @Test
    fun loadChangesOnlyTheRequestedNavigationTarget() {
        val state = DenebWebViewState("https://example.com/start")
        state.currentUrl = "https://example.com/redirected"
        state.pageTitle = "Existing page"
        state.loading = true
        state.progress = 42

        state.load("https://example.com/next")

        assertEquals("https://example.com/next", state.url)
        assertEquals("https://example.com/redirected", state.currentUrl)
        assertEquals("Existing page", state.pageTitle)
        assertTrue(state.loading)
        assertEquals(42, state.progress)
    }

    @Test
    fun repeatedLoadsKeepTheLatestValueVerbatim() {
        val state = DenebWebViewState("about:blank")

        state.load("https://one.example/path")
        state.load("  custom://value with spaces  ")
        state.load("")

        assertEquals("", state.url)
        assertEquals("about:blank", state.currentUrl)
    }

    @Test
    fun backCommandsUseMonotonicTicks() {
        val state = DenebWebViewState("https://example.com")

        repeat(3) { state.goBack() }

        assertEquals(3, state.goBackTick)
        assertEquals(0, state.goForwardTick)
        assertEquals(0, state.reloadTick)
        assertEquals(0, state.stopTick)
    }

    @Test
    fun forwardReloadAndStopTicksAreIndependent() {
        val state = DenebWebViewState("https://example.com")

        state.goForward()
        state.goForward()
        state.reload()
        repeat(4) { state.stop() }

        assertEquals(0, state.goBackTick)
        assertEquals(2, state.goForwardTick)
        assertEquals(1, state.reloadTick)
        assertEquals(4, state.stopTick)
    }

    @Test
    fun commandsDoNotRewritePlatformReportedPageState() {
        val state = DenebWebViewState("https://example.com")
        state.currentUrl = "https://example.com/final"
        state.pageTitle = "Final"
        state.canGoBack = true
        state.canGoForward = true
        state.loading = true
        state.progress = 87

        state.goBack()
        state.goForward()
        state.reload()
        state.stop()

        assertEquals("https://example.com/final", state.currentUrl)
        assertEquals("Final", state.pageTitle)
        assertTrue(state.canGoBack)
        assertTrue(state.canGoForward)
        assertTrue(state.loading)
        assertEquals(87, state.progress)
    }

    @Test
    fun translationToggleIsIndependentFromNavigation() {
        val state = DenebWebViewState("https://example.com")

        state.translateEnabled = true
        state.load("https://example.com/ko")
        state.reload()

        assertTrue(state.translateEnabled)
        assertEquals("https://example.com/ko", state.url)
        assertEquals(1, state.reloadTick)

        state.translateEnabled = false
        assertFalse(state.translateEnabled)
    }

    @Test
    fun platformFieldsAcceptBoundaryValuesWithoutChangingCommands() {
        val state = DenebWebViewState("https://example.com")
        state.progress = 0
        state.progress = 100
        state.pageTitle = ""
        state.currentUrl = ""

        assertEquals(100, state.progress)
        assertEquals("", state.pageTitle)
        assertEquals("", state.currentUrl)
        assertEquals(0, state.goBackTick)
        assertEquals(0, state.goForwardTick)
        assertEquals(0, state.reloadTick)
        assertEquals(0, state.stopTick)
    }

    @Test
    fun rendererFailureRequiresExplicitRetryAndAdvancesGeneration() {
        val state = DenebWebViewState("https://example.com")
        state.platformState = "saved"
        state.canGoBack = true
        state.canGoForward = true
        state.progress = 75

        state.markRendererGone(crashed = true)

        assertTrue(state.rendererRecoveryPending)
        assertEquals(1, state.rendererGeneration)
        assertEquals(null, state.platformState)
        assertTrue(state.loadError.orEmpty().contains("비정상 종료"))
        assertFalse(state.canGoBack)
        assertFalse(state.canGoForward)
        assertEquals(0, state.progress)

        state.retry()
        assertFalse(state.rendererRecoveryPending)
        assertEquals(null, state.loadError)
        assertEquals(1, state.retryTick)
    }
}
