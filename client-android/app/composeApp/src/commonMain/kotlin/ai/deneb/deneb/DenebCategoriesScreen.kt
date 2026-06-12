package ai.deneb.deneb

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
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
import ai.deneb.Platform
import ai.deneb.currentPlatform
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebPressable
import kotlinx.coroutines.launch

/** The wiki category absorbed by the merged people surface: its pages reach the
 *  user through the pinned "사람" entry (with Gmail recency folded in), so the
 *  raw 인물 bucket is hidden from the category list below to avoid two "people"
 *  rows meaning different things on one screen. */
private const val PEOPLE_WIKI_CATEGORY = "인물"

/**
 * Wiki category browser (`miniapp.memory.categories`): every category with its
 * page count + corpus totals. Tap a category to list its pages. Also the browse
 * hub's pinned entry points — "사람" (the merged people surface) and "최근 일기"
 * — which are not wiki categories themselves. Framed by [DenebScreenScaffold].
 */
@Composable
fun DenebCategoriesScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onOpenCategory: (String) -> Unit = {},
    onOpenDiary: () -> Unit = {},
    onOpenPeople: () -> Unit = {},
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

    // Desktop: the persistent sidebar is the navigation — a back affordance on a
    // top-level section is redundant there (showBack drops it).
    DenebScreenScaffold(
        title = "카테고리",
        onBack = onBack,
        tabBar = navigationTabBar,
        showBack = currentPlatform !is Platform.Desktop,
    ) {
        Column(
            Modifier
                .fillMaxWidth()
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp),
        ) {
            Spacer(Modifier.height(8.dp))

            // Pinned entry points above the wiki buckets: the merged people surface
            // (recent Gmail contacts + 인물 pages) and the recent-diary timeline.
            PinnedEntryRow("사람") { haptics.tap(); onOpenPeople() }
            PinnedEntryRow("최근 일기") { haptics.tap(); onOpenDiary() }
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
                    d.categories.filter { it.name != PEOPLE_WIKI_CATEGORY }.forEach { cat ->
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .denebPressable(onClick = { haptics.tap(); onOpenCategory(cat.name) })
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

/** A pinned non-category entry point row ("사람", "최근 일기"): label + a trailing
 *  arrow, in the same flat row idiom as the category rows below it. */
@Composable
private fun PinnedEntryRow(label: String, onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .denebPressable(onClick = onClick)
            .padding(vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            label,
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.weight(1f),
        )
        Text(
            "→",
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.primary,
        )
    }
    HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
}
