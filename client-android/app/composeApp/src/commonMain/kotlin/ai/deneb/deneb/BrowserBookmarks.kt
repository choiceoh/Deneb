package ai.deneb.deneb

import ai.deneb.data.decodeStoredJsonOrDefault
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlin.time.Clock
import kotlin.time.ExperimentalTime

private const val BROWSER_BOOKMARK_LIMIT = 80
private const val BROWSER_BOOKMARK_TITLE_LIMIT = 96

private val browserBookmarksJson = Json { ignoreUnknownKeys = true }

@Serializable
internal data class BrowserBookmark(
    val url: String = "",
    val title: String = "",
    val addedAtMs: Long = 0,
)

internal fun decodeBrowserBookmarks(raw: String): List<BrowserBookmark> = decodeStoredJsonOrDefault(
    raw = raw,
    defaultValue = { emptyList() },
    decode = { browserBookmarksJson.decodeFromString<List<BrowserBookmark>>(it) },
)
    .asSequence()
    .mapNotNull { bookmark ->
        val url = canonicalBrowserBookmarkUrl(bookmark.url)
        if (!canBookmarkUrl(url)) return@mapNotNull null
        bookmark.copy(url = url, title = cleanBrowserBookmarkTitle(bookmark.title, url))
    }
    .distinctBy { it.url }
    .take(BROWSER_BOOKMARK_LIMIT)
    .toList()

internal fun encodeBrowserBookmarks(bookmarks: List<BrowserBookmark>): String = browserBookmarksJson.encodeToString(bookmarks.sanitizedBrowserBookmarks())

internal fun toggleBrowserBookmark(
    bookmarks: List<BrowserBookmark>,
    url: String,
    title: String,
    nowMs: Long = browserBookmarkNowMs(),
): List<BrowserBookmark> {
    val key = canonicalBrowserBookmarkUrl(url)
    if (!canBookmarkUrl(key)) return bookmarks.sanitizedBrowserBookmarks()
    if (bookmarks.any { canonicalBrowserBookmarkUrl(it.url) == key }) {
        return bookmarks.filterNot { canonicalBrowserBookmarkUrl(it.url) == key }.sanitizedBrowserBookmarks()
    }
    val next = listOf(
        BrowserBookmark(
            url = key,
            title = cleanBrowserBookmarkTitle(title, key),
            addedAtMs = nowMs,
        ),
    ) + bookmarks
    return next.sanitizedBrowserBookmarks()
}

internal fun removeBrowserBookmark(bookmarks: List<BrowserBookmark>, url: String): List<BrowserBookmark> {
    val key = canonicalBrowserBookmarkUrl(url)
    return bookmarks.filterNot { canonicalBrowserBookmarkUrl(it.url) == key }.sanitizedBrowserBookmarks()
}

internal fun isBrowserBookmarked(bookmarks: List<BrowserBookmark>, url: String): Boolean {
    val key = canonicalBrowserBookmarkUrl(url)
    return canBookmarkUrl(key) && bookmarks.any { canonicalBrowserBookmarkUrl(it.url) == key }
}

internal fun canBookmarkUrl(url: String): Boolean {
    val s = canonicalBrowserBookmarkUrl(url)
    val schemeLength = when {
        s.startsWith("https://", ignoreCase = true) -> 8
        s.startsWith("http://", ignoreCase = true) -> 7
        else -> return false
    }
    if (s.any { it.isWhitespace() || it.isISOControl() }) return false
    val authority = s.drop(schemeLength).substringBefore('/').substringBefore('?').substringBefore('#')
    return authority.isNotBlank()
}

/**
 * Resolve the browser entry URL: explicit nav route, else last http(s) page,
 * else the user-chosen home, else blank.
 */
internal fun resolveBrowserStartUrl(navUrl: String, lastUrl: String, homeUrl: String = ""): String {
    val nav = navUrl.trim()
    if (nav.isNotEmpty()) return nav
    val last = lastUrl.trim()
    if (canBookmarkUrl(last)) return last
    val home = homeUrl.trim()
    return if (canBookmarkUrl(home)) home else ""
}

internal fun browserBookmarkDisplayTitle(bookmark: BrowserBookmark): String = bookmark.title.ifBlank { browserBookmarkHost(bookmark.url) }.ifBlank { bookmark.url }

private fun List<BrowserBookmark>.sanitizedBrowserBookmarks(): List<BrowserBookmark> = asSequence()
    .mapNotNull { bookmark ->
        val url = canonicalBrowserBookmarkUrl(bookmark.url)
        if (!canBookmarkUrl(url)) return@mapNotNull null
        bookmark.copy(url = url, title = cleanBrowserBookmarkTitle(bookmark.title, url))
    }
    .distinctBy { it.url }
    .take(BROWSER_BOOKMARK_LIMIT)
    .toList()

private fun canonicalBrowserBookmarkUrl(url: String): String = url.trim()

private fun cleanBrowserBookmarkTitle(title: String, url: String): String {
    var cleaned = title
        .trim()
        .replace(Regex("\\s+"), " ")
        .take(BROWSER_BOOKMARK_TITLE_LIMIT)
    if (cleaned.lastOrNull()?.isHighSurrogate() == true) cleaned = cleaned.dropLast(1)
    return cleaned.ifBlank { browserBookmarkHost(url) }
}

private fun browserBookmarkHost(url: String): String {
    val withoutScheme = url.substringAfter("://", url)
    return withoutScheme
        .substringBefore('/')
        .substringBefore('?')
        .substringBefore('#')
        .let { if (it.startsWith("www.", ignoreCase = true)) it.drop(4) else it }
}

@OptIn(ExperimentalTime::class)
private fun browserBookmarkNowMs(): Long = Clock.System.now().toEpochMilliseconds()
