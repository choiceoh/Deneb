package ai.deneb.deneb

import ai.deneb.PlatformBackHandler
import ai.deneb.openUrl
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebSearchField
import ai.deneb.ui.components.LocalShowFullScreenImageModel
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebPressable
import ai.deneb.ui.markdown.MarkdownContent
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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.DriveFileMove
import androidx.compose.material.icons.automirrored.outlined.InsertDriveFile
import androidx.compose.material.icons.outlined.CloudUpload
import androidx.compose.material.icons.outlined.CreateNewFolder
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.DriveFileRenameOutline
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
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
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
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
            Text(
                it,
                style = DenebType.meta,
                color = MaterialTheme.colorScheme.error,
                modifier = Modifier.padding(horizontal = 24.dp, vertical = 2.dp),
            )
        }
        crudError?.let {
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
            placeholder = "파일 검색",
            onSearch = { runSearch(searchText) },
            clearContentDescription = "검색 지우기",
            modifier = Modifier.padding(horizontal = 16.dp),
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

/**
 * The three file-search scopes. [content]/[semantic] are the mutually-exclusive
 * wire flags passed to `miniapp.files.search` (see [filesSearch]):
 * - NAME: file names only (both false) — fastest.
 * - CONTENT: also extracted file text (PDF/Word/Excel/…) — slower.
 * - SEMANTIC: BGE-M3 meaning search — backend ranks by score, and falls back to
 *   name/content if the embedding server is down.
 */
internal enum class FilesSearchMode(val label: String, val content: Boolean, val semantic: Boolean) {
    NAME("이름", content = false, semantic = false),
    CONTENT("내용", content = true, semantic = false),
    SEMANTIC("의미", content = false, semantic = true),
}

/**
 * The 이름 / 내용 / 의미 search-scope selector — a stateless, previewable body
 * ([DenebFilesScreen] owns the selection state + re-search). Material
 * SingleChoiceSegmentedButton for the control (selection state, a11y, haptics);
 * Deneb-Korean labels for presentation.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun FilesSearchModeRow(
    mode: FilesSearchMode,
    onModeChange: (FilesSearchMode) -> Unit,
    modifier: Modifier = Modifier,
) {
    val haptics = rememberHaptics()
    val modes = FilesSearchMode.entries
    SingleChoiceSegmentedButtonRow(
        modifier
            .fillMaxWidth()
            .padding(start = 16.dp, end = 16.dp, top = 2.dp, bottom = 6.dp),
    ) {
        modes.forEachIndexed { i, m ->
            SegmentedButton(
                selected = mode == m,
                onClick = {
                    if (mode != m) {
                        haptics.tap()
                        onModeChange(m)
                    }
                },
                shape = SegmentedButtonDefaults.itemShape(i, modes.size),
            ) { Text(m.label, style = DenebType.rowSubtitle) }
        }
    }
}

/**
 * The folder listing as a column of rows — the stateless, previewable core
 * ([DenebFilesScreen] owns the data + states around it). Folders descend on tap;
 * files open the action sheet.
 */
@Composable
internal fun FilesListContent(
    entries: List<FilesEntry>,
    onOpenFolder: (FilesEntry) -> Unit,
    onFileAction: (FilesEntry) -> Unit,
    modifier: Modifier = Modifier,
    // Long-press any row → entry action sheet (the only way to manage a folder,
    // whose tap descends). Null in previews that don't exercise it.
    onEntryLongPress: ((FilesEntry) -> Unit)? = null,
) {
    LazyColumn(modifier.fillMaxSize()) {
        items(entries, key = { it.id.ifBlank { it.pathLower.ifBlank { it.pathDisplay } } }) { e ->
            FilesRow(
                entry = e,
                onClick = { if (e.isFolder) onOpenFolder(e) else onFileAction(e) },
                onLongClick = onEntryLongPress?.let { { it(e) } },
            )
            HorizontalDivider(color = denebHairline())
        }
    }
}

/** One file row: type icon, name, and (files) a size · modified meta line. A
 *  long-press (when wired) opens the entry action sheet. */
@Composable
internal fun FilesRow(entry: FilesEntry, onClick: () -> Unit, onLongClick: (() -> Unit)? = null) {
    Row(
        Modifier
            .fillMaxWidth()
            .denebPressable(onClick = onClick, onLongClick = onLongClick)
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
            val meta = filesRowMeta(entry)
            if (meta.isNotBlank()) {
                Spacer(Modifier.height(2.dp))
                Text(meta, style = DenebType.meta, color = denebHint(), maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

/**
 * Bottom-sheet actions for a single entry (file or folder). For files, [onPreview]
 * (when non-null: image / text·markdown) is the primary "미리보기" action and
 * [onShare] (non-null) offers the signed download link; folders pass null for both.
 * [onRename] / [onMove] / [onDelete] (the management actions) are always shown,
 * with 삭제 last and tinted as destructive.
 */
@Composable
internal fun FilesActionSheetContent(
    entry: FilesEntry,
    onPreview: (() -> Unit)?,
    onShare: (() -> Unit)?,
    onRename: () -> Unit,
    onMove: () -> Unit,
    onDelete: () -> Unit,
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
        if (onPreview != null) {
            FilesActionRow(icon = Icons.Outlined.Visibility, label = "미리보기", onClick = onPreview)
            HorizontalDivider(color = denebHairline())
        }
        if (onShare != null) {
            FilesActionRow(icon = Icons.Outlined.Link, label = "공유 링크", onClick = onShare)
            HorizontalDivider(color = denebHairline())
        }
        FilesActionRow(icon = Icons.Outlined.DriveFileRenameOutline, label = "이름 변경", onClick = onRename)
        HorizontalDivider(color = denebHairline())
        FilesActionRow(icon = Icons.AutoMirrored.Outlined.DriveFileMove, label = "이동", onClick = onMove)
        HorizontalDivider(color = denebHairline())
        FilesActionRow(
            icon = Icons.Outlined.Delete,
            label = "삭제",
            onClick = onDelete,
            tint = MaterialTheme.colorScheme.error,
        )
    }
}

/** One tappable action row in the file action sheet (icon + label). [tint] colors
 *  both the icon and label (defaults to primary; destructive rows pass error). */
@Composable
private fun FilesActionRow(
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    label: String,
    onClick: () -> Unit,
    tint: androidx.compose.ui.graphics.Color = MaterialTheme.colorScheme.primary,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .denebPressable(onClick = onClick)
            .padding(horizontal = 24.dp, vertical = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(22.dp))
        Spacer(Modifier.width(16.dp))
        Text(label, style = DenebType.rowTitle, color = tint)
    }
}

/**
 * A single-field text-input dialog (Material AlertDialog) for the name/path entry
 * shared by 새 폴더 / 이름 변경 / 이동. Confirm is disabled while [busy] or the
 * field is blank. Controls are Material (OutlinedTextField + TextButton); only the
 * copy is Deneb-Korean.
 */
@Composable
private fun FilesNameDialog(
    title: String,
    label: String,
    initial: String,
    confirmLabel: String,
    busy: Boolean,
    onDismiss: () -> Unit,
    onConfirm: (String) -> Unit,
) {
    var value by remember { mutableStateOf(initial) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            OutlinedTextField(
                value = value,
                onValueChange = { value = it },
                label = { Text(label) },
                singleLine = true,
                enabled = !busy,
                modifier = Modifier.fillMaxWidth(),
            )
        },
        confirmButton = {
            TextButton(
                enabled = !busy && value.trim().isNotBlank(),
                onClick = { onConfirm(value.trim()) },
            ) { Text(confirmLabel) }
        },
        dismissButton = {
            TextButton(enabled = !busy, onClick = onDismiss) { Text("취소") }
        },
    )
}

/** Size · modified meta for a file row; folders show none. */
private fun filesRowMeta(e: FilesEntry): String {
    if (e.isFolder) return ""
    val size = humanBytes(e.size)
    val date = e.modified.takeIf { it.isNotBlank() }?.let { shortDate(it) }
    return if (date != null) "$size · $date" else size
}

// --- In-app preview -------------------------------------------------------

/** What kind of in-app preview a file supports, by extension. */
internal enum class FilePreviewKind { IMAGE, TEXT }

private val IMAGE_EXTS = setOf("png", "jpg", "jpeg", "gif", "webp", "bmp")

// Text-ish extensions we render in-app (markdown gets the rich renderer, the rest
// monospace). Kept conservative — anything not listed (pdf/docx/binaries) falls
// back to the share link.
private val TEXT_EXTS = setOf(
    "txt", "md", "markdown", "json", "csv", "tsv", "log", "xml", "yaml", "yml",
    "kt", "go", "py", "js", "ts", "tsx", "jsx", "sh", "conf", "ini", "toml",
    "java", "c", "cpp", "h", "rs", "rb", "php", "sql", "html", "css", "env",
    "properties", "gradle", "kts",
)

/** Lower-cased extension after the last dot ("" when none). */
private fun fileExt(name: String): String = name.substringAfterLast('.', "").lowercase()

/** The preview kind for [name], or null when only the share link applies. */
internal fun previewKindOf(name: String): FilePreviewKind? = when (fileExt(name)) {
    in IMAGE_EXTS -> FilePreviewKind.IMAGE
    in TEXT_EXTS -> FilePreviewKind.TEXT
    else -> null
}

/** True when [name]'s extension wants the rich markdown renderer (tables etc.). */
private fun isMarkdown(name: String): Boolean = fileExt(name) in setOf("md", "markdown")

/**
 * Stateful host for the text/markdown viewer: fetches the body lazily and shows
 * loading / error / content via the stateless [FilesTextViewerContent]. Separated
 * so the body can be exercised by renderPreviews with mock data.
 */
@Composable
private fun FilesTextViewerHost(
    client: DenebGatewayClient,
    entry: FilesEntry,
    onBack: () -> Unit,
) {
    val scope = rememberCoroutineScope()
    // null = loading, else loaded (text may be "" for an empty file); ok=false → error.
    var text by remember(entry.pathLower) { mutableStateOf<String?>(null) }
    var loadOk by remember(entry.pathLower) { mutableStateOf<Boolean?>(null) }

    suspend fun load() {
        loadOk = null
        text = null
        val res = client.filesDownloadText(entry.pathDisplay)
        text = res
        loadOk = res != null
    }

    LaunchedEffect(entry.pathLower) { load() }
    // Android hardware back closes the viewer first.
    PlatformBackHandler(enabled = true) { onBack() }

    FilesTextViewerContent(
        name = entry.name,
        markdown = isMarkdown(entry.name),
        text = text,
        loadOk = loadOk,
        onBack = onBack,
        onRetry = { scope.launch { load() } },
    )
}

/**
 * Stateless text/markdown viewer body in the Deneb idiom (DenebScreenScaffold).
 * [text] null = loading; [loadOk] false = fetch failed. Markdown files render via
 * the chat [MarkdownContent] renderer (tables, lists, …); other text shows in a
 * scrollable monospace block.
 */
@Composable
internal fun FilesTextViewerContent(
    name: String,
    markdown: Boolean,
    text: String?,
    loadOk: Boolean?,
    onBack: () -> Unit,
    onRetry: () -> Unit,
) {
    DenebScreenScaffold(title = name, onBack = onBack) {
        when {
            text == null && loadOk == null ->
                Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { DenebLoading() }

            loadOk == false ->
                Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    DenebError("파일을 불러오지 못했습니다.", onRetry = onRetry)
                }

            (text ?: "").isBlank() ->
                Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    DenebEmpty("빈 파일입니다")
                }

            else -> Column(
                Modifier
                    .fillMaxSize()
                    .verticalScroll(rememberScrollState())
                    .padding(horizontal = 20.dp, vertical = 8.dp),
            ) {
                val body = text ?: ""
                if (markdown) {
                    MarkdownContent(body, baseStyle = MaterialTheme.typography.bodyMedium)
                } else {
                    Text(
                        body,
                        style = DenebType.body.copy(fontFamily = FontFamily.Monospace, fontSize = 13.sp),
                        color = MaterialTheme.colorScheme.onBackground,
                    )
                }
                Spacer(Modifier.height(24.dp))
            }
        }
    }
}
