package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Checkbox
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.components.rememberHaptics
import kotlinx.coroutines.launch

/**
 * Pages within one wiki category (`miniapp.memory.list_in_category`). Tap a page
 * to open it; long-press (with haptic) to enter multi-select mode, then a tonal
 * bottom bar deletes the selected pages via `miniapp.memory.delete_pages` after a
 * confirmation. Surface-wrapped for dark mode.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun DenebCategoryPagesScreen(
    client: DenebGatewayClient,
    category: String,
    onBack: () -> Unit,
    onOpenWiki: (String) -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var pages by remember(category) { mutableStateOf<List<WikiPageRef>?>(null) }
    var loadFailed by remember(category) { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()

    // Multi-select state. selected holds page paths (the delete key). busy guards
    // the in-flight delete so a double-tap can't fire two deletes; confirmDelete
    // gates the destructive action behind a dialog.
    var selecting by remember(category) { mutableStateOf(false) }
    val selected = remember(category) { mutableStateListOf<String>() }
    var busy by remember(category) { mutableStateOf(false) }
    var confirmDelete by remember(category) { mutableStateOf(false) }

    fun clearSelection() {
        selecting = false
        selected.clear()
    }

    suspend fun load() {
        loadFailed = false
        pages = null
        val fetched = client.fetchCategoryPages(category)
        if (fetched == null) loadFailed = true else pages = fetched
    }
    LaunchedEffect(category) { load() }

    fun runDelete() {
        if (busy) return
        haptics.confirm()
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

    if (confirmDelete) {
        AlertDialog(
            onDismissRequest = { if (!busy) confirmDelete = false },
            title = { Text("페이지 삭제") },
            text = { Text("선택한 ${selected.size}개 페이지를 삭제할까요? 되돌릴 수 없습니다.") },
            confirmButton = { TextButton(onClick = { runDelete() }, enabled = !busy) { Text("삭제") } },
            dismissButton = { TextButton(onClick = { confirmDelete = false }, enabled = !busy) { Text("취소") } },
        )
    }

    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) {
            Column(modifier = Modifier.padding(horizontal = 16.dp).padding(top = 16.dp)) {
                if (navigationTabBar != null) {
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) { navigationTabBar() }
                    Spacer(Modifier.height(12.dp))
                }
                if (selecting) {
                    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        Text(
                            "${selected.size}개 선택",
                            style = MaterialTheme.typography.titleLarge,
                            fontWeight = FontWeight.SemiBold,
                            color = MaterialTheme.colorScheme.onSurface,
                            modifier = Modifier.weight(1f),
                        )
                        TextButton(onClick = { clearSelection() }) { Text("취소") }
                    }
                } else {
                    TextButton(onClick = onBack) { Text("← 뒤로") }
                    Spacer(Modifier.height(4.dp))
                    Text(
                        category.ifBlank { "(미분류)" },
                        style = MaterialTheme.typography.titleLarge,
                        fontWeight = FontWeight.SemiBold,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                }
                Spacer(Modifier.height(12.dp))
                HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
            }

            Box(Modifier.weight(1f).fillMaxWidth()) {
                Column(
                    modifier = Modifier
                        .padding(horizontal = 16.dp)
                        .verticalScroll(rememberScrollState()),
                ) {
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
                                            haptics.confirm()
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
                                        style = MaterialTheme.typography.bodyLarge,
                                        color = MaterialTheme.colorScheme.onSurface,
                                        maxLines = 1,
                                        overflow = TextOverflow.Ellipsis,
                                    )
                                    if (page.summary.isNotBlank()) {
                                        Text(
                                            page.summary,
                                            style = MaterialTheme.typography.bodySmall,
                                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                                            maxLines = 2,
                                            overflow = TextOverflow.Ellipsis,
                                        )
                                    }
                                    if (page.updated.isNotBlank()) {
                                        Text(
                                            page.updated.take(10),
                                            style = MaterialTheme.typography.labelSmall,
                                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                                        )
                                    }
                                }
                            }
                            HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
                        }
                    }
                    Spacer(Modifier.height(24.dp))
                }
            }

            if (selecting && selected.isNotEmpty()) {
                Surface(tonalElevation = 3.dp, shadowElevation = 6.dp) {
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text("${selected.size}개 선택", style = MaterialTheme.typography.titleSmall)
                        Spacer(Modifier.weight(1f))
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
}
