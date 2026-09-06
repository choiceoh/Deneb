package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class BrowserLoadCommandTest {
    @Test
    fun tapWithNoPlatformViewIsDeliveredWhenOneAttaches() {
        // The windows with no WebView are real — before the AndroidView factory
        // runs, after a release, after a renderer crash. A bookmark tapped in one
        // of them used to be consumed and dropped: the sheet closed, the page
        // never changed, and nothing retried it.
        val state = DenebWebViewState("https://start.example")
        val cursor = BrowserCommandCursor(state)
        state.load("https://bookmark.example")

        assertNull(
            browserLoadCommand(state, cursor, hasView = false, target = "https://bookmark.example", lastCommandUrl = ""),
            "a command with nowhere to go must not be answered",
        )
        assertEquals(
            "https://bookmark.example",
            browserLoadCommand(state, cursor, hasView = true, target = "https://bookmark.example", lastCommandUrl = ""),
            "the attach that follows has to deliver the waiting tap",
        )
    }

    @Test
    fun aDeliveredCommandIsSpentExactlyOnce() {
        val state = DenebWebViewState("https://start.example")
        val cursor = BrowserCommandCursor(state)
        state.load("https://bookmark.example")

        assertEquals(
            "https://bookmark.example",
            browserLoadCommand(state, cursor, hasView = true, target = "https://bookmark.example", lastCommandUrl = ""),
        )
        // Re-running the effect (a recomposition, an attach) must not re-navigate
        // the page the user has since moved on from.
        assertNull(
            browserLoadCommand(
                state,
                cursor,
                hasView = true,
                target = "https://bookmark.example",
                lastCommandUrl = "https://bookmark.example",
            ),
        )
    }

    @Test
    fun tappingTheSameBookmarkAgainStillLoads() {
        val state = DenebWebViewState("https://start.example")
        val cursor = BrowserCommandCursor(state)
        state.load("https://bookmark.example")
        browserLoadCommand(state, cursor, hasView = true, target = "https://bookmark.example", lastCommandUrl = "")

        // Same address, second tap: an explicit command outranks the guard.
        state.load("https://bookmark.example")
        assertEquals(
            "https://bookmark.example",
            browserLoadCommand(
                state,
                cursor,
                hasView = true,
                target = "https://bookmark.example",
                lastCommandUrl = "https://bookmark.example",
            ),
        )
    }

    @Test
    fun compositionWithoutACommandLeavesTheRestoredPageAlone() {
        val state = DenebWebViewState("https://restored.example")
        val cursor = BrowserCommandCursor(state)
        assertNull(
            browserLoadCommand(
                state,
                cursor,
                hasView = true,
                target = "https://restored.example",
                lastCommandUrl = "https://restored.example",
            ),
            "re-composing must not reload the page the WebView already restored",
        )
    }

    @Test
    fun blankTargetSpendsNothingUseful() {
        val state = DenebWebViewState("")
        val cursor = BrowserCommandCursor(state)
        assertNull(browserLoadCommand(state, cursor, hasView = true, target = "", lastCommandUrl = ""))
    }
}
