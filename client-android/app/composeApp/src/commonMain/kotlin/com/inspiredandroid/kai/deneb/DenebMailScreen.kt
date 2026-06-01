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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.components.rememberHaptics
import kotlinx.coroutines.launch

/**
 * Native inbox triage backed by `miniapp.gmail.list_recent`. Pull to refresh;
 * long-press a row (with haptic) to multi-select; a tonal bottom bar runs bulk
 * read / archive / trash, and "더 보기" pages through nextPageToken. Tapping a
 * row opens the detail screen.
 */
@OptIn(ExperimentalFoundationApi::class, ExperimentalMaterial3Api::class)
@Composable
fun DenebMailScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onOpenDetail: (String) -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val mail by client.denebMail.collectAsState()
    val nextToken by client.denebMailNextToken.collectAsState()
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()

    // null = first load in flight, true = loaded ok, false = fetch failed.
    var loadOk by remember { mutableStateOf<Boolean?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    var selecting by remember { mutableStateOf(false) }
    val selected = remember { mutableStateListOf<String>() }
    var busy by remember { mutableStateOf(false) }
    var loadingMore by remember { mutableStateOf(false) }

    LaunchedEffect(Unit) { loadOk = client.refreshMail() }

    fun clearSelection() {
        selecting = false
        selected.clear()
    }

    fun bulk(action: suspend (String) -> Unit) {
        if (busy) return
        haptics.confirm()
        val ids = selected.toList()
        scope.launch {
            busy = true
            ids.forEach { action(it) }
            busy = false
            clearSelection()
        }
    }

    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(Modifier.fillMaxSize().statusBarsPadding()) {
            if (navigationTabBar != null) {
                Spacer(Modifier.height(8.dp))
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) { navigationTabBar() }
            }
            Row(
                modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp, top = 12.dp, bottom = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (selecting) {
                    Text(
                        "${selected.size}개 선택",
                        style = MaterialTheme.typography.titleLarge,
                        fontWeight = FontWeight.SemiBold,
                        modifier = Modifier.weight(1f),
                    )
                    TextButton(onClick = { clearSelection() }) { Text("취소") }
                } else {
                    Column(Modifier.weight(1f)) {
                        Text("받은 메일", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.SemiBold)
                        if (mail.isNotEmpty()) {
                            Text(
                                "${mail.size}통 · 안 읽음 ${mail.count { it.unread }}",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                    TextButton(onClick = onBack) { Text("닫기") }
                }
            }

            Box(Modifier.weight(1f).fillMaxWidth()) {
                if (mail.isEmpty() && loadOk == null) {
                    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { DenebLoading() }
                } else if (mail.isEmpty() && loadOk == false) {
                    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                        DenebError(
                            "메일을 불러오지 못했어요.",
                            onRetry = { scope.launch { loadOk = null; loadOk = client.refreshMail() } },
                        )
                    }
                } else if (mail.isEmpty()) {
                    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                        Text("최근 7일 메일 없음", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                } else {
                    PullToRefreshBox(
                        isRefreshing = refreshing,
                        onRefresh = { scope.launch { refreshing = true; loadOk = client.refreshMail(); refreshing = false } },
                        modifier = Modifier.fillMaxSize(),
                    ) {
                        LazyColumn(Modifier.fillMaxSize()) {
                            items(mail, key = { it.id }) { m ->
                                Column(Modifier.animateItem()) {
                                    MailRow(
                                        message = m,
                                        selecting = selecting,
                                        isSelected = m.id in selected,
                                        onTap = {
                                            haptics.tap()
                                            if (selecting) {
                                                if (m.id in selected) selected.remove(m.id) else selected.add(m.id)
                                                if (selected.isEmpty()) selecting = false
                                            } else {
                                                onOpenDetail(m.id)
                                            }
                                        },
                                        onLongPress = {
                                            haptics.confirm()
                                            selecting = true
                                            if (m.id !in selected) selected.add(m.id)
                                        },
                                    )
                                    HorizontalDivider(
                                        modifier = Modifier.padding(start = 68.dp),
                                        color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f),
                                    )
                                }
                            }
                            if (nextToken != null) {
                                item {
                                    Box(Modifier.fillMaxWidth().padding(vertical = 14.dp), contentAlignment = Alignment.Center) {
                                        if (loadingMore) {
                                            CircularProgressIndicator(Modifier.size(22.dp), strokeWidth = 2.dp)
                                        } else {
                                            TextButton(onClick = {
                                                scope.launch {
                                                    loadingMore = true
                                                    client.loadMoreMail()
                                                    loadingMore = false
                                                }
                                            }) { Text("더 보기") }
                                        }
                                    }
                                }
                            }
                        }
                    }
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
                        TextButton(onClick = { bulk { client.markMailRead(it) } }, enabled = !busy) { Text("읽음") }
                        TextButton(onClick = { bulk { client.archiveMail(it) } }, enabled = !busy) { Text("보관") }
                        TextButton(onClick = { bulk { client.trashMail(it) } }, enabled = !busy) { Text("휴지통") }
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
internal fun MailRow(
    message: MailMessage,
    selecting: Boolean,
    isSelected: Boolean,
    onTap: () -> Unit,
    onLongPress: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .combinedClickable(onClick = onTap, onLongClick = onLongPress)
            .background(
                if (isSelected) {
                    MaterialTheme.colorScheme.primaryContainer.copy(alpha = 0.5f)
                } else {
                    Color.Transparent
                },
            )
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.Top,
    ) {
        if (selecting) {
            Checkbox(checked = isSelected, onCheckedChange = null, modifier = Modifier.padding(end = 10.dp))
        }
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (message.unread) {
                    Box(Modifier.size(8.dp).clip(CircleShape).background(MaterialTheme.colorScheme.primary))
                    Spacer(Modifier.width(6.dp))
                }
                Text(
                    senderName(message.from).ifBlank { "(발신자 없음)" },
                    style = MaterialTheme.typography.bodyLarge,
                    fontWeight = if (message.unread) FontWeight.Bold else FontWeight.Medium,
                    color = MaterialTheme.colorScheme.onSurface,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                Spacer(Modifier.width(8.dp))
                Text(
                    shortDate(message.date),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Spacer(Modifier.height(3.dp))
            Text(
                message.subject.ifBlank { "(제목 없음)" },
                style = MaterialTheme.typography.bodyMedium,
                fontWeight = if (message.unread) FontWeight.Medium else FontWeight.Normal,
                color = MaterialTheme.colorScheme.onSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (message.snippet.isNotBlank()) {
                Spacer(Modifier.height(2.dp))
                Text(
                    message.snippet,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

/** "Name <email>" -> "Name"; a bare address is returned as-is. */
private fun senderName(from: String): String {
    val lt = from.indexOf('<')
    return if (lt > 0) from.substring(0, lt).trim().trim('"') else from.trim()
}

/** "2026-05-30T12:41:31Z" -> "05-30 12:41". */
private fun shortDate(date: String): String =
    if (date.length >= 16) date.substring(5, 16).replace('T', ' ') else date
