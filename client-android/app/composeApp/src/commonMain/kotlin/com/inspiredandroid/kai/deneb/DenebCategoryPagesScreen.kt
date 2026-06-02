package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
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
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.components.rememberHaptics
import kotlinx.coroutines.launch

/**
 * Pages within one wiki category (`miniapp.memory.list_in_category`). Tap a page
 * to open it. Surface-wrapped for dark mode.
 */
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

    suspend fun load() {
        loadFailed = false
        pages = null
        val fetched = client.fetchCategoryPages(category)
        if (fetched == null) loadFailed = true else pages = fetched
    }
    LaunchedEffect(category) { load() }

    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(
            modifier = Modifier.statusBarsPadding().padding(16.dp).verticalScroll(rememberScrollState()),
        ) {
            if (navigationTabBar != null) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) { navigationTabBar() }
                Spacer(Modifier.height(12.dp))
            }
            TextButton(onClick = onBack) { Text("← 뒤로") }
            Spacer(Modifier.height(4.dp))
            Text(
                category.ifBlank { "(미분류)" },
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.SemiBold,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Spacer(Modifier.height(12.dp))
            HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)

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
                    Column(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable { haptics.tap(); onOpenWiki(page.path) }
                            .padding(vertical = 12.dp),
                    ) {
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
                    HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
                }
            }
            Spacer(Modifier.height(24.dp))
        }
    }
}
