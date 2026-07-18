package ai.deneb.deneb

import ai.deneb.data.decodeStoredJsonOrDefault
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlin.time.Clock
import kotlin.time.ExperimentalTime

private const val BROWSER_HISTORY_LIMIT = 20
private const val BROWSER_HISTORY_TITLE_LIMIT = 96

private val browserHistoryJson = Json { ignoreUnknownKeys = true }

@Serializable
internal data class BrowserVisit(
    val url: String = "",
    val title: String = "",
    val visitedAtMs: Long = 0,
)

internal fun decodeBrowserHistory(raw: String): List<BrowserVisit> = decodeStoredJsonOrDefault(
    raw = raw,
    defaultValue = { emptyList() },
    decode = { browserHistoryJson.decodeFromString<List<BrowserVisit>>(it) },
).sanitizedBrowserHistory()

internal fun encodeBrowserHistory(visits: List<BrowserVisit>): String = browserHistoryJson.encodeToString(visits.sanitizedBrowserHistory())

internal fun recordBrowserVisit(
    visits: List<BrowserVisit>,
    url: String,
    title: String,
    nowMs: Long = browserHistoryNowMs(),
): List<BrowserVisit> {
    val key = url.trim()
    if (!canBookmarkUrl(key)) return visits.sanitizedBrowserHistory()
    val entry = BrowserVisit(
        url = key,
        title = cleanBrowserHistoryTitle(title, key),
        visitedAtMs = nowMs,
    )
    return (listOf(entry) + visits.filterNot { it.url.trim() == key }).sanitizedBrowserHistory()
}

internal fun removeBrowserVisit(visits: List<BrowserVisit>, url: String): List<BrowserVisit> {
    val key = url.trim()
    return visits.filterNot { it.url.trim() == key }.sanitizedBrowserHistory()
}

internal fun browserVisitDisplayTitle(visit: BrowserVisit): String = visit.title.ifBlank { browserHistoryHost(visit.url) }.ifBlank { visit.url }

private fun List<BrowserVisit>.sanitizedBrowserHistory(): List<BrowserVisit> = asSequence()
    .mapNotNull { visit ->
        val url = visit.url.trim()
        if (!canBookmarkUrl(url)) return@mapNotNull null
        visit.copy(url = url, title = cleanBrowserHistoryTitle(visit.title, url))
    }
    .distinctBy { it.url }
    .take(BROWSER_HISTORY_LIMIT)
    .toList()

private fun cleanBrowserHistoryTitle(title: String, url: String): String {
    var cleaned = title
        .trim()
        .replace(Regex("\\s+"), " ")
        .take(BROWSER_HISTORY_TITLE_LIMIT)
    if (cleaned.lastOrNull()?.isHighSurrogate() == true) cleaned = cleaned.dropLast(1)
    return cleaned.ifBlank { browserHistoryHost(url) }
}

private fun browserHistoryHost(url: String): String {
    val withoutScheme = url.substringAfter("://", url)
    return withoutScheme
        .substringBefore('/')
        .substringBefore('?')
        .substringBefore('#')
        .let { if (it.startsWith("www.", ignoreCase = true)) it.drop(4) else it }
}

@OptIn(ExperimentalTime::class)
private fun browserHistoryNowMs(): Long = Clock.System.now().toEpochMilliseconds()
