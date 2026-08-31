package ai.deneb.ui.dynamicui

/** Smallest frame an inline deneb-html answer gets, in dp. */
internal const val DENEB_HTML_MIN_HEIGHT_DP = 160

/**
 * Runaway backstop — **not** a content budget.
 *
 * A page sized off the viewport (`min-height:200vh`) reports a height derived
 * from the frame we just handed it, so growth has to terminate somewhere. Real
 * cards land far below this: the 6KB briefing card that exposed the old 900dp
 * ceiling showed 3 of its 9 sections there, with no way to reach the rest.
 */
internal const val DENEB_HTML_MAX_HEIGHT_DP = 8000

/** Slack over the reported height, so sub-pixel rounding never leaves a scrollbar. */
private const val DENEB_HTML_HEIGHT_SLACK_DP = 8

/**
 * Next frame height for a page reporting [reported] CSS px (== dp in a WebView
 * or iframe at default zoom) while the frame is [current] dp tall.
 *
 * Entry point: the `DenebNative.height` bridge in `DenebHtmlView.android.kt`.
 * Tests: `commonTest/.../DenebHtmlFrameTest.kt`. Twin: `denebHtmlFrameHeight`
 * in andromeda's `denebHtmlSandbox.ts`. Contract: `docs/research/deneb-html.md`.
 *
 * The frame **grows to fit** — a card must never scroll inside the transcript.
 * That makes every report after the first an echo: once the frame fits, the
 * page's `documentElement.scrollHeight` just hands the frame's own height back,
 * so adding slack unconditionally would ratchet the card upward forever. That
 * echo is why the ceiling used to be low, and why a long card was truncated
 * instead of shown. Only a report that EXCEEDS the current frame is news.
 */
internal fun denebHtmlFrameHeight(current: Int, reported: Int): Int {
    val floor = current.coerceIn(DENEB_HTML_MIN_HEIGHT_DP, DENEB_HTML_MAX_HEIGHT_DP)
    if (reported <= floor) return floor
    // Clamp BEFORE adding slack: a bogus report near Int.MAX_VALUE would overflow
    // into a negative frame, which reads as a card that vanished.
    val target = reported.coerceAtMost(DENEB_HTML_MAX_HEIGHT_DP - DENEB_HTML_HEIGHT_SLACK_DP)
    return (target + DENEB_HTML_HEIGHT_SLACK_DP).coerceAtLeast(floor)
}
