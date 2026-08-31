package ai.deneb.ui.dynamicui

/** Smallest frame an inline deneb-html answer gets, in dp. */
internal const val DENEB_HTML_MIN_HEIGHT_DP = 160

/**
 * Runaway backstop — **not** a content budget.
 *
 * Only a page sized off the viewport (`min-height:100vh`) can climb here: its
 * reported content height is derived from the frame we just handed it, so each
 * report asks for a little more. Real cards land far below — the 900dp ceiling
 * this replaced showed 3 of a briefing card's 9 sections and hid the rest.
 */
internal const val DENEB_HTML_MAX_HEIGHT_DP = 8000

/** Slack over the reported height, so sub-pixel rounding never leaves a scrollbar. */
private const val DENEB_HTML_HEIGHT_SLACK_DP = 8

/**
 * Frame height for a page reporting [reported] CSS px of content (== dp in a
 * WebView or iframe at default zoom).
 *
 * Entry point: the `DenebNative.height` bridge in `DenebHtmlView.android.kt`.
 * Tests: `commonTest/.../DenebHtmlFrameTest.kt`. Twin: `denebHtmlFrameHeight`
 * in andromeda's `denebHtmlSandbox.ts`. Contract: `docs/research/deneb-html.md`.
 *
 * The frame **grows to fit** — a card never scrolls inside the transcript — so
 * the latest report always wins, in **both directions**. Shrinking is not a
 * nicety: a page's first measurement can land before its fonts or its final
 * width do, and a frame that can only grow freezes at that inflated number and
 * leaves screens of blank under the content (shipped and reverted 2026-08-31).
 *
 * This works only because the page reports its own content height and not
 * `documentElement.scrollHeight`, which is `max(content, viewport)` — a frame
 * already too tall would just hear its own height back and could never learn it
 * had overshot. The measurement and this function are one mechanism; changing
 * one without the other reintroduces the blank.
 */
internal fun denebHtmlFrameHeight(reported: Int): Int {
    // Clamp BEFORE the slack: a bogus report near Int.MAX_VALUE would overflow
    // into a negative frame, which reads as a card that vanished.
    val safe = reported.coerceIn(0, DENEB_HTML_MAX_HEIGHT_DP - DENEB_HTML_HEIGHT_SLACK_DP)
    return (safe + DENEB_HTML_HEIGHT_SLACK_DP).coerceIn(DENEB_HTML_MIN_HEIGHT_DP, DENEB_HTML_MAX_HEIGHT_DP)
}
