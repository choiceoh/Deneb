package ai.deneb.deneb

import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.icons.outlined.Public
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

internal data class BrowserTabVisual(
    val favicon: ImageBitmap? = null,
    val preview: ImageBitmap? = null,
)

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun BrowserTabsSheet(
    store: BrowserTabStore,
    visuals: Map<String, BrowserTabVisual> = emptyMap(),
    waitingForSlot: Boolean = false,
    onSelect: (String) -> Unit,
    onClose: (String) -> Unit,
    onAdd: () -> Unit,
    onDismiss: () -> Unit,
) {
    val haptics = rememberHaptics()
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier
                .fillMaxWidth()
                .fillMaxHeight(0.78f)
                .padding(bottom = 16.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth().padding(horizontal = 24.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(Modifier.weight(1f)) {
                    Text("탭", style = DenebType.subject, color = MaterialTheme.colorScheme.onSurface)
                    Text(
                        if (waitingForSlot) {
                            "새 탭을 계속 열려면 하나를 닫으세요"
                        } else {
                            "${store.tabs.size} / $BROWSER_TAB_LIMIT"
                        },
                        style = DenebType.meta,
                        color = if (waitingForSlot) MaterialTheme.colorScheme.primary else denebHint(),
                    )
                }
                TextButton(
                    enabled = store.tabs.size < BROWSER_TAB_LIMIT,
                    onClick = {
                        haptics.tap()
                        onAdd()
                    },
                ) {
                    Icon(Icons.Outlined.Add, contentDescription = null, modifier = Modifier.size(18.dp))
                    Text("새 탭", modifier = Modifier.padding(start = 4.dp))
                }
            }
            Spacer(Modifier.height(12.dp))
            HorizontalDivider(color = denebHairline())
            LazyColumn(Modifier.fillMaxWidth().weight(1f)) {
                items(store.tabs, key = { it.id }) { tab ->
                    val active = tab.id == store.activeId
                    val visual = visuals[tab.id]
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .background(
                                if (active) {
                                    MaterialTheme.colorScheme.primary.copy(alpha = 0.08f)
                                } else {
                                    MaterialTheme.colorScheme.background
                                },
                            )
                            .clickable {
                                haptics.tap()
                                onSelect(tab.id)
                            }
                            .padding(start = 24.dp, top = 12.dp, bottom = 12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        BrowserTabArtwork(visual = visual, active = active)
                        Column(Modifier.weight(1f).padding(start = 12.dp)) {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Text(
                                    browserTabDisplayTitle(tab),
                                    style = DenebType.rowTitle,
                                    color = MaterialTheme.colorScheme.onSurface,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                    modifier = Modifier.weight(1f),
                                )
                                if (active) {
                                    Text(
                                        "현재",
                                        style = DenebType.meta,
                                        color = MaterialTheme.colorScheme.primary,
                                        modifier = Modifier.padding(start = 8.dp),
                                    )
                                }
                            }
                            Text(
                                browserTabDisplayUrl(tab),
                                style = DenebType.meta,
                                color = denebHint(),
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                        }
                        IconButton(
                            onClick = {
                                haptics.tap()
                                onClose(tab.id)
                            },
                            modifier = Modifier.size(48.dp),
                        ) {
                            Icon(Icons.Outlined.Close, contentDescription = "탭 닫기", tint = denebHint())
                        }
                    }
                    HorizontalDivider(color = denebHairline(), modifier = Modifier.padding(start = 124.dp))
                }
            }
        }
    }
}

@Composable
private fun BrowserTabArtwork(visual: BrowserTabVisual?, active: Boolean) {
    val preview = visual?.preview
    if (preview != null) {
        Image(
            bitmap = preview,
            contentDescription = "탭 미리보기",
            contentScale = ContentScale.Crop,
            modifier = Modifier
                .width(88.dp)
                .height(56.dp)
                .clip(RoundedCornerShape(10.dp)),
        )
        return
    }
    Surface(
        color = if (active) {
            MaterialTheme.colorScheme.primary.copy(alpha = 0.10f)
        } else {
            MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.55f)
        },
        shape = RoundedCornerShape(10.dp),
        modifier = Modifier.size(56.dp),
    ) {
        val favicon = visual?.favicon
        if (favicon != null) {
            Image(
                bitmap = favicon,
                contentDescription = "사이트 아이콘",
                contentScale = ContentScale.Fit,
                modifier = Modifier.padding(14.dp),
            )
        } else {
            Icon(
                Icons.Outlined.Public,
                contentDescription = null,
                tint = if (active) MaterialTheme.colorScheme.primary else denebHint(),
                modifier = Modifier.padding(17.dp),
            )
        }
    }
}
