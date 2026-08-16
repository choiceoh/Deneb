package ai.deneb.deneb

import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.icons.outlined.Public
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun BrowserTabsSheet(
    store: BrowserTabStore,
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
                        "${store.tabs.size} / $BROWSER_TAB_LIMIT",
                        style = DenebType.meta,
                        color = denebHint(),
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
                        Icon(
                            Icons.Outlined.Public,
                            contentDescription = null,
                            tint = if (active) MaterialTheme.colorScheme.primary else denebHint(),
                            modifier = Modifier.size(22.dp),
                        )
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
                    HorizontalDivider(color = denebHairline(), modifier = Modifier.padding(start = 58.dp))
                }
            }
        }
    }
}
