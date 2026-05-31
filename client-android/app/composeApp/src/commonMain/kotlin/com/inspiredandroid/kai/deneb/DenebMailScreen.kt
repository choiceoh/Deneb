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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/**
 * Native inbox triage backed by `miniapp.gmail.list_recent`, ported from the
 * Mini App's list.ts: long-press a row to enter multi-select, a bottom bar
 * runs bulk read / archive / trash, and "더 보기" pages through nextPageToken.
 * Tapping a row (when not selecting) opens the detail screen.
 */
@OptIn(ExperimentalFoundationApi::class)
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

    var loaded by remember { mutableStateOf(false) }
    var selecting by remember { mutableStateOf(false) }
    val selected = remember { mutableStateListOf<String>() }
    var busy by remember { mutableStateOf(false) }
    var loadingMore by remember { mutableStateOf(false) }

    LaunchedEffect(Unit) {
        client.refreshMail()
        loaded = true
    }

    fun clearSelection() {
        selecting = false
        selected.clear()
    }

    fun bulk(action: suspend (String) -> Unit) {
        if (busy) return
        val ids = selected.toList()
        scope.launch {
            busy = true
            ids.forEach { action(it) }
            busy = false
            clearSelection()
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .statusBarsPadding()
            .padding(horizontal = 16.dp),
    ) {
        if (navigationTabBar != null) {
            Spacer(Modifier.height(8.dp))
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) { navigationTabBar() }
        }
        Spacer(Modifier.height(12.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            if (selecting) {
                TextButton(onClick = { clearSelection() }) { Text("취소") }
                Text(
                    "${selected.size}개 선택",
                    style = MaterialTheme.typography.titleMedium,
                    modifier = Modifier.weight(1f).padding(start = 8.dp),
                )
            } else {
                Text(
                    "받은 메일",
                    style = MaterialTheme.typography.headlineSmall,
                    modifier = Modifier.weight(1f),
                )
                TextButton(onClick = onBack) { Text("닫기") }
            }
        }
        Spacer(Modifier.height(8.dp))

        Box(Modifier.weight(1f).fillMaxWidth()) {
            if (!loaded && mail.isEmpty()) {
                DenebLoading()
            } else if (mail.isEmpty()) {
                Text(
                    "최근 7일 메일 없음",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            } else {
                LazyColumn(Modifier.fillMaxSize()) {
                    items(mail, key = { it.id }) { m ->
                        MailRow(
                            message = m,
                            selecting = selecting,
                            isSelected = m.id in selected,
                            onClick = {
                                if (selecting) {
                                    if (m.id in selected) selected.remove(m.id) else selected.add(m.id)
                                    if (selected.isEmpty()) selecting = false
                                } else {
                                    onOpenDetail(m.id)
                                }
                            },
                            onLongClick = {
                                selecting = true
                                if (m.id !in selected) selected.add(m.id)
                            },
                        )
                        HorizontalDivider()
                    }
                    if (nextToken != null) {
                        item {
                            Box(
                                Modifier.fillMaxWidth().padding(vertical = 14.dp),
                                contentAlignment = Alignment.Center,
                            ) {
                                if (loadingMore) {
                                    CircularProgressIndicator(Modifier.size(22.dp))
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

        if (selecting && selected.isNotEmpty()) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .background(MaterialTheme.colorScheme.surfaceContainerHigh)
                    .padding(horizontal = 8.dp, vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    "${selected.size}개",
                    style = MaterialTheme.typography.bodyMedium,
                    modifier = Modifier.padding(start = 8.dp),
                )
                Spacer(Modifier.weight(1f))
                TextButton(onClick = { bulk { client.markMailRead(it) } }, enabled = !busy) { Text("읽음") }
                TextButton(onClick = { bulk { client.archiveMail(it) } }, enabled = !busy) { Text("보관") }
                TextButton(onClick = { bulk { client.trashMail(it) } }, enabled = !busy) { Text("휴지통") }
            }
        }
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun MailRow(
    message: MailMessage,
    selecting: Boolean,
    isSelected: Boolean,
    onClick: () -> Unit,
    onLongClick: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .combinedClickable(onClick = onClick, onLongClick = onLongClick)
            .background(
                if (isSelected) {
                    MaterialTheme.colorScheme.primaryContainer
                } else {
                    androidx.compose.ui.graphics.Color.Transparent
                },
            )
            .padding(vertical = 10.dp),
        verticalAlignment = Alignment.Top,
    ) {
        when {
            selecting -> Text(
                if (isSelected) "☑ " else "☐ ",
                color = MaterialTheme.colorScheme.primary,
            )
            message.unread -> Text("● ", color = MaterialTheme.colorScheme.primary)
        }
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    senderName(message.from),
                    style = MaterialTheme.typography.bodyMedium,
                    fontWeight = if (message.unread) FontWeight.Bold else FontWeight.Normal,
                    maxLines = 1,
                    modifier = Modifier.weight(1f),
                )
                Text(
                    shortDate(message.date),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Spacer(Modifier.height(2.dp))
            Text(
                message.subject.ifBlank { "(제목 없음)" },
                style = MaterialTheme.typography.bodyLarge,
                fontWeight = if (message.unread) FontWeight.SemiBold else FontWeight.Normal,
                maxLines = 1,
            )
            if (message.snippet.isNotBlank()) {
                Text(
                    message.snippet,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 2,
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
