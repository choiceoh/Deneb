package ai.deneb.deneb

import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebPressable
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Checkbox
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/**
 * Pages within one wiki category (`miniapp.memory.list_in_category`). Tap a page
 * to open it; long-press (with haptic) to enter multi-select mode, then a flat
 * bottom bar either reclassifies the selected pages into another category
 * (`miniapp.memory.move_page`, via a picker) or deletes them
 * (`miniapp.memory.delete_pages`, after a confirmation). Framed by
 * [DenebScreenScaffold].
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun DenebCategoryPagesScreen(
    client: DenebGatewayClient,
    category: String,
    onBack: () -> Unit,
    onOpenWiki: (String) -> Unit = {},
    onOpenCategory: (String) -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var pages by remember(category) { mutableStateOf<List<WikiPageRef>?>(null) }
    var subFolders by remember(category) { mutableStateOf<List<CategoryNode>>(emptyList()) }
    // Top-level taxonomy categories, populated on load — the move picker's options.
    var topCategories by remember(category) { mutableStateOf<List<String>>(emptyList()) }
    var loadFailed by remember(category) { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()

    // Multi-select state. selected holds page paths (the delete/move key). busy
    // guards the in-flight action so a double-tap can't fire twice; confirmDelete
    // gates the destructive delete, and showMovePicker the reclassify dialog.
    var selecting by remember(category) { mutableStateOf(false) }
    val selected = remember(category) { mutableStateListOf<String>() }
    var busy by remember(category) { mutableStateOf(false) }
    var confirmDelete by remember(category) { mutableStateOf(false) }
    var showMovePicker by remember(category) { mutableStateOf(false) }

    fun clearSelection() {
        selecting = false
        selected.clear()
    }

    suspend fun load() {
        loadFailed = false
        pages = null
        val fetched = client.fetchCategoryPages(category)
        if (fetched == null) loadFailed = true else pages = fetched
        // One categories fetch feeds both the sub-folder rows and the move
        // picker's options.
        val cats = client.fetchCategories()?.categories ?: emptyList()
        // Sub-folders under this category (the next path segment) so a project
        // that accrues several documents drills down instead of cluttering the
        // top-level list.
        subFolders = subCategories(category, cats)
        // Top-level taxonomy categories (first path segment) — the reclassify targets.
        topCategories = cats.map { it.name.substringBefore('/') }
            .filter { it.isNotBlank() }
            .distinct()
            .sorted()
    }
    LaunchedEffect(category) { load() }

    fun runDelete() {
        if (busy) return
        haptics.reject()
        val paths = selected.toList()
        scope.launch {
            busy = true
            client.deleteCategoryPages(paths)
            busy = false
            confirmDelete = false
            clearSelection()
            // Reload so the list reflects exactly what survived — an honest view
            // even when the backend reported a partial failure.
            load()
        }
    }

    fun runMove(target: String) {
        if (busy) return
        haptics.tap()
        val paths = selected.toList()
        scope.launch {
            busy = true
            client.moveCategoryPages(paths, target)
            busy = false
            showMovePicker = false
            clearSelection()
            // Reload — moved pages leave this category's list (an honest view even
            // on a partial move, e.g. a name already taken in the target).
            load()
        }
    }

    if (confirmDelete) {
        AlertDialog(
            onDismissRequest = { if (!busy) confirmDelete = false },
            title = { Text("페이지 삭제") },
            text = { Text("선택한 ${selected.size}개 페이지를 삭제할까요? 되돌릴 수 없습니다.") },
            confirmButton = { TextButton(onClick = { runDelete() }, enabled = !busy) { Text("삭제") } },
            dismissButton = { TextButton(onClick = { confirmDelete = false }, enabled = !busy) { Text("취소") } },
        )
    }

    if (showMovePicker) {
        // Material overlay, Deneb-idiom rows inside: tap a category to reclassify
        // the selected pages into it (filed under <category>/<basename>).
        AlertDialog(
            onDismissRequest = { if (!busy) showMovePicker = false },
            title = { Text("${selected.size}개 페이지 이동") },
            text = {
                Column {
                    Text(
                        "옮길 카테고리를 선택하세요.",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Spacer(Modifier.height(8.dp))
                    topCategories.forEach { cat ->
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .denebPressable(onClick = { if (!busy) runMove(cat) })
                                .padding(vertical = 12.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(
                                cat,
                                style = DenebType.rowTitle,
                                color = MaterialTheme.colorScheme.onSurface,
                            )
                        }
                        HorizontalDivider(color = denebHairline())
                    }
                }
            },
            confirmButton = {},
            dismissButton = { TextButton(onClick = { showMovePicker = false }, enabled = !busy) { Text("취소") } },
        )
    }

    DenebScreenScaffold(title = "카테고리", onBack = onBack, tabBar = navigationTabBar) {
        Column(modifier = Modifier.padding(horizontal = 24.dp)) {
            if (selecting) {
                Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        "${selected.size}개 선택",
                        style = DenebType.cardTitle,
                        color = MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.weight(1f),
                    )
                    TextButton(onClick = { clearSelection() }) { Text("취소") }
                }
            } else {
                Text(
                    category.ifBlank { "(미분류)" },
                    style = DenebType.subject,
                    color = MaterialTheme.colorScheme.onSurface,
                )
            }
            Spacer(Modifier.height(12.dp))
            HorizontalDivider(color = denebHairline())
        }

        Box(Modifier.weight(1f).fillMaxWidth()) {
            Column(
                modifier = Modifier
                    .padding(horizontal = 24.dp)
                    .verticalScroll(rememberScrollState()),
            ) {
                if (!selecting && subFolders.isNotEmpty()) {
                    subFolders.forEach { sf ->
                        SubCategoryRow(sf.name.substringAfterLast('/'), sf.pageCount) { onOpenCategory(sf.name) }
                    }
                    Spacer(Modifier.height(4.dp))
                }
                val p = pages
                when {
                    loadFailed -> {
                        Spacer(Modifier.height(12.dp))
                        DenebError(
                            "페이지를 불러오지 못했습니다.",
                            onRetry = { scope.launch { load() } },
                        )
                    }

                    p == null -> {
                        Spacer(Modifier.height(12.dp))
                        DenebLoading()
                    }

                    p.isEmpty() -> {
                        Spacer(Modifier.height(12.dp))
                        DenebEmpty("이 카테고리에 페이지가 없습니다.")
                    }

                    else -> p.forEach { page ->
                        val isSelected = page.path in selected
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .combinedClickable(
                                    onClick = {
                                        haptics.tap()
                                        if (selecting) {
                                            if (isSelected) selected.remove(page.path) else selected.add(page.path)
                                            if (selected.isEmpty()) selecting = false
                                        } else {
                                            onOpenWiki(page.path)
                                        }
                                    },
                                    onLongClick = {
                                        haptics.longPress()
                                        selecting = true
                                        if (page.path !in selected) selected.add(page.path)
                                    },
                                )
                                .background(
                                    if (isSelected) {
                                        MaterialTheme.colorScheme.primaryContainer.copy(alpha = 0.5f)
                                    } else {
                                        Color.Transparent
                                    },
                                )
                                .padding(vertical = 12.dp),
                            verticalAlignment = Alignment.Top,
                        ) {
                            if (selecting) {
                                Checkbox(
                                    checked = isSelected,
                                    onCheckedChange = null,
                                    modifier = Modifier.padding(end = 10.dp),
                                )
                            }
                            Column(Modifier.weight(1f)) {
                                Text(
                                    page.title,
                                    style = DenebType.rowTitleStrong,
                                    color = if (isSelected) {
                                        MaterialTheme.colorScheme.primary
                                    } else {
                                        MaterialTheme.colorScheme.onSurface
                                    },
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                                if (page.summary.isNotBlank()) {
                                    Text(
                                        page.summary,
                                        style = DenebType.rowSubtitle,
                                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                                        maxLines = 2,
                                        overflow = TextOverflow.Ellipsis,
                                    )
                                }
                                if (page.updated.isNotBlank()) {
                                    Text(
                                        page.updated.take(10),
                                        style = DenebType.meta,
                                        color = denebHint(),
                                    )
                                }
                            }
                        }
                        HorizontalDivider(color = denebHairline())
                    }
                }
                Spacer(Modifier.height(24.dp))
            }
        }

        if (selecting && selected.isNotEmpty()) {
            // Flat action bar in the Deneb idiom: a hairline above, no elevation.
            Column(Modifier.fillMaxWidth()) {
                HorizontalDivider(color = denebHairline())
                Row(
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text("${selected.size}개 선택", style = MaterialTheme.typography.titleSmall)
                    Spacer(Modifier.weight(1f))
                    TextButton(
                        onClick = { showMovePicker = true },
                        enabled = !busy && topCategories.isNotEmpty(),
                    ) {
                        Text("이동")
                    }
                    Spacer(Modifier.width(4.dp))
                    TextButton(
                        onClick = { confirmDelete = true },
                        enabled = !busy,
                    ) {
                        Text("삭제", color = MaterialTheme.colorScheme.error)
                    }
                }
            }
        }
    }
}

/** A sub-folder row in the Deneb idiom: name + page count + a drill chevron, in
 *  the flat-hairline row style of the category browser. Navigation only — never
 *  part of the page multi-select below it. */
@Composable
private fun SubCategoryRow(label: String, count: Int, onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .denebPressable(onClick = onClick)
            .padding(vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            label,
            style = DenebType.rowTitle,
            color = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.weight(1f),
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            "$count ›",
            style = DenebType.meta,
            color = MaterialTheme.colorScheme.primary,
        )
    }
    HorizontalDivider(color = denebHairline())
}
