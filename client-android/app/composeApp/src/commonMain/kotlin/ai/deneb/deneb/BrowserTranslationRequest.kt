package ai.deneb.deneb

internal const val MAX_BROWSER_TRANSLATE_SEGMENTS = 40
internal const val MAX_BROWSER_TRANSLATE_SEGMENT_CHARS = 1_200
internal const val MAX_BROWSER_TRANSLATE_TOTAL_CHARS = 32_000
internal const val MAX_BROWSER_TRANSLATE_JSON_CHARS = 48_000
internal const val MAX_BROWSER_TRANSLATE_REQUEST_ID_CHARS = 64

internal fun isBrowserTranslateEnvelopeWithinLimits(requestId: String, jsonChars: Int): Boolean = requestId.length in 1..MAX_BROWSER_TRANSLATE_REQUEST_ID_CHARS &&
    jsonChars in 0..MAX_BROWSER_TRANSLATE_JSON_CHARS

internal fun areBrowserTranslateSegmentsWithinLimits(segments: List<String>): Boolean {
    if (segments.isEmpty() || segments.size > MAX_BROWSER_TRANSLATE_SEGMENTS) return false
    var total = 0
    for (segment in segments) {
        if (segment.length > MAX_BROWSER_TRANSLATE_SEGMENT_CHARS) return false
        total += segment.length
        if (total > MAX_BROWSER_TRANSLATE_TOTAL_CHARS) return false
    }
    return true
}
