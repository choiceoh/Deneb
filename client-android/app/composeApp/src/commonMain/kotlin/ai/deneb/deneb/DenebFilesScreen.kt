package ai.deneb.deneb

import ai.deneb.PlatformBackHandler
import ai.deneb.openUrl
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebUnderlineSearchField
import ai.deneb.ui.components.LocalShowFullScreenImageModel
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebPressable
import ai.deneb.ui.icons.outlined.CloudUpload
import ai.deneb.ui.icons.outlined.CreateNewFolder
import ai.deneb.ui.icons.outlined.Folder
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material3.AlertDialog
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
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.github.vinceglb.filekit.dialogs.FileKitType
import io.github.vinceglb.filekit.dialogs.compose.rememberFilePickerLauncher
import io.github.vinceglb.filekit.name
import io.github.vinceglb.filekit.readBytes
import kotlinx.coroutines.launch

/**
 * Native local file browser backed by `miniapp.files.*`, in the Deneb idiom
 * (DenebScreenScaffold, full-width hairline rows, DenebType roles), over the
 * gateway's local file store. Browse folders (tap a folder to descend, system/back
 * arrow to ascend), full-store search, upload a device file into the current folder,
 * and a per-file action sheet that opens a signed download link. Controls (search
 * field, bottom sheet, pull refresh, buttons) stay Material; only the presentation
 * is Deneb.
 *
 * There is no OAuth/connect wizard — the store is always available, so the screen
 * shows the browser straight away (failures surface as retry/empty). "AI 분석" is
 * also absent: the local store has no analyze chat-bridge RPC.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DenebFilesScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()
    val showFullScreenImage = LocalShowFullScreenImageModel.current

    // Folder path stack; the current folder is the last element ("" = store root).
    val pathStack = remember { mutableStateListOf("") }
    var entries by remember { mutableStateOf<List<FilesEntry>>(emptyList()) }
    // null = loading, true = loaded, false = fetch failed (show retry).
    var loadOk by remember { mutableStateOf<Boolean?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    var searchText by remember { mutableStateOf("") }
    // The query the current list came from (null = browsing the folder, not searching).
    var activeQuery by remember { mutableStateOf<String?>(null) }
    // Search scope: 이름 (names only, fastest), 내용 (also extracted file text), or
    // 의미 (BGE-M3 meaning search). searchMode is the picker's live selection;
    // activeMode is the mode the current results came from (captured per search so
    // a refresh/retry re-runs in the same mode). Default = name-only.
    var searchMode by remember { mutableStateOf(FilesSearchMode.NAME) }
    var activeMode by remember { mutableStateOf(FilesSearchMode.NAME) }
    var actionTarget by remember { mutableStateOf<FilesEntry?>(null) }
    // File currently open in the in-app text/markdown viewer (null = closed).
    var textPreview by remember { mutableStateOf<FilesEntry?>(null) }
    var uploading by remember { mutableStateOf(false) }
    var uploadError by remember { mutableStateOf<String?>(null) }
    // Monotonic load token: each navigation/search/refresh bumps it and captures
    // the value; a slower in-flight load that finds the token changed bails out
    // instead of overwriting a newer folder's contents (out-of-order RPC guard).
    var loadToken by remember { mutableStateOf(0) }

    // CRUD dialog state. Each holds the target entry (rename/move/delete) or is a
    // plain bool (new folder). actionBusy gates the dialog buttons while an RPC runs.
    var renameTarget by remember { mutableStateOf<FilesEntry?>(null) }
    var moveTarget by remember { mutableStateOf<FilesEntry?>(null) }
    var deleteTarget by remember { mutableStateOf<FilesEntry?>(null) }
    var showNewFolder by remember { mutableStateOf(false) }
    var actionBusy by remember { mutableStateOf(false) }
    // Transient error from a CRUD op, shown under the header (cleared on next nav).
    var crudError by remember { mutableStateOf<String?>(null) }

    suspend fun loadCurrent() {
        val token = ++loadToken
        loadOk = null
        val res = client.filesList(pathStack.last())
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
        val res = client.filesSearch(q, content = activeMode.content, semantic = activeMode.semantic)
        if (token != loadToken) return
        entries = res ?: emptyList()
        loadOk = res != null
    }

    fun openFolder(e: FilesEntry) {
        searchText = ""
        activeQuery = null
        // Push the display-cased path so the folder title/breadcrumb (and upload
        // dest) keep mixed-case names; list accepts the display path too.
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
        // Re-run when either the query or the search mode changed (so switching
        // 이름/내용/의미 on the same query re-searches), but skip a redundant
        // identical search.
        if (q == activeQuery && searchMode == activeMode) return
        activeQuery = q
        activeMode = searchMode
        scope.launch { reload() }
    }

    LaunchedEffect(Unit) { loadCurrent() }

    // Android hardware back walks up the folder stack / out of search first.
    PlatformBackHandler(enabled = activeQuery != null || pathStack.size > 1) { goUp() }

    // Upload the picked device file into the current folder, then re-list. Hoisted
    // so the launcher is created once.
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
            val ok = client.filesUpload(dest, bytes) != null
            uploading = false
            if (ok) {
                loadCurrent()
            } else {
                uploadError = "업로드에 실패했습니다."
            }
        }
    }

    val title = when {
        activeQuery != null -> "파일 검색"
        pathStack.last().isBlank() -> "파일"
        else -> pathStack.last().substringAfterLast('/').ifBlank { "파일" }
    }

    DenebScreenScaffold(
        title = title,
        onBack = { if (!goUp()) onBack() },
        tabBar = navigationTabBar,
    ) {
        // Header: current path + upload action (upload only while browsing — search
        // results span folders, so "current folder" is undefined then).
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
            // New-folder + upload only while browsing — search results span folders,
            // so "current folder" (the create/upload target) is undefined then.
            if (activeQuery == null) {
                TextButton(onClick = {
                    crudError = null
                    showNewFolder = true
                }) {
                    Icon(Icons.Outlined.CreateNewFolder, contentDescription = null, modifier = Modifier.size(18.dp))
                    Spacer(Modifier.width(6.dp))
                    Text("새 폴더")
                }
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
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 24.dp),
            ) {
                Text(
                    it,
                    style = DenebType.meta,
                    color = MaterialTheme.colorScheme.error,
                    modifier = Modifier.weight(1f).padding(vertical = 2.dp),
                )
                // Retry = re-pick the file: the failed bytes aren't retained, and the
                // picker restores the exact upload context.
                TextButton(onClick = { uploadLauncher.launch() }) { Text("다시 시도") }
            }
        }
        crudError?.let {
            Text(
                it,
                style = DenebType.meta,
                color = MaterialTheme.colorScheme.error,
                modifier = Modifier.padding(horizontal = 24.dp, vertical = 2.dp),
            )
        }

        DenebUnderlineSearchField(
            query = searchText,
            onQueryChange = {
                searchText = it
                // Clearing the field returns to the current folder listing.
                if (it.isBlank() && activeQuery != null) runSearch("")
            },
            placeholder = "파일 검색",
            textStyle = DenebType.body,
            onSearch = { runSearch(searchText) },
            clearable = true,
            modifier = Modifier.padding(horizontal = 24.dp),
        )

        // Search scope: 이름 / 내용 / 의미. Material SingleChoiceSegmentedButton
        // (control), Deneb-Korean labels (presentation). Picking a mode while a
        // query is active re-runs that search in the new scope.
        FilesSearchModeRow(
            mode = searchMode,
            onModeChange = { m ->
                searchMode = m
                if (activeQuery != null) runSearch(searchText)
            },
        )

        // "상위 폴더" affordance — phones hide the in-app ← (system back drives it),
        // so a visible up row keeps deep folders navigable by touch.
        if (activeQuery == null && pathStack.size > 1) {
            Row(
                Modifier
                    .fillMaxWidth()
                    .denebPressable(onClick = {
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
                            "파일을 불러오지 못했습니다.",
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
                        haptics.refresh()
                        scope.launch {
                            refreshing = true
                            val token = ++loadToken
                            val q = activeQuery
                            val res = if (q != null) {
                                client.filesSearch(q, content = activeMode.content, semantic = activeMode.semantic)
                            } else {
                                client.filesList(pathStack.last())
                            }
                            // Drop a stale refresh result if the user navigated meanwhile.
                            if (token == loadToken) res?.let { entries = it }
                            refreshing = false
                        }
                    },
                    modifier = Modifier.fillMaxSize(),
                ) {
                    FilesListContent(
                        entries = entries,
                        onOpenFolder = {
                            haptics.tap()
                            openFolder(it)
                        },
                        onFileAction = {
                            haptics.tap()
                            actionTarget = it
                        },
                        // Long-press any row (file or folder) opens the action sheet —
                        // the only path to folder CRUD, since a folder tap descends.
                        onEntryLongPress = {
                            haptics.tap()
                            actionTarget = it
                        },
                    )
                }
            }
        }
    }

    // Per-entry action sheet. Material control; Deneb-styled rows inside. Files get
    // preview (image / text·markdown opens an in-app viewer) + the always-present
    // 공유 링크 (signed download link); folders get neither. Both get 이름 변경 /
    // 이동 / 삭제 (the management actions). Each management action opens its own
    // dialog (confirm for delete, text input for rename/move).
    actionTarget?.let { target ->
        ModalBottomSheet(onDismissRequest = { actionTarget = null }) {
            FilesActionSheetContent(
                entry = target,
                onPreview = if (target.isFolder) {
                    null
                } else {
                    previewKindOf(target.name)?.let { kind ->
                        {
                            actionTarget = null
                            when (kind) {
                                FilePreviewKind.IMAGE -> showFullScreenImage(client.filesDownloadUrl(target.pathDisplay))
                                FilePreviewKind.TEXT -> textPreview = target
                            }
                        }
                    }
                },
                onShare = if (target.isFolder) {
                    null
                } else {
                    {
                        actionTarget = null
                        scope.launch { client.filesShare(target.pathDisplay)?.let { openUrl(it) } }
                    }
                },
                onRename = {
                    actionTarget = null
                    crudError = null
                    renameTarget = target
                },
                onMove = {
                    actionTarget = null
                    crudError = null
                    moveTarget = target
                },
                onDelete = {
                    actionTarget = null
                    crudError = null
                    deleteTarget = target
                },
            )
        }
    }

    // --- New folder: name input. Creates "<current folder>/<name>" then re-lists. ---
    if (showNewFolder) {
        FilesNameDialog(
            title = "새 폴더",
            label = "폴더 이름",
            initial = "",
            confirmLabel = "만들기",
            busy = actionBusy,
            onDismiss = { if (!actionBusy) showNewFolder = false },
            onConfirm = { name ->
                scope.launch {
                    actionBusy = true
                    val folder = pathStack.last().trimEnd('/')
                    val err = client.filesMkdir("$folder/$name")
                    actionBusy = false
                    if (err == null) {
                        showNewFolder = false
                        loadCurrent()
                    } else {
                        showNewFolder = false
                        crudError = err
                    }
                }
            },
        )
    }

    // --- Rename: new name in the same parent folder (a same-folder move). ---
    renameTarget?.let { target ->
        FilesNameDialog(
            title = "이름 변경",
            label = "새 이름",
            initial = target.name,
            confirmLabel = "변경",
            busy = actionBusy,
            onDismiss = { if (!actionBusy) renameTarget = null },
            onConfirm = { name ->
                scope.launch {
                    actionBusy = true
                    val parent = target.pathDisplay.substringBeforeLast('/', "").ifBlank { "" }
                    val dst = "$parent/$name"
                    val err = client.filesMove(target.pathDisplay, dst)
                    actionBusy = false
                    renameTarget = null
                    if (err == null) reload() else crudError = err
                }
            },
        )
    }

    // --- Move: destination folder path (the store creates missing parents). ---
    moveTarget?.let { target ->
        FilesNameDialog(
            title = "이동",
            label = "대상 폴더 경로 (예: /계약/완료)",
            initial = target.pathDisplay.substringBeforeLast('/', "").ifBlank { "/" },
            confirmLabel = "이동",
            busy = actionBusy,
            onDismiss = { if (!actionBusy) moveTarget = null },
            onConfirm = { destFolder ->
                scope.launch {
                    actionBusy = true
                    val folder = destFolder.trim().trimEnd('/').ifBlank { "" }
                    val dst = "$folder/${target.name}"
                    val err = client.filesMove(target.pathDisplay, dst)
                    actionBusy = false
                    moveTarget = null
                    if (err == null) reload() else crudError = err
                }
            },
        )
    }

    // --- Delete: confirm, then remove and re-list. ---
    deleteTarget?.let { target ->
        AlertDialog(
            onDismissRequest = { if (!actionBusy) deleteTarget = null },
            title = { Text(if (target.isFolder) "폴더 삭제" else "파일 삭제") },
            text = {
                Text(
                    if (target.isFolder) {
                        "${target.name} 폴더를 삭제합니다. 비어 있지 않으면 삭제되지 않습니다."
                    } else {
                        "${target.name} 파일을 삭제합니다. 되돌릴 수 없습니다."
                    },
                )
            },
            confirmButton = {
                TextButton(
                    enabled = !actionBusy,
                    onClick = {
                        haptics.reject()
                        scope.launch {
                            actionBusy = true
                            val err = client.filesDelete(target.pathDisplay)
                            actionBusy = false
                            deleteTarget = null
                            if (err == null) reload() else crudError = err
                        }
                    },
                ) { Text("삭제", color = MaterialTheme.colorScheme.error) }
            },
            dismissButton = {
                TextButton(enabled = !actionBusy, onClick = { deleteTarget = null }) { Text("취소") }
            },
        )
    }

    // In-app text / markdown viewer (full screen). Fetches the body lazily; `.md`
    // renders through the chat markdown renderer (tables included), other text shows
    // monospace.
    textPreview?.let { target ->
        FilesTextViewerHost(
            client = client,
            entry = target,
            onBack = { textPreview = null },
        )
    }
}
