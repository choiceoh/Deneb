package ai.deneb.deneb

import ai.deneb.PlatformBackHandler
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.JetBrainsMonoFamily
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebPressable
import ai.deneb.ui.icons.automirrored.outlined.DriveFileMove
import ai.deneb.ui.icons.automirrored.outlined.InsertDriveFile
import ai.deneb.ui.icons.outlined.DriveFileRenameOutline
import ai.deneb.ui.icons.outlined.Folder
import ai.deneb.ui.icons.outlined.Link
import ai.deneb.ui.icons.outlined.Visibility
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
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch

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
internal fun FilesNameDialog(
    title: String,
    label: String,
    initial: String,
    confirmLabel: String,
    busy: Boolean,
    onDismiss: () -> Unit,
    onConfirm: (String) -> Unit,
) {
    var value by remember { mutableStateOf(initial) }
    val haptics = rememberHaptics()
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
                onClick = {
                    haptics.confirm()
                    onConfirm(value.trim())
                },
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
internal fun FilesTextViewerHost(
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
                        style = DenebType.body.copy(fontFamily = JetBrainsMonoFamily(), fontSize = 13.sp),
                        color = MaterialTheme.colorScheme.onBackground,
                    )
                }
                Spacer(Modifier.height(24.dp))
            }
        }
    }
}
