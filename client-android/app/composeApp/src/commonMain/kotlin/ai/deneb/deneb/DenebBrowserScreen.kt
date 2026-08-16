package ai.deneb.deneb

import ai.deneb.PlatformBackHandler
import ai.deneb.data.AppSettings
import ai.deneb.openUrl
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import ai.deneb.ui.icons.automirrored.outlined.OpenInNew
import ai.deneb.ui.icons.filled.Bookmark
import ai.deneb.ui.icons.outlined.Block
import ai.deneb.ui.icons.outlined.BookmarkBorder
import ai.deneb.ui.icons.outlined.ContentCopy
import ai.deneb.ui.icons.outlined.History
import ai.deneb.ui.icons.outlined.Translate
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.ime
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.union
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowForward
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Home
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer

/**
 * In-app browser for external links, with one-tap in-place DeepL translation to
 * Korean. Android renders a real WebView; other platforms show an Android-only
 * stub (the chrome and navigation still render, so the desktop harness /
 * renderPreviews can exercise them).
 */
@Composable
fun DenebBrowserScreen(
    url: String,
    client: DenebGatewayClient,
    appSettings: AppSettings,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    // Durable metadata restores up to five tab URLs after an app restart. The
    // Android actual additionally parks a WebView Bundle in each runtime while
    // switching, so back/forward history and scroll survive without retaining
    // five renderer processes at once.
    val initialStore = remember {
        resolveBrowserTabStore(
            stored = decodeBrowserTabStore(appSettings.getBrowserTabsJson()),
            explicitUrl = url,
            fallbackUrl = resolveBrowserStartUrl(
                navUrl = "",
                lastUrl = appSettings.getBrowserLastUrl(),
                homeUrl = appSettings.getBrowserHomeUrl(),
            ),
            translateDefault = appSettings.isBrowserTranslateEnabled(),
        )
    }
    val tabRuntimes = remember {
        mutableStateMapOf<String, BrowserTabRuntime>().apply {
            initialStore.tabs.forEach { snapshot ->
                put(snapshot.id, snapshot.toRuntime(appSettings.isBrowserAdBlockEnabled()))
            }
        }
    }
    var tabStore by remember { mutableStateOf(initialStore) }

    fun currentStore(): BrowserTabStore = BrowserTabStore(
        activeId = tabStore.activeId,
        tabs = tabStore.tabs.map { snapshot -> tabRuntimes[snapshot.id]?.snapshot() ?: snapshot },
    )

    fun persistStore(next: BrowserTabStore) {
        val keepIds = next.tabs.mapTo(HashSet()) { it.id }
        tabRuntimes.keys.toList().filterNot { it in keepIds }.forEach(tabRuntimes::remove)
        next.tabs.forEach { snapshot ->
            val runtime = tabRuntimes[snapshot.id]
            if (runtime == null) {
                tabRuntimes[snapshot.id] = snapshot.toRuntime(appSettings.isBrowserAdBlockEnabled())
            } else {
                runtime.lastUsedAtMs = snapshot.lastUsedAtMs
            }
        }
        tabStore = next
        appSettings.setBrowserTabsJson(encodeBrowserTabStore(next))
    }

    fun switchStore(next: BrowserTabStore) {
        if (next.activeId != tabStore.activeId) {
            tabRuntimes[tabStore.activeId]?.state?.let { oldState ->
                oldState.jsDialog?.cancel()
                oldState.jsDialog = null
            }
        }
        persistStore(next)
    }

    val activeRuntime = tabRuntimes[tabStore.activeId] ?: error("browser active tab missing")
    val state = activeRuntime.state

    LaunchedEffect(url) {
        // A navigation route can change while this screen stays composed. Resolve
        // it into the existing tab set so background WebView Bundles survive.
        switchStore(
            resolveBrowserTabStore(
                stored = currentStore(),
                explicitUrl = url,
                fallbackUrl = "",
                translateDefault = appSettings.isBrowserTranslateEnabled(),
            ),
        )
    }
    LaunchedEffect(state.translateEnabled) {
        if (state.translateEnabled != appSettings.isBrowserTranslateEnabled()) {
            appSettings.setBrowserTranslateEnabled(state.translateEnabled)
        }
    }
    LaunchedEffect(state.adBlockEnabled) {
        if (state.adBlockEnabled != appSettings.isBrowserAdBlockEnabled()) {
            appSettings.setBrowserAdBlockEnabled(state.adBlockEnabled)
        }
        tabRuntimes.values.forEach { runtime ->
            if (runtime.state.adBlockEnabled != state.adBlockEnabled) {
                runtime.state.adBlockEnabled = state.adBlockEnabled
            }
        }
    }
    LaunchedEffect(tabStore.activeId, state.currentUrl, state.pageTitle, state.translateEnabled) {
        val next = updateBrowserTab(
            store = currentStore(),
            id = tabStore.activeId,
            url = state.currentUrl.ifBlank { state.url },
            title = state.pageTitle,
            translateEnabled = state.translateEnabled,
        )
        persistStore(next)
    }
    // Disk-back the translate cache (encrypted cached-section store) so recent
    // pages' translations survive an app restart. Constant owner: translations
    // of public web pages are browsing data, not account data — they stay valid
    // across a credential switch.
    remember {
        val slot = SectionDiskSlot(
            appSettings,
            "browser-translate",
            MapSerializer(String.serializer(), String.serializer()),
        ) { "browser" }
        browserTranslateCache.attachPersistence(load = { slot.load() }, save = { slot.save(it) })
    }
    var showBookmarks by remember { mutableStateOf(false) }
    var showHistory by remember { mutableStateOf(false) }
    var showTabs by remember { mutableStateOf(false) }
    var homeUrl by remember { mutableStateOf(appSettings.getBrowserHomeUrl()) }
    var bookmarks by remember { mutableStateOf(decodeBrowserBookmarks(appSettings.getBrowserBookmarksJson())) }
    var history by remember { mutableStateOf(decodeBrowserHistory(appSettings.getBrowserHistoryJson())) }
    fun persistBookmarks(next: List<BrowserBookmark>) {
        val json = encodeBrowserBookmarks(next)
        appSettings.setBrowserBookmarksJson(json)
        bookmarks = decodeBrowserBookmarks(json)
    }
    fun persistHistory(next: List<BrowserVisit>) {
        val json = encodeBrowserHistory(next)
        appSettings.setBrowserHistoryJson(json)
        history = decodeBrowserHistory(json)
    }
    LaunchedEffect(state.currentUrl, state.pageTitle) {
        val current = state.currentUrl.trim()
        if (!canBookmarkUrl(current)) return@LaunchedEffect
        if (current != appSettings.getBrowserLastUrl()) {
            appSettings.setBrowserLastUrl(current)
        }
        val next = recordBrowserVisit(history, current, state.pageTitle)
        if (next != history) persistHistory(next)
    }
    val bookmarkUrl = state.currentUrl.ifBlank { state.url }
    val bookmarkable = canBookmarkUrl(bookmarkUrl)
    val bookmarked = isBrowserBookmarked(bookmarks, bookmarkUrl)
    DenebBrowserChrome(
        state = state,
        onBack = onBack,
        modifier = modifier,
        bookmarksCount = bookmarks.size,
        historyCount = history.size,
        tabCount = tabStore.tabs.size,
        homeUrl = homeUrl,
        isBookmarked = bookmarked,
        canBookmark = bookmarkable,
        onToggleBookmark = {
            persistBookmarks(toggleBrowserBookmark(bookmarks, bookmarkUrl, state.pageTitle))
        },
        onShowBookmarks = { showBookmarks = true },
        onShowHistory = { showHistory = true },
        onShowTabs = { showTabs = true },
        onGoHome = {
            val target = homeUrl.trim()
            if (canBookmarkUrl(target)) state.load(target)
        },
        onSetHome = {
            if (canBookmarkUrl(bookmarkUrl)) {
                appSettings.setBrowserHomeUrl(bookmarkUrl)
                homeUrl = appSettings.getBrowserHomeUrl()
            }
        },
        onClearHome = {
            appSettings.setBrowserHomeUrl("")
            homeUrl = ""
        },
    ) {
        if (browserShowsStart(state.url, state.currentUrl)) {
            BrowserStartPane(
                bookmarks = bookmarks,
                visits = history,
                onOpen = { state.load(it) },
                modifier = Modifier.fillMaxWidth().weight(1f),
            )
        } else {
            key(activeRuntime.id, state.rendererGeneration) {
                DenebWebView(
                    state = state,
                    // Recently seen segments (back-nav, reload, shared site chrome) apply
                    // instantly from the LRU cache; only misses round-trip to DeepL.
                    translate = { segments, lang ->
                        browserTranslateCache.translate(segments, lang) { miss, missLang ->
                            client.translateSegments(miss, missLang)
                        }
                    },
                    modifier = Modifier.fillMaxWidth().weight(1f),
                    onOpenNewTab = { target ->
                        switchStore(
                            resolveBrowserTabStore(
                                stored = currentStore(),
                                explicitUrl = target,
                                fallbackUrl = "",
                                translateDefault = appSettings.isBrowserTranslateEnabled(),
                            ),
                        )
                    },
                )
            }
        }
    }
    var diagnosticsCopied by remember { mutableStateOf(false) }
    val screenClipboard = LocalClipboardManager.current
    // The probe result lands asynchronously from the WebView; copy it out and say
    // so, since the whole point is to paste it somewhere it can be read.
    LaunchedEffect(state.diagnostics) {
        state.diagnostics?.let {
            screenClipboard.setText(AnnotatedString(it))
            state.diagnostics = null
            diagnosticsCopied = true
        }
    }
    if (diagnosticsCopied) {
        AlertDialog(
            onDismissRequest = { diagnosticsCopied = false },
            title = { Text("스크롤 진단") },
            text = { Text("결과를 클립보드에 복사했습니다. 데네브 대화에 붙여넣어 주세요.") },
            confirmButton = { TextButton(onClick = { diagnosticsCopied = false }) { Text("확인") } },
        )
    }

    // alert()/confirm()/prompt() from the page. The page's JS thread is blocked
    // until answered, and BrowserJsDialog.answer is idempotent, so a dismiss that
    // races a button tap still delivers exactly one result.
    state.jsDialog?.let { dialog ->
        var promptValue by remember(dialog) { mutableStateOf(dialog.defaultValue) }
        val finish: (Boolean, String?) -> Unit = { ok, value ->
            dialog.answer(ok, value)
            state.jsDialog = null
        }
        AlertDialog(
            onDismissRequest = { finish(false, null) },
            title = { Text(state.currentUrl.ifBlank { "페이지" }, style = MaterialTheme.typography.labelMedium) },
            text = {
                Column {
                    Text(dialog.message)
                    if (dialog.kind == BrowserJsDialog.Kind.PROMPT) {
                        OutlinedTextField(
                            value = promptValue,
                            onValueChange = { promptValue = it },
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
                        )
                    }
                }
            },
            confirmButton = {
                TextButton(onClick = {
                    finish(true, if (dialog.kind == BrowserJsDialog.Kind.PROMPT) promptValue else null)
                }) { Text("확인") }
            },
            dismissButton = if (dialog.kind == BrowserJsDialog.Kind.ALERT) {
                null
            } else {
                { TextButton(onClick = { finish(false, null) }) { Text("취소") } }
            },
        )
    }
    if (showBookmarks) {
        BrowserBookmarksSheet(
            bookmarks = bookmarks,
            onOpen = { bookmark ->
                state.load(bookmark.url)
                showBookmarks = false
            },
            onDelete = { bookmark -> persistBookmarks(removeBrowserBookmark(bookmarks, bookmark.url)) },
            onDismiss = { showBookmarks = false },
        )
    }
    if (showHistory) {
        BrowserHistorySheet(
            visits = history,
            onOpen = { visit ->
                state.load(visit.url)
                showHistory = false
            },
            onDelete = { visit -> persistHistory(removeBrowserVisit(history, visit.url)) },
            onDismiss = { showHistory = false },
        )
    }
    if (showTabs) {
        BrowserTabsSheet(
            store = currentStore(),
            onSelect = { id ->
                switchStore(selectBrowserTab(currentStore(), id))
                showTabs = false
            },
            onClose = { id ->
                switchStore(
                    closeBrowserTab(
                        store = currentStore(),
                        id = id,
                        translateDefault = appSettings.isBrowserTranslateEnabled(),
                    ),
                )
            },
            onAdd = {
                switchStore(
                    addBrowserTab(
                        store = currentStore(),
                        translateDefault = appSettings.isBrowserTranslateEnabled(),
                    ),
                )
                showTabs = false
            },
            onDismiss = { showTabs = false },
        )
    }
}

private class BrowserTabRuntime(
    val id: String,
    val state: DenebWebViewState,
    var lastUsedAtMs: Long,
) {
    fun snapshot(): BrowserTabSnapshot = BrowserTabSnapshot(
        id = id,
        url = state.currentUrl.ifBlank { state.url },
        title = state.pageTitle,
        translateEnabled = state.translateEnabled,
        lastUsedAtMs = lastUsedAtMs,
    )
}

private fun BrowserTabSnapshot.toRuntime(adBlockEnabled: Boolean): BrowserTabRuntime = BrowserTabRuntime(
    id = id,
    state = DenebWebViewState(
        initialUrl = url,
        translateEnabled = translateEnabled,
        adBlockEnabled = adBlockEnabled,
    ),
    lastUsedAtMs = lastUsedAtMs,
)

/**
 * Stateless browser chrome, separated from the stateful shell so renderPreviews can
 * exercise the look with mock state. Safari-style: NO top header — the page fills the
 * screen and all chrome (editable address bar + actions) sits at the BOTTOM, above the
 * system nav bar. The address bar shows the real current URL (link safety) and is
 * editable (type/paste + Go); the close button exits immediately, while system
 * back pops page history then exits. The open-external action escapes to the
 * system browser.
 *
 * Design system: Material IconButtons with functional icons, skinned with Deneb colors
 * — the translate toggle lights the warm insight accent when ON (DeepL-backed
 * translation), inactive actions use the muted hint color. [content] is the page
 * area (the real WebView, or a stub) and takes the weight, so it fills above the
 * bottom bar.
 */
@Composable
fun DenebBrowserChrome(
    state: DenebWebViewState,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
    bookmarksCount: Int = 0,
    historyCount: Int = 0,
    tabCount: Int = 1,
    homeUrl: String = "",
    isBookmarked: Boolean = false,
    canBookmark: Boolean = false,
    onToggleBookmark: (() -> Unit)? = null,
    onShowBookmarks: (() -> Unit)? = null,
    onShowHistory: (() -> Unit)? = null,
    onShowTabs: (() -> Unit)? = null,
    onGoHome: (() -> Unit)? = null,
    onSetHome: (() -> Unit)? = null,
    onClearHome: (() -> Unit)? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    val haptics = rememberHaptics()
    val focusManager = LocalFocusManager.current

    // No on-screen back button — system back owns "back": page history first, then exit.
    PlatformBackHandler(enabled = true) { if (state.canGoBack) state.goBack() else onBack() }

    // Editable address bar value, re-synced to the real URL as the page navigates.
    var field by remember(state.currentUrl) { mutableStateOf(state.currentUrl) }
    var menuOpen by remember { mutableStateOf(false) }
    val clipboard = LocalClipboardManager.current

    Surface(color = MaterialTheme.colorScheme.background, modifier = modifier.fillMaxSize()) {
        Column(Modifier.fillMaxSize().statusBarsPadding()) {
            // Page fills the top — no top header.
            content()
            if (state.loading) {
                LinearProgressIndicator(modifier = Modifier.fillMaxWidth(), progress = { state.progress / 100f })
            }
            // A failed load leaves the WebView blank; say why instead of showing
            // an empty page with no explanation.
            state.loadError?.let { message ->
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .background(MaterialTheme.colorScheme.errorContainer)
                        .padding(horizontal = 12.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        text = message,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onErrorContainer,
                        modifier = Modifier.weight(1f),
                    )
                    TextButton(onClick = state::retry) { Text("재시도") }
                }
            }
            HorizontalDivider(color = denebHairline())
            // Bottom chrome (Safari-style): close · forward · editable address bar
            // (omnibox) · reload-or-stop · translate · overflow (⋮), above the
            // system gesture bar. Secondary actions (copy, open-external, fallback)
            // live in the ⋮ menu so the row scales as features grow.
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    // Float above whichever is present — the soft keyboard (when the address
                    // bar is focused) or the system nav bar. Without the ime inset the keyboard
                    // covered this bottom chrome (address bar + actions).
                    .windowInsetsPadding(WindowInsets.ime.union(WindowInsets.navigationBars))
                    .padding(horizontal = 4.dp, vertical = 2.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                IconButton(
                    onClick = {
                        haptics.tap()
                        onBack()
                    },
                    modifier = Modifier.size(40.dp),
                ) {
                    Icon(Icons.Outlined.Close, contentDescription = "브라우저 닫기", tint = denebHint())
                }
                IconButton(
                    onClick = {
                        haptics.tap()
                        state.goForward()
                    },
                    enabled = state.canGoForward,
                    modifier = Modifier.size(40.dp),
                ) {
                    Icon(
                        Icons.AutoMirrored.Outlined.ArrowForward,
                        contentDescription = "앞으로",
                        tint = if (state.canGoForward) denebHint() else denebHint().copy(alpha = 0.3f),
                    )
                }
                BasicTextField(
                    value = field,
                    onValueChange = { field = it },
                    singleLine = true,
                    textStyle = DenebType.meta.copy(color = MaterialTheme.colorScheme.onSurface),
                    cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Go),
                    keyboardActions = KeyboardActions(
                        onGo = {
                            val target = normalizeUrl(field)
                            if (target.isNotEmpty()) {
                                state.load(target)
                                field = target
                            }
                            focusManager.clearFocus()
                        },
                    ),
                    modifier = Modifier.weight(1f).padding(horizontal = 4.dp),
                    decorationBox = { innerTextField ->
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Box(Modifier.weight(1f)) {
                                if (field.isEmpty()) {
                                    Text("주소 또는 검색", style = DenebType.meta, color = denebHint(), maxLines = 1)
                                }
                                innerTextField()
                            }
                            if (field.isNotEmpty()) {
                                IconButton(
                                    onClick = {
                                        haptics.tap()
                                        field = ""
                                    },
                                    modifier = Modifier.size(28.dp),
                                ) {
                                    Icon(
                                        Icons.Outlined.Close,
                                        contentDescription = "주소 지우기",
                                        tint = denebHint(),
                                        modifier = Modifier.size(18.dp),
                                    )
                                }
                            }
                        }
                    },
                )
                IconButton(
                    onClick = {
                        haptics.tap()
                        if (state.loading) state.stop() else state.reload()
                    },
                    modifier = Modifier.size(40.dp),
                ) {
                    Icon(
                        if (state.loading) Icons.Outlined.Close else Icons.Outlined.Refresh,
                        contentDescription = if (state.loading) "정지" else "새로고침",
                        tint = denebHint(),
                    )
                }
                IconButton(
                    onClick = {
                        haptics.tap()
                        state.translateEnabled = !state.translateEnabled
                    },
                    modifier = Modifier.size(40.dp),
                ) {
                    Icon(
                        Icons.Outlined.Translate,
                        contentDescription = if (state.translateEnabled) "원문 보기" else "DeepL로 한국어 번역",
                        tint = if (state.translateEnabled) denebInsight() else denebHint(),
                    )
                }
                // Frequency-ordered (operator feedback): the persistent bar hosts the
                // FREQUENT action — opening the bookmark list — while the rare
                // add/remove toggle lives in the More menu. The filled/insight tint
                // still signals "this page is bookmarked" at a glance.
                IconButton(
                    onClick = {
                        haptics.tap()
                        onShowBookmarks?.invoke()
                    },
                    enabled = onShowBookmarks != null,
                    modifier = Modifier.size(40.dp),
                ) {
                    Icon(
                        if (isBookmarked) Icons.Filled.Bookmark else Icons.Outlined.BookmarkBorder,
                        contentDescription = "북마크 목록",
                        tint = if (isBookmarked) denebInsight() else denebHint(),
                    )
                }
                IconButton(
                    onClick = {
                        haptics.tap()
                        onShowTabs?.invoke()
                    },
                    enabled = onShowTabs != null,
                    modifier = Modifier.size(40.dp),
                ) {
                    BadgedBox(
                        badge = {
                            Badge(containerColor = MaterialTheme.colorScheme.primary) {
                                Text(tabCount.toString())
                            }
                        },
                    ) {
                        Icon(
                            Icons.Outlined.ContentCopy,
                            contentDescription = "탭 ${tabCount}개",
                            tint = denebHint(),
                            modifier = Modifier.size(22.dp),
                        )
                    }
                }
                Box {
                    IconButton(
                        onClick = {
                            haptics.tap()
                            menuOpen = true
                        },
                        modifier = Modifier.size(40.dp),
                    ) {
                        Icon(Icons.Outlined.MoreVert, contentDescription = "더보기", tint = denebHint())
                    }
                    DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                        DropdownMenuItem(
                            enabled = false,
                            text = {
                                Column {
                                    Text("DeepL 번역")
                                    Text("기본 엔진", style = DenebType.meta, color = denebHint())
                                }
                            },
                            leadingIcon = { Icon(Icons.Outlined.Translate, contentDescription = null, tint = denebInsight()) },
                            onClick = {},
                        )
                        DropdownMenuItem(
                            text = {
                                Column {
                                    Text(if (state.adBlockEnabled) "광고 차단 끄기" else "광고 차단 켜기")
                                    Text(
                                        when {
                                            !state.adBlockEnabled -> "차단 꺼짐"
                                            state.adBlockedCount > 0 -> "이 페이지 ${state.adBlockedCount}건 차단"
                                            else -> "알려진 광고·추적 요청 차단 중"
                                        },
                                        style = DenebType.meta,
                                        color = denebHint(),
                                    )
                                }
                            },
                            leadingIcon = {
                                Icon(
                                    Icons.Outlined.Block,
                                    contentDescription = null,
                                    tint = if (state.adBlockEnabled) denebInsight() else denebHint(),
                                )
                            },
                            onClick = {
                                haptics.tap()
                                state.adBlockEnabled = !state.adBlockEnabled
                                state.reload()
                                menuOpen = false
                            },
                        )
                        if (onToggleBookmark != null) {
                            DropdownMenuItem(
                                enabled = canBookmark,
                                text = {
                                    Column {
                                        Text(if (isBookmarked) "북마크 삭제" else "북마크 추가")
                                        Text(
                                            when {
                                                isBookmarked -> "현재 페이지 저장됨 · 전체 ${bookmarksCount}개"
                                                bookmarksCount > 0 -> "전체 ${bookmarksCount}개 저장됨"
                                                else -> "현재 페이지를 저장"
                                            },
                                            style = DenebType.meta,
                                            color = denebHint(),
                                        )
                                    }
                                },
                                leadingIcon = {
                                    Icon(
                                        if (isBookmarked) Icons.Filled.Bookmark else Icons.Outlined.BookmarkBorder,
                                        contentDescription = null,
                                        tint = if (isBookmarked) denebInsight() else denebHint(),
                                    )
                                },
                                onClick = {
                                    haptics.tap()
                                    onToggleBookmark()
                                    menuOpen = false
                                },
                            )
                        }
                        if (onShowHistory != null) {
                            DropdownMenuItem(
                                text = {
                                    Column {
                                        Text("최근 방문")
                                        Text(
                                            if (historyCount > 0) "${historyCount}개" else "방문 기록 없음",
                                            style = DenebType.meta,
                                            color = denebHint(),
                                        )
                                    }
                                },
                                leadingIcon = { Icon(Icons.Outlined.History, contentDescription = null, tint = denebHint()) },
                                onClick = {
                                    haptics.tap()
                                    menuOpen = false
                                    onShowHistory()
                                },
                            )
                        }
                        val hasHome = canBookmarkUrl(homeUrl)
                        if (onGoHome != null) {
                            DropdownMenuItem(
                                enabled = hasHome,
                                text = {
                                    Column {
                                        Text("홈으로")
                                        Text(
                                            if (hasHome) homeUrl else "설정된 홈 없음",
                                            style = DenebType.meta,
                                            color = denebHint(),
                                            maxLines = 1,
                                            overflow = TextOverflow.Ellipsis,
                                        )
                                    }
                                },
                                leadingIcon = { Icon(Icons.Outlined.Home, contentDescription = null, tint = denebHint()) },
                                onClick = {
                                    haptics.tap()
                                    menuOpen = false
                                    onGoHome()
                                },
                            )
                        }
                        if (onSetHome != null) {
                            DropdownMenuItem(
                                enabled = canBookmark,
                                text = { Text("현재 페이지를 홈으로") },
                                leadingIcon = { Icon(Icons.Outlined.Home, contentDescription = null, tint = denebInsight()) },
                                onClick = {
                                    haptics.tap()
                                    menuOpen = false
                                    onSetHome()
                                },
                            )
                        }
                        if (onClearHome != null && hasHome) {
                            DropdownMenuItem(
                                text = { Text("홈 지우기") },
                                leadingIcon = { Icon(Icons.Outlined.Delete, contentDescription = null, tint = denebHint()) },
                                onClick = {
                                    haptics.tap()
                                    menuOpen = false
                                    onClearHome()
                                },
                            )
                        }
                        DropdownMenuItem(
                            text = { Text("URL 복사") },
                            leadingIcon = { Icon(Icons.Outlined.ContentCopy, contentDescription = null, tint = denebHint()) },
                            onClick = {
                                haptics.tap()
                                clipboard.setText(AnnotatedString(state.currentUrl))
                                menuOpen = false
                            },
                        )
                        DropdownMenuItem(
                            text = { Text("스크롤 진단 복사") },
                            leadingIcon = { Icon(Icons.Outlined.ContentCopy, contentDescription = null, tint = denebHint()) },
                            onClick = {
                                haptics.tap()
                                state.runDiagnostics()
                                menuOpen = false
                            },
                        )
                        DropdownMenuItem(
                            text = { Text("외부 브라우저로 열기") },
                            leadingIcon = { Icon(Icons.AutoMirrored.Outlined.OpenInNew, contentDescription = null, tint = denebHint()) },
                            onClick = {
                                haptics.tap()
                                openUrl(state.currentUrl)
                                menuOpen = false
                            },
                        )
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun BrowserBookmarksSheet(
    bookmarks: List<BrowserBookmark>,
    onOpen: (BrowserBookmark) -> Unit,
    onDelete: (BrowserBookmark) -> Unit,
    onDismiss: () -> Unit,
) {
    val haptics = rememberHaptics()
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier
                .fillMaxWidth()
                .padding(horizontal = 24.dp)
                .padding(bottom = 24.dp),
        ) {
            Text("북마크", style = DenebType.subject, color = MaterialTheme.colorScheme.onSurface)
            Spacer(Modifier.height(2.dp))
            Text(
                if (bookmarks.isEmpty()) "저장한 페이지가 없습니다" else "저장한 페이지 ${bookmarks.size}개",
                style = DenebType.meta,
                color = denebHint(),
            )
            Spacer(Modifier.height(12.dp))
            if (bookmarks.isEmpty()) {
                Text(
                    "아직 비어 있습니다.",
                    style = DenebType.body,
                    color = denebHint(),
                )
            } else {
                LazyColumn(Modifier.fillMaxWidth()) {
                    items(bookmarks, key = { it.url }) { bookmark ->
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clickable {
                                    haptics.tap()
                                    onOpen(bookmark)
                                }
                                .padding(vertical = 10.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Icon(
                                Icons.Filled.Bookmark,
                                contentDescription = null,
                                tint = denebInsight(),
                                modifier = Modifier.size(22.dp),
                            )
                            Column(Modifier.weight(1f).padding(start = 12.dp)) {
                                Text(
                                    browserBookmarkDisplayTitle(bookmark),
                                    style = DenebType.rowTitle,
                                    color = MaterialTheme.colorScheme.onSurface,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                                Text(
                                    bookmark.url,
                                    style = DenebType.meta,
                                    color = denebHint(),
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                            IconButton(
                                onClick = {
                                    haptics.tap()
                                    onDelete(bookmark)
                                },
                                modifier = Modifier.size(40.dp),
                            ) {
                                Icon(Icons.Outlined.Delete, contentDescription = "북마크 삭제", tint = denebHint())
                            }
                        }
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun BrowserHistorySheet(
    visits: List<BrowserVisit>,
    onOpen: (BrowserVisit) -> Unit,
    onDelete: (BrowserVisit) -> Unit,
    onDismiss: () -> Unit,
) {
    val haptics = rememberHaptics()
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier
                .fillMaxWidth()
                .padding(horizontal = 24.dp)
                .padding(bottom = 24.dp),
        ) {
            Text("최근 방문", style = DenebType.subject, color = MaterialTheme.colorScheme.onSurface)
            Spacer(Modifier.height(2.dp))
            Text(
                if (visits.isEmpty()) "최근 방문이 없습니다" else "최근 ${visits.size}개",
                style = DenebType.meta,
                color = denebHint(),
            )
            Spacer(Modifier.height(12.dp))
            if (visits.isEmpty()) {
                Text(
                    "페이지를 열면 여기에 쌓입니다.",
                    style = DenebType.body,
                    color = denebHint(),
                )
            } else {
                LazyColumn(Modifier.fillMaxWidth()) {
                    items(visits, key = { it.url }) { visit ->
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clickable {
                                    haptics.tap()
                                    onOpen(visit)
                                }
                                .padding(vertical = 10.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Icon(
                                Icons.Outlined.History,
                                contentDescription = null,
                                tint = denebHint(),
                                modifier = Modifier.size(22.dp),
                            )
                            Column(Modifier.weight(1f).padding(start = 12.dp)) {
                                Text(
                                    browserVisitDisplayTitle(visit),
                                    style = DenebType.rowTitle,
                                    color = MaterialTheme.colorScheme.onSurface,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                                Text(
                                    visit.url,
                                    style = DenebType.meta,
                                    color = denebHint(),
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                            IconButton(
                                onClick = {
                                    haptics.tap()
                                    onDelete(visit)
                                },
                                modifier = Modifier.size(40.dp),
                            ) {
                                Icon(Icons.Outlined.Delete, contentDescription = "기록 삭제", tint = denebHint())
                            }
                        }
                    }
                }
            }
        }
    }
}

/**
 * Omnibox: turns address-bar input into a loadable URL. Explicit http(s) → as-is; a
 * bare host (no spaces, has a dot) → assume https; anything else → a web search. Empty
 * stays empty (the blank "new tab" state, which the WebView's blank-guard leaves unloaded).
 */
private fun normalizeUrl(input: String): String {
    val s = input.trim()
    if (s.isEmpty()) return ""
    if (s.startsWith("http://", ignoreCase = true) || s.startsWith("https://", ignoreCase = true)) return s
    return if (!s.contains(' ') && s.contains('.')) {
        "https://$s"
    } else {
        "https://www.google.com/search?q=${encodeQuery(s)}"
    }
}

/** Percent-encodes a search query (UTF-8); space → '+'. KMP-safe (no java URLEncoder). */
private fun encodeQuery(s: String): String {
    val out = StringBuilder()
    for (b in s.encodeToByteArray()) {
        val v = b.toInt() and 0xFF
        val ch = v.toChar()
        when {
            ch in 'A'..'Z' || ch in 'a'..'z' || ch in '0'..'9' ||
                ch == '-' || ch == '_' || ch == '.' || ch == '~' -> out.append(ch)

            v == 0x20 -> out.append('+')

            else -> {
                out.append('%')
                out.append(((v shr 4) and 0xF).toString(16).uppercase())
                out.append((v and 0xF).toString(16).uppercase())
            }
        }
    }
    return out.toString()
}
