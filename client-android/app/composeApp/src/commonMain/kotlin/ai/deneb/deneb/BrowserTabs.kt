package ai.deneb.deneb

import ai.deneb.data.decodeStoredJsonOrDefault
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlin.time.Clock
import kotlin.time.ExperimentalTime

// Eight tabs cost nothing beyond tab metadata: only the ACTIVE tab holds a live
// WebView — background tabs park a state Bundle — so the cap guards UI clutter,
// not memory.
internal const val BROWSER_TAB_LIMIT = 8
private const val BROWSER_TAB_ID_LIMIT = 64
private const val BROWSER_TAB_TITLE_LIMIT = 96

private val browserTabsJson = Json { ignoreUnknownKeys = true }

/** Durable, platform-neutral part of one browser tab. WebView history/scroll is
 * kept separately in the Android runtime while the screen remains alive. */
@Serializable
internal data class BrowserTabSnapshot(
    val id: String = "",
    val url: String = "",
    val title: String = "",
    val translateEnabled: Boolean = false,
    val lastUsedAtMs: Long = 0,
)

@Serializable
internal data class BrowserTabStore(
    val activeId: String = "",
    val tabs: List<BrowserTabSnapshot> = emptyList(),
)

internal fun decodeBrowserTabStore(raw: String): BrowserTabStore = decodeStoredJsonOrDefault(
    raw = raw,
    defaultValue = { BrowserTabStore() },
    decode = { browserTabsJson.decodeFromString<BrowserTabStore>(it) },
).sanitized()

internal fun encodeBrowserTabStore(store: BrowserTabStore): String = browserTabsJson.encodeToString(BrowserTabStore.serializer(), store.sanitized())

/**
 * Resolves entry into the browser. A routed web link gets its own tab while room
 * remains. At the tab cap the opener is preserved; the UI asks the user to
 * close a tab before continuing. A lone blank tab is reused instead of leaving
 * dead clutter.
 */
internal fun resolveBrowserTabStore(
    stored: BrowserTabStore,
    explicitUrl: String,
    fallbackUrl: String,
    translateDefault: Boolean,
    nowMs: Long = browserTabNowMs(),
): BrowserTabStore {
    val store = stored.sanitized()
    val explicit = explicitUrl.trim().takeIf(::canBookmarkUrl)
    val fallback = fallbackUrl.trim().takeIf(::canBookmarkUrl).orEmpty()

    if (store.tabs.isEmpty()) {
        val first = newBrowserTabSnapshot(
            tabs = emptyList(),
            url = explicit ?: fallback,
            translateEnabled = translateDefault,
            nowMs = nowMs,
        )
        return BrowserTabStore(activeId = first.id, tabs = listOf(first))
    }

    if (explicit == null) {
        return store.ensureActive(nowMs)
    }

    store.tabs.firstOrNull { it.url == explicit }?.let { existing ->
        return store.copy(
            activeId = existing.id,
            tabs = store.tabs.map { if (it.id == existing.id) it.copy(lastUsedAtMs = nowMs) else it },
        ).sanitized()
    }

    val activeIndex = store.tabs.indexOfFirst { it.id == store.activeId }.let { if (it >= 0) it else 0 }
    val active = store.tabs[activeIndex]
    val replacement = newBrowserTabSnapshot(
        tabs = store.tabs,
        url = explicit,
        translateEnabled = translateDefault,
        nowMs = nowMs,
    )
    val next = when {
        store.tabs.size == 1 && active.url.isBlank() -> store.tabs.toMutableList().apply { this[activeIndex] = replacement }
        store.tabs.size < BROWSER_TAB_LIMIT -> store.tabs + replacement
        else -> return store
    }
    return BrowserTabStore(activeId = replacement.id, tabs = next).sanitized()
}

internal fun addBrowserTab(
    store: BrowserTabStore,
    translateDefault: Boolean,
    nowMs: Long = browserTabNowMs(),
): BrowserTabStore {
    val clean = store.sanitized()
    if (clean.tabs.size >= BROWSER_TAB_LIMIT) return clean
    val tab = newBrowserTabSnapshot(clean.tabs, url = "", translateEnabled = translateDefault, nowMs = nowMs)
    return BrowserTabStore(activeId = tab.id, tabs = clean.tabs + tab)
}

internal fun selectBrowserTab(store: BrowserTabStore, id: String, nowMs: Long = browserTabNowMs()): BrowserTabStore {
    val clean = store.sanitized()
    if (clean.tabs.none { it.id == id }) return clean
    return clean.copy(
        activeId = id,
        tabs = clean.tabs.map { if (it.id == id) it.copy(lastUsedAtMs = nowMs) else it },
    )
}

internal fun closeBrowserTab(
    store: BrowserTabStore,
    id: String,
    translateDefault: Boolean,
    nowMs: Long = browserTabNowMs(),
): BrowserTabStore {
    val clean = store.sanitized()
    val closedIndex = clean.tabs.indexOfFirst { it.id == id }
    if (closedIndex < 0) return clean
    val remaining = clean.tabs.filterNot { it.id == id }
    if (remaining.isEmpty()) {
        val blank = newBrowserTabSnapshot(emptyList(), url = "", translateEnabled = translateDefault, nowMs = nowMs)
        return BrowserTabStore(activeId = blank.id, tabs = listOf(blank))
    }
    val nextActive = if (clean.activeId == id) {
        remaining[(closedIndex - 1).coerceAtLeast(0).coerceAtMost(remaining.lastIndex)].id
    } else {
        clean.activeId.takeIf { active -> remaining.any { it.id == active } } ?: remaining.first().id
    }
    return BrowserTabStore(activeId = nextActive, tabs = remaining).sanitized()
}

internal fun updateBrowserTab(
    store: BrowserTabStore,
    id: String,
    url: String,
    title: String,
    translateEnabled: Boolean,
    nowMs: Long = browserTabNowMs(),
): BrowserTabStore {
    val clean = store.sanitized()
    val normalizedUrl = url.trim().takeIf { it.isEmpty() || canBookmarkUrl(it) }
    if (normalizedUrl == null || clean.tabs.none { it.id == id }) return clean
    return clean.copy(
        tabs = clean.tabs.map { tab ->
            if (tab.id != id) {
                tab
            } else {
                tab.copy(
                    url = normalizedUrl,
                    title = cleanBrowserTabTitle(title),
                    translateEnabled = translateEnabled,
                    lastUsedAtMs = nowMs,
                )
            }
        },
    ).sanitized()
}

internal fun browserTabDisplayTitle(tab: BrowserTabSnapshot): String = tab.title.ifBlank { browserTabHost(tab.url) }.ifBlank { "새 탭" }

internal fun browserTabDisplayUrl(tab: BrowserTabSnapshot): String = tab.url.ifBlank { "주소를 입력하세요" }

/** True when a distinct web target cannot be opened without closing a tab. */
internal fun browserTabLimitBlocks(store: BrowserTabStore, targetUrl: String): Boolean {
    val target = targetUrl.trim().takeIf(::canBookmarkUrl) ?: return false
    val clean = store.sanitized()
    return clean.tabs.size >= BROWSER_TAB_LIMIT && clean.tabs.none { it.url == target }
}

/**
 * Chooses durable tab metadata without allowing a renderer's transient
 * `about:blank`/`data:` URL to erase the last restorable web URL.
 */
internal fun stableBrowserTabUrl(currentUrl: String, requestedUrl: String, previousUrl: String): String = sequenceOf(currentUrl, previousUrl, requestedUrl)
    .map(String::trim)
    .firstOrNull(::canBookmarkUrl)
    .orEmpty()

internal fun stableBrowserPageTitle(reportedTitle: String?, reportedUrl: String, stableUrl: String, previousTitle: String): String {
    val next = reportedTitle.orEmpty().trim()
    if (next.isEmpty()) return previousTitle
    if (!canBookmarkUrl(reportedUrl) && canBookmarkUrl(stableUrl)) return previousTitle
    return next
}

/** Keeps a transient blob/data/about document from pairing its title with the
 * durable HTTP URL that will actually be restored after a restart. */
internal fun stableBrowserTabTitle(currentUrl: String, stableUrl: String, reportedTitle: String, previousTitle: String): String {
    val current = currentUrl.trim()
    val durable = stableUrl.trim()
    if (!canBookmarkUrl(current) || current != durable) return cleanBrowserTabTitle(previousTitle)
    return cleanBrowserTabTitle(reportedTitle).ifBlank { cleanBrowserTabTitle(previousTitle) }
}

private fun BrowserTabStore.ensureActive(nowMs: Long): BrowserTabStore {
    val active = tabs.firstOrNull { it.id == activeId } ?: tabs.first()
    return copy(
        activeId = active.id,
        tabs = tabs.map { if (it.id == active.id) it.copy(lastUsedAtMs = nowMs) else it },
    ).sanitized()
}

private fun BrowserTabStore.sanitized(): BrowserTabStore {
    val cleanTabs = tabs.asSequence()
        .mapNotNull { tab ->
            val id = tab.id.trim().take(BROWSER_TAB_ID_LIMIT)
            val url = tab.url.trim()
            if (!validBrowserTabId(id)) return@mapNotNull null
            tab.copy(
                id = id,
                // Never restore an untrusted/non-web scheme, but keep the tab
                // itself usable. Live pages can legitimately land on
                // about:blank; dropping the active tab here would leave the
                // browser with no active runtime.
                url = url.takeIf(::canBookmarkUrl).orEmpty(),
                title = cleanBrowserTabTitle(tab.title),
                lastUsedAtMs = tab.lastUsedAtMs.coerceAtLeast(0),
            )
        }
        .distinctBy { it.id }
        .take(BROWSER_TAB_LIMIT)
        .toList()
    val active = activeId.takeIf { id -> cleanTabs.any { it.id == id } } ?: cleanTabs.firstOrNull()?.id.orEmpty()
    return BrowserTabStore(activeId = active, tabs = cleanTabs)
}

private fun newBrowserTabSnapshot(
    tabs: List<BrowserTabSnapshot>,
    url: String,
    translateEnabled: Boolean,
    nowMs: Long,
): BrowserTabSnapshot {
    val used = tabs.mapTo(HashSet()) { it.id }
    val base = "tab-${nowMs.coerceAtLeast(0)}"
    var id = base
    var suffix = 1
    while (id in used) {
        id = "$base-$suffix"
        suffix++
    }
    return BrowserTabSnapshot(
        id = id,
        url = url.trim().takeIf(::canBookmarkUrl).orEmpty(),
        translateEnabled = translateEnabled,
        lastUsedAtMs = nowMs.coerceAtLeast(0),
    )
}

private fun validBrowserTabId(id: String): Boolean = id.isNotEmpty() && id.all { it.isLetterOrDigit() || it == '-' || it == '_' }

private fun cleanBrowserTabTitle(title: String): String {
    var cleaned = title.trim().replace(Regex("\\s+"), " ").take(BROWSER_TAB_TITLE_LIMIT)
    if (cleaned.lastOrNull()?.isHighSurrogate() == true) cleaned = cleaned.dropLast(1)
    return cleaned
}

private fun browserTabHost(url: String): String = url
    .substringAfter("://", url)
    .substringBefore('/')
    .substringBefore('?')
    .substringBefore('#')
    .let { if (it.startsWith("www.", ignoreCase = true)) it.drop(4) else it }

@OptIn(ExperimentalTime::class)
private fun browserTabNowMs(): Long = Clock.System.now().toEpochMilliseconds()
