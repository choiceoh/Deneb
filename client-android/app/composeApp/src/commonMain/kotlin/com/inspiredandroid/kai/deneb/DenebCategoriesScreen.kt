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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.components.rememberHaptics
import kotlinx.coroutines.launch

/**
 * Wiki category browser (`miniapp.memory.categories`): every category with its
 * page count + corpus totals. Tap a category to list its pages. Surface-wrapped
 * so unstyled text inherits the right content color in dark mode.
 */
@Composable
fun DenebCategoriesScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onOpenCategory: (String) -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var data by remember { mutableStateOf<WikiCategories?>(null) }
    var loadFailed by remember { mutableStateOf(false) }
    val haptics = rememberHaptics()
    val scope = rememberCoroutineScope()

    suspend fun load() {
        loadFailed = false
        data = null
        val d = client.fetchCategories()
        data = d
        loadFailed = d == null
    }
    LaunchedEffect(Unit) { load() }

    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(
            modifier = Modifier.statusBarsPadding().padding(16.dp).verticalScroll(rememberScrollState()),
        ) {
            if (navigationTabBar != null) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) { navigationTabBar() }
                Spacer(Modifier.height(16.dp))
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    "카테고리",
                    style = MaterialTheme.typography.headlineMedium,
                    fontWeight = FontWeight.SemiBold,
                    modifier = Modifier.weight(1f),
                )
                TextButton(onClick = onBack) { Text("닫기") }
            }
            Spacer(Modifier.height(8.dp))

            val d = data
            when {
                d == null && loadFailed -> DenebError(
                    "카테고리를 불러오지 못했습니다.",
                    onRetry = { scope.launch { load() } },
                )
                d == null -> DenebLoading()
                d.categories.isEmpty() -> DenebEmpty("위키 페이지가 없습니다.")
                else -> {
                    Text(
                        "${d.totalPages}개 페이지 · ${humanBytes(d.totalBytes)}",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Spacer(Modifier.height(8.dp))
                    d.categories.forEach { cat ->
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clickable { haptics.tap(); onOpenCategory(cat.name) }
                                .padding(vertical = 14.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(
                                cat.name.ifBlank { "(미분류)" },
                                style = MaterialTheme.typography.bodyLarge,
                                color = MaterialTheme.colorScheme.onSurface,
                                modifier = Modifier.weight(1f),
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                            Text(
                                "${cat.pageCount}",
                                style = MaterialTheme.typography.labelMedium,
                                color = MaterialTheme.colorScheme.primary,
                            )
                        }
                        HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
                    }
                    Spacer(Modifier.height(24.dp))
                }
            }
        }
    }
}
