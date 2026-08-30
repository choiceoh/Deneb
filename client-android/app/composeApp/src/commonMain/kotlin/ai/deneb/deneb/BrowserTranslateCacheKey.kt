package ai.deneb.deneb

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonPrimitive

/**
 * Cache key for one browser-translate segment.
 *
 * Entry point: [browserTranslateCacheKey], used only by `BrowserTranslateCache.key`.
 * Tests: `commonTest/.../BrowserTranslateCacheTest.kt`. Verify: `make ci ARGS=--kotlin`.
 *
 * The payload the page worker ships is not always the sentence: a segment inside a
 * paragraph arrives wrapped in a context envelope
 * (`deneb_translate_segment:v1:{text, parts, context, role}` — see
 * `deneb-translate.js`). Keying on that whole string made the same sentence miss
 * whenever its surrounding block differed, and the gateway then answered from its
 * OWN cache, which keys on the parsed sentence and drops context by design
 * (`translateops/translate_cache.go`). The round trip bought nothing.
 *
 * So this strips context and role. It does NOT strip `parts`: the reply for a
 * grouped segment is an array the page worker matches by length, and a wrong length
 * fails the whole unit ("count is sacred"). Two groups with the same joined text but
 * different part boundaries are therefore different cache entries.
 *
 * The sentinel is optional on input, so old and new client builds both parse here.
 * It is written as an escape on purpose: U+E000 is a private-use rune that renders
 * as nothing in terminals, grep and diffs, and a literal one has already caused one
 * misdiagnosis (see the note in `translateops/translate.go`).
 */
private val cacheKeyJson = Json {
    ignoreUnknownKeys = true
    isLenient = true
}

internal const val BROWSER_TRANSLATE_SENTINEL = "\uE000"
internal const val BROWSER_TRANSLATE_SEGMENT_PREFIX = "deneb_translate_segment:v1:"

internal fun browserTranslateCacheKey(targetLang: String, segment: String): String {
    val verbatim = "$targetLang $segment"
    val body = segment.removePrefix(BROWSER_TRANSLATE_SENTINEL)
    if (!body.startsWith(BROWSER_TRANSLATE_SEGMENT_PREFIX)) return verbatim

    val envelope = runCatching {
        cacheKeyJson.parseToJsonElement(body.removePrefix(BROWSER_TRANSLATE_SEGMENT_PREFIX)) as? JsonObject
    }.getOrNull() ?: return verbatim

    val parts = (envelope["parts"] as? JsonArray)
        ?.mapNotNull { runCatching { it.jsonPrimitive.contentOrNull }.getOrNull() }
        ?.filter { it.isNotEmpty() }
    if (!parts.isNullOrEmpty()) {
        // Part boundaries are part of the identity — see the doc comment. The count
        // is in the key so two different splits of the same text cannot collide.
        return "$targetLang p ${parts.size} ${parts.joinToString(" ")}"
    }

    val text = runCatching { envelope["text"]?.jsonPrimitive?.contentOrNull }.getOrNull()
    // An envelope we cannot read stays keyed verbatim rather than collapsing onto
    // some other segment under a truncated key.
    if (text.isNullOrEmpty()) return verbatim
    return "$targetLang t $text"
}
