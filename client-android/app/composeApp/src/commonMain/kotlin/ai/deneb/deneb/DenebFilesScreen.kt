package ai.deneb.deneb

import ai.deneb.PlatformBackHandler
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
import androidx.compose.material.icons.automirrored.outlined.InsertDriveFile
import androidx.compose.material.icons.outlined.CloudUpload
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material.icons.outlined.Visibility
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
    val uriHandler = LocalUriHandler.current
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
    var actionTarget by remember { mutableStateOf<FilesEntry?>(null) }
    // File currently open in the in-app text/markdown viewer (null = closed).
    var textPreview by remember { mutableStateOf<FilesEntry?>(null) }
    var uploading by remember { mutableStateOf(false) }
    var uploadError by remember { mutableStateOf<String?>(null) }
    // Monotonic load token: each navigation/search/refresh bumps it and captures
    // the value; a slower in-flight load that finds the token changed bails out
    // instead of overwriting a newer folder's contents (out-of-order RPC guard).
    var loadToken by remember { mutableStateOf(0) }

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
        val res = client.filesSearch(q)
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
        if (q == activeQuery) return
        activeQuery = q
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
            placeholder = "파일 검색",
            onSearch = { runSearch(searchText) },
            clearContentDescription = "검색 지우기",
            modifier = Modifier.padding(horizontal = 16.dp),
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
                            val res = if (q != null) client.filesSearch(q) else client.filesList(pathStack.last())
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
                    )
                }
            }
        }
    }

    // Per-file action sheet. Material control; Deneb-styled rows inside. Preview-able
    // types (image / text·markdown) get a primary "미리보기" action that opens an in-app
    // viewer; everything else (and the always-present 공유 링크) falls back to the signed
    // download link opened in the browser.
    actionTarget?.let { target ->
        ModalBottomSheet(onDismissRequest = { actionTarget = null }) {
            FilesActionSheetContent(
                entry = target,
                onPreview = previewKindOf(target.name)?.let { kind ->
                    {
                        actionTarget = null
                        when (kind) {
                            FilePreviewKind.IMAGE -> showFullScreenImage(client.filesDownloadUrl(target.pathLower))
                            FilePreviewKind.TEXT -> textPreview = target
                        }
                    }
                },
                onShare = {
                    actionTarget = null
                    scope.launch { client.filesShare(target.pathLower)?.let { uriHandler.openUri(it) } }
                },
            )
        }
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
) {
    LazyColumn(modifier.fillMaxSize()) {
        items(entries, key = { it.id.ifBlank { it.pathLower.ifBlank { it.pathDisplay } } }) { e ->
            FilesRow(entry = e, onClick = { if (e.isFolder) onOpenFolder(e) else onFileAction(e) })
            HorizontalDivider(color = denebHairline())
        }
    }
}

/** One file row: type icon, name, and (files) a size · modified meta line. */
@Composable
internal fun FilesRow(entry: FilesEntry, onClick: () -> Unit) {
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
            val meta = filesRowMeta(entry)
            if (meta.isNotBlank()) {
                Spacer(Modifier.height(2.dp))
                Text(meta, style = DenebType.meta, color = denebHint(), maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

/**
 * Bottom-sheet actions for a single file. When [onPreview] is non-null (image /
 * text·markdown) it is the primary "미리보기" action; the signed download link
 * ([onShare]) is always offered as the fallback / share path.
 */
@Composable
internal fun FilesActionSheetContent(
    entry: FilesEntry,
    onPreview: (() -> Unit)?,
    onShare: () -> Unit,
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
            FilesActionRow(
                icon = Icons.Outlined.Visibility,
                label = "미리보기",
                onClick = onPreview,
            )
            HorizontalDivider(color = denebHairline())
        }
        FilesActionRow(
            icon = Icons.Outlined.Link,
            label = "공유 링크",
            onClick = onShare,
        )
    }
}

/** One tappable action row in the file action sheet (icon + label). */
@Composable
private fun FilesActionRow(
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    label: String,
    onClick: () -> Unit,
) {
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
        val res = client.filesDownloadText(entry.pathLower)
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
