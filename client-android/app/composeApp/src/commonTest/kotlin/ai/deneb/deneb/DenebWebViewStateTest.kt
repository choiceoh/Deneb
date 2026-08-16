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
        val state = DenebWebViewState("https://example.com", initialPageTitle = "Saved title", translateEnabled = true)
        assertTrue(state.translateEnabled)
        assertEquals("Saved title", state.pageTitle)
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
    fun omniboxDraftSurvivesTabReattachUntilThatTabNavigates() {
        val first = DenebWebViewState("https://one.example")
        val second = DenebWebViewState("https://two.example")

        first.editOmnibox("unfinished search")
        second.editOmnibox("second draft")
        first.syncOmniboxWithCurrentUrl()
        second.syncOmniboxWithCurrentUrl()

        assertEquals("unfinished search", first.omniboxDraft)
        assertEquals("second draft", second.omniboxDraft)

        first.currentUrl = "https://one.example/landed"
        first.syncOmniboxWithCurrentUrl()
        assertEquals("https://one.example/landed", first.omniboxDraft)
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

    @Test
    fun rendererRecoveryLoadsTheLastCapturedPageInsteadOfTheStaleRequest() {
        val state = DenebWebViewState("https://example.com/requested")
        state.currentUrl = "https://example.com/committed"
        state.markRendererGone(crashed = true)

        assertEquals(
            "https://example.com/committed",
            browserWebViewCommandUrl(
                requestedUrl = state.url,
                currentUrl = state.currentUrl,
                rendererRecoveryUrl = state.rendererRecoveryUrl,
                rendererRecoveryPending = state.rendererRecoveryPending,
            ),
        )
    }

    @Test
    fun commandCursorDoesNotReplayCommandsAfterRendererReattach() {
        val state = DenebWebViewState("https://example.com")
        state.goBack()
        state.reload()
        val attached = BrowserCommandCursor(state)

        assertFalse(attached.consumeGoBack(state))
        assertFalse(attached.consumeReload(state))

        state.goBack()
        assertTrue(attached.consumeGoBack(state))
        assertFalse(attached.consumeGoBack(state))
    }

    @Test
    fun navigationCommitsAreOneShotAndDoNotReplayOnTabSelection() {
        val state = DenebWebViewState("https://example.com")
        state.commitNavigation("https://example.com/article")
        val commit = state.pendingNavigationCommit ?: error("missing commit")

        assertEquals("https://example.com/article", state.consumeNavigationCommit(commit))
        assertEquals(null, state.consumeNavigationCommit(commit))
        assertEquals(null, state.pendingNavigationCommit)

        state.commitNavigation("https://example.com/article")
        assertEquals(null, state.pendingNavigationCommit)
        state.commitNavigation("https://example.com/article", force = true)
        assertTrue(state.pendingNavigationCommit != null)
    }

    @Test
    fun newLoadsClearDetachedScrollAndTranslationProgress() {
        val state = DenebWebViewState("https://example.com")
        state.platformState = "saved"
        state.platformScrollX = 20
        state.platformScrollY = 800
        state.updateTranslationProgress(total = 10, applied = 4, pending = 6, failed = 0)

        state.load("https://example.com/next")

        assertEquals(null, state.platformState)
        assertEquals(0, state.platformScrollX)
        assertEquals(0, state.platformScrollY)
        assertEquals(BrowserTranslationProgress(), state.translationProgress)
    }

    @Test
    fun translationStatusMakesScanningProgressCompletionAndFailureVisible() {
        assertTrue(browserTranslationStatusText(BrowserTranslationProgress()).contains("찾는 중"))
        assertEquals(
            "번역 중 · 4/10",
            browserTranslationStatusText(BrowserTranslationProgress(scanned = true, total = 10, applied = 4, pending = 6)),
        )
        assertTrue(
            browserTranslationStatusText(BrowserTranslationProgress(scanned = true, total = 10, applied = 9, failed = 1))
                .contains("일부 실패"),
        )
        assertEquals(
            "번역 완료 · 10개",
            browserTranslationStatusText(BrowserTranslationProgress(scanned = true, total = 10, applied = 10)),
        )
        assertEquals("번역할 텍스트 없음", browserTranslationStatusText(BrowserTranslationProgress(scanned = true)))
    }
}
