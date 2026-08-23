package ai.deneb.deneb

/**
 * Omnibox autocomplete over the user's own browsing data (bookmarks + recent
 * visits). Pure ranking so the suggestion list is unit-testable; the chrome
 * only renders what this returns.
 */
/** Public because the stateless [DenebBrowserChrome] (exercised by
 *  renderPreviews) takes a suggestion list as a parameter. */
data class BrowserOmniboxSuggestion(
    val url: String,
    val title: String,
    val source: Source,
) {
    enum class Source { BOOKMARK, HISTORY }
}

/** A single letter would list half the history — wait for a real fragment. */
private const val BROWSER_OMNIBOX_MIN_QUERY = 2

/**
 * Up to [limit] suggestions for the address bar's [query]. Host-prefix matches
 * outrank host-substring, which outrank URL-substring, which outrank
 * title-substring; within a tier bookmarks come first, then most-recent. A URL
 * the user already typed in full is skipped — suggesting it back is noise.
 */
internal fun browserOmniboxSuggestions(
    query: String,
    bookmarks: List<BrowserBookmark>,
    history: List<BrowserVisit>,
    limit: Int = 5,
): List<BrowserOmniboxSuggestion> {
    val q = query.trim().lowercase()
    if (q.length < BROWSER_OMNIBOX_MIN_QUERY || limit <= 0) return emptyList()

    data class Ranked(val tier: Int, val order: Int, val suggestion: BrowserOmniboxSuggestion)

    val ranked = ArrayList<Ranked>()
    var order = 0
    for (bookmark in bookmarks) {
        val url = bookmark.url.trim()
        if (url.isEmpty()) continue
        val title = browserBookmarkDisplayTitle(bookmark)
        val tier = omniboxMatchTier(q, url, title) ?: continue
        ranked += Ranked(tier, order++, BrowserOmniboxSuggestion(url, title, BrowserOmniboxSuggestion.Source.BOOKMARK))
    }
    for (visit in history) {
        val url = visit.url.trim()
        if (url.isEmpty()) continue
        val title = browserVisitDisplayTitle(visit)
        val tier = omniboxMatchTier(q, url, title) ?: continue
        ranked += Ranked(tier, order++, BrowserOmniboxSuggestion(url, title, BrowserOmniboxSuggestion.Source.HISTORY))
    }

    val out = ArrayList<BrowserOmniboxSuggestion>(limit.coerceAtMost(ranked.size))
    for (entry in ranked.sortedWith(compareBy({ it.tier }, { it.order }))) {
        if (out.size >= limit) break
        if (out.any { it.url == entry.suggestion.url }) continue
        if (entry.suggestion.url.lowercase() == q) continue
        out += entry.suggestion
    }
    return out
}

/** Match strength for [url]/[title] against the lowercase query, or null when
 *  neither mentions the fragment. */
private fun omniboxMatchTier(q: String, url: String, title: String): Int? {
    val lowerUrl = url.lowercase()
    val host = lowerUrl
        .substringAfter("://", lowerUrl)
        .substringBefore('/')
        .substringBefore('?')
        .substringBefore('#')
    return when {
        host.startsWith(q) -> 0
        host.contains(q) -> 1
        lowerUrl.contains(q) -> 2
        title.lowercase().contains(q) -> 3
        else -> null
    }
}
