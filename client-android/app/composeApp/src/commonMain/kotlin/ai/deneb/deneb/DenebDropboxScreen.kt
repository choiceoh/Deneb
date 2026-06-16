package ai.deneb.deneb

import ai.deneb.PlatformBackHandler
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebSearchField
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebPressable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.InsertDriveFile
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.CloudUpload
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.github.vinceglb.filekit.dialogs.FileKitType
import io.github.vinceglb.filekit.dialogs.compose.rememberFilePickerLauncher
import io.github.vinceglb.filekit.name
import io.github.vinceglb.filekit.readBytes
import kotlinx.coroutines.launch

/**
 * Native Dropbox file browser backed by `miniapp.dropbox.*`, in the Deneb idiom
 * (DenebScreenScaffold, full-width hairline rows, DenebType roles). Browse folders
 * (tap a folder to descend, system/back arrow to ascend), full-account search,
 * and per-file actions via a bottom sheet: 열기/공유 (a Dropbox shared link opened
 * in the browser) and AI 분석 (one agent turn whose result lands in chat). The
 * upload button picks a device file and stores it in the current folder. Controls
 * (search field, bottom sheet, pull refresh, buttons) stay Material; only the
 * presentation is Deneb.
 *
 * Not-connected is a first-class state: when the host has no Dropbox token the
 * screen shows a connect CTA pointing at Settings > 연동 rather than an error.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DenebDropboxScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onAnalyze: (String) -> Unit,
    onConnect: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()
    val uriHandler = LocalUriHandler.current

    // null = still checking the host; true/false = known link state.
    var connected by remember { mutableStateOf<Boolean?>(null) }
    // Folder path stack; the current folder is the last element ("" = account root).
    val pathStack = remember { mutableStateListOf("") }
    var entries by remember { mutableStateOf<List<DropboxEntry>>(emptyList()) }
    // null = loading, true = loaded, false = fetch failed (show retry).
    var loadOk by remember { mutableStateOf<Boolean?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    var searchText by remember { mutableStateOf("") }
    // The query the current list came from (null = browsing the folder, not searching).
    var activeQuery by remember { mutableStateOf<String?>(null) }
    var actionTarget by remember { mutableStateOf<DropboxEntry?>(null) }
    var uploading by remember { mutableStateOf(false) }
    var uploadError by remember { mutableStateOf<String?>(null) }
    // Monotonic load token: each navigation/search/refresh bumps it and captures
    // the value; a slower in-flight load that finds the token changed bails out
    // instead of overwriting a newer folder's contents (out-of-order RPC guard).
    var loadToken by remember { mutableStateOf(0) }

    suspend fun loadCurrent() {
        val token = ++loadToken
        loadOk = null
        val res = client.dropboxList(pathStack.last())
        if (token != loadToken) return // a newer load superseded this one
        entries = res ?: emptyList()
        loadOk = res != null
    }

    // Reload whatever view is current — the active search, else the folder. Retry
    // uses this: loadCurrent alone would re-list the folder behind a failed search.
    suspend fun reload() {
        val q = activeQuery
        if (q == null) {
            loadCurrent()
            return
        }
        val token = ++loadToken
        loadOk = null
        val res = client.dropboxSearch(q)
        if (token != loadToken) return
        entries = res ?: emptyList()
        loadOk = res != null
    }

    fun openFolder(e: DropboxEntry) {
        searchText = ""
        activeQuery = null
        // Push the display-cased path: Dropbox path_lower is lowercased, which would
        // render the folder title/breadcrumb (and upload dest) in all-lowercase for
        // mixed-case names. list_folder accepts the display path too.
        pathStack.add(e.pathDisplay.ifBlank { e.pathLower })
        scope.launch { loadCurrent() }
    }

    // Walk up one level (or out of a search) — returns false at the root so the
    // caller falls through to leaving the screen.
    fun goUp(): Boolean {
        if (activeQuery != null) {
            activeQuery = null
            searchText = ""
            scope.launch { loadCurrent() }
            return true
        }
        if (pathStack.size > 1) {
            pathStack.removeAt(pathStack.lastIndex)
            scope.launch { loadCurrent() }
            return true
        }
        return false
    }

    fun runSearch(raw: String) {
        val q = raw.trim().ifBlank { null }
        if (q == activeQuery) return
        activeQuery = q
        scope.launch { reload() }
    }

    LaunchedEffect(Unit) {
        connected = client.dropboxStatus()?.connected == true
        if (connected == true) loadCurrent()
    }

    // Android hardware back walks up the folder stack / out of search first.
    PlatformBackHandler(enabled = activeQuery != null || pathStack.size > 1) { goUp() }

    // Upload the picked device file into the current folder, then re-list. Hoisted
    // so the launcher is created once (harmless when not connected).
    val uploadLauncher = rememberFilePickerLauncher(type = FileKitType.File()) { file ->
        if (file == null) return@rememberFilePickerLauncher
        scope.launch {
            uploading = true
            uploadError = null
            val bytes = runCatching { file.readBytes() }.getOrNull()
            if (bytes == null) {
                uploadError = "파일을 읽지 못했습니다."
                uploading = false
                return@launch
            }
            val folder = pathStack.last().trimEnd('/')
            val dest = "$folder/${file.name}"
            val ok = client.dropboxUpload(dest, bytes) != null
            uploading = false
            if (ok) {
                loadCurrent()
            } else {
                uploadError = "업로드에 실패했습니다 (단일 파일 최대 150MB)."
            }
        }
    }

    val title = when {
        activeQuery != null -> "Dropbox 검색"
        pathStack.last().isBlank() -> "Dropbox"
        else -> pathStack.last().substringAfterLast('/').ifBlank { "Dropbox" }
    }

    DenebScreenScaffold(
        title = title,
        onBack = { if (!goUp()) onBack() },
        tabBar = navigationTabBar,
    ) {
        when (connected) {
            null -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { DenebLoading() }

            false -> DropboxNotConnected(onConnect)

            else -> {
                // Header: current path + upload action (upload only while browsing —
                // search results span folders, so "current folder" is undefined then).
                Row(
                    Modifier.fillMaxWidth().padding(start = 24.dp, end = 12.dp, bottom = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        pathStack.last().ifBlank { "/" },
                        style = DenebType.hint,
                        color = denebHint(),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                    if (activeQuery == null) {
                        if (uploading) {
                            CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                        } else {
                            TextButton(onClick = { uploadLauncher.launch() }) {
                                Icon(Icons.Outlined.CloudUpload, contentDescription = null, modifier = Modifier.size(18.dp))
                                Spacer(Modifier.width(6.dp))
                                Text("업로드")
                            }
                        }
                    }
                }
                uploadError?.let {
                    Text(
                        it,
                        style = DenebType.meta,
                        color = MaterialTheme.colorScheme.error,
                        modifier = Modifier.padding(horizontal = 24.dp, vertical = 2.dp),
                    )
                }

                DenebSearchField(
                    query = searchText,
                    onQueryChange = {
                        searchText = it
                        // Clearing the field returns to the current folder listing.
                        if (it.isBlank() && activeQuery != null) runSearch("")
                    },
                    placeholder = "Dropbox 파일 검색",
                    onSearch = { runSearch(searchText) },
                    clearContentDescription = "검색 지우기",
                    modifier = Modifier.padding(horizontal = 16.dp),
                )

                // "상위 폴더" affordance — phones hide the in-app ← (system back drives
                // it), so a visible up row keeps deep folders navigable by touch.
                if (activeQuery == null && pathStack.size > 1) {
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .denebPressable(onClick = {
                                haptics.tap()
                                goUp()
                            })
                            .padding(horizontal = 24.dp, vertical = 12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Icon(Icons.Outlined.Folder, contentDescription = null, tint = denebHint(), modifier = Modifier.size(22.dp))
                        Spacer(Modifier.width(14.dp))
                        Text("상위 폴더", style = DenebType.rowTitle, color = denebHint())
                    }
                    HorizontalDivider(color = denebHairline())
                }

                Box(Modifier.weight(1f).fillMaxWidth()) {
                    when {
                        entries.isEmpty() && loadOk == null ->
                            Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { DenebLoading() }

                        entries.isEmpty() && loadOk == false ->
                            Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                                DenebError(
                                    "Dropbox를 불러오지 못했습니다.",
                                    onRetry = { scope.launch { reload() } },
                                )
                            }

                        entries.isEmpty() ->
                            Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                                DenebEmpty(if (activeQuery != null) "검색 결과 없음" else "폴더가 비어 있습니다")
                            }

                        else -> PullToRefreshBox(
                            isRefreshing = refreshing,
                            onRefresh = {
                                scope.launch {
                                    refreshing = true
                                    val token = ++loadToken
                                    val q = activeQuery
                                    val res = if (q != null) client.dropboxSearch(q) else client.dropboxList(pathStack.last())
                                    // Drop a stale refresh result if the user navigated meanwhile.
                                    if (token == loadToken) res?.let { entries = it }
                                    refreshing = false
                                }
                            },
                            modifier = Modifier.fillMaxSize(),
                        ) {
                            DropboxListContent(
                                entries = entries,
                                onOpenFolder = {
                                    haptics.tap()
                                    openFolder(it)
                                },
                                onFileAction = {
                                    haptics.tap()
                                    actionTarget = it
                                },
                            )
                        }
                    }
                }
            }
        }
    }

    // Per-file action sheet. Material control; Deneb-styled rows inside.
    actionTarget?.let { target ->
        ModalBottomSheet(onDismissRequest = { actionTarget = null }) {
            DropboxActionSheetContent(
                entry = target,
                onOpen = {
                    actionTarget = null
                    scope.launch { client.dropboxShare(target.pathLower)?.let { uriHandler.openUri(it) } }
                },
                onAnalyze = {
                    actionTarget = null
                    onAnalyze(target.pathLower)
                },
            )
        }
    }
}

/**
 * The folder listing as a column of rows — the stateless, previewable core
 * ([DenebDropboxScreen] owns the data + states around it). Folders descend on
 * tap; files open the action sheet.
 */
@Composable
internal fun DropboxListContent(
    entries: List<DropboxEntry>,
    onOpenFolder: (DropboxEntry) -> Unit,
    onFileAction: (DropboxEntry) -> Unit,
    modifier: Modifier = Modifier,
) {
    LazyColumn(modifier.fillMaxSize()) {
        items(entries, key = { it.id.ifBlank { it.pathLower.ifBlank { it.pathDisplay } } }) { e ->
            DropboxRow(entry = e, onClick = { if (e.isFolder) onOpenFolder(e) else onFileAction(e) })
            HorizontalDivider(color = denebHairline())
        }
    }
}

/** One Dropbox row: type icon, name, and (files) a size · modified meta line. */
@Composable
internal fun DropboxRow(entry: DropboxEntry, onClick: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .denebPressable(onClick = onClick)
            .padding(horizontal = 24.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            imageVector = if (entry.isFolder) Icons.Outlined.Folder else Icons.AutoMirrored.Outlined.InsertDriveFile,
            contentDescription = if (entry.isFolder) "폴더" else "파일",
            tint = if (entry.isFolder) MaterialTheme.colorScheme.primary else denebHint(),
            modifier = Modifier.size(22.dp),
        )
        Spacer(Modifier.width(14.dp))
        Column(Modifier.weight(1f)) {
            Text(
                entry.name,
                style = DenebType.rowTitle,
                color = MaterialTheme.colorScheme.onBackground,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            val meta = dropboxRowMeta(entry)
            if (meta.isNotBlank()) {
                Spacer(Modifier.height(2.dp))
                Text(meta, style = DenebType.meta, color = denebHint(), maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

/** Bottom-sheet actions for a single file: open the shared link, or AI-analyze it. */
@Composable
internal fun DropboxActionSheetContent(
    entry: DropboxEntry,
    onOpen: () -> Unit,
    onAnalyze: () -> Unit,
) {
    Column(Modifier.fillMaxWidth().padding(bottom = 24.dp)) {
        Text(
            entry.name,
            style = DenebType.subject,
            color = MaterialTheme.colorScheme.onBackground,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.padding(horizontal = 24.dp, vertical = 12.dp),
        )
        HorizontalDivider(color = denebHairline())
        DropboxSheetAction(Icons.Outlined.Link, "열기 / 공유 링크", onOpen)
        DropboxSheetAction(Icons.Outlined.AutoAwesome, "AI로 분석", onAnalyze)
    }
}

@Composable
private fun DropboxSheetAction(icon: androidx.compose.ui.graphics.vector.ImageVector, label: String, onClick: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .denebPressable(onClick = onClick)
            .padding(horizontal = 24.dp, vertical = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(icon, contentDescription = null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(22.dp))
        Spacer(Modifier.width(16.dp))
        Text(label, style = DenebType.rowTitle, color = MaterialTheme.colorScheme.onBackground)
    }
}

/** The not-connected CTA — points the user at the Settings > 연동 connect wizard. */
@Composable
internal fun DropboxNotConnected(onConnect: () -> Unit) {
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        DenebEmpty(
            "Dropbox가 연결되어 있지 않습니다.\n설정 > 연동에서 계정을 연결하세요.",
            actionLabel = "연동하기",
            onAction = onConnect,
        )
    }
}

/** Size · modified meta for a file row; folders show none. */
private fun dropboxRowMeta(e: DropboxEntry): String {
    if (e.isFolder) return ""
    val size = humanBytes(e.size)
    val date = e.modified.takeIf { it.isNotBlank() }?.let { shortDate(it) }
    return if (date != null) "$size · $date" else size
}
