package com.inspiredandroid.kai.deneb

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
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.components.rememberHaptics
import kotlinx.coroutines.launch

/**
 * Topic doc surface (`miniapp.topicdocs.*`). Blank [name] enters create mode;
 * otherwise reads the file with a view (markdown) / edit toggle.
 */
@Composable
fun DenebTopicDocScreen(
    client: DenebGatewayClient,
    name: String,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val creating = name.isBlank()
    var doc by remember(name) { mutableStateOf<TopicDocContent?>(null) }
    var loadFailed by remember(name) { mutableStateOf(false) }
    var editing by remember(name) { mutableStateOf(creating) }
    var draftName by remember(name) { mutableStateOf("") }
    var draft by remember(name) { mutableStateOf("") }
    var saving by remember(name) { mutableStateOf(false) }
    var status by remember(name) { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()

    suspend fun loadDoc() {
        if (creating) return
        loadFailed = false
        doc = null
        val d = client.readTopicDoc(name)
        doc = d
        loadFailed = d == null
        if (d != null) draft = d.content
    }
    LaunchedEffect(name) { loadDoc() }

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

            val d = doc
            if (creating) {
                Text(
                    "새 토픽 문서",
                    style = MaterialTheme.typography.titleLarge,
                    fontWeight = FontWeight.SemiBold,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Spacer(Modifier.height(8.dp))
                OutlinedTextField(
                    value = draftName,
                    onValueChange = { draftName = it },
                    label = { Text("파일명 (예: projectx.md)") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
            } else if (d == null) {
                if (loadFailed) {
                    DenebError("문서를 불러오지 못했습니다.", onRetry = { scope.launch { loadDoc() } })
                } else {
                    DenebLoading()
                }
                return@Column
            } else {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        d.name,
                        style = MaterialTheme.typography.titleLarge,
                        fontWeight = FontWeight.SemiBold,
                        color = MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.weight(1f),
                    )
                    TextButton(onClick = { haptics.tap(); editing = !editing; if (!editing) draft = d.content; status = null }) {
                        Text(if (editing) "취소" else "편집")
                    }
                }
                if (d.modified.isNotBlank()) {
                    Text(
                        d.modified.take(10),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }

            Spacer(Modifier.height(12.dp))
            HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
            Spacer(Modifier.height(12.dp))

            if (editing) {
                OutlinedTextField(
                    value = draft,
                    onValueChange = { draft = it },
                    label = { Text("내용 (마크다운)") },
                    modifier = Modifier.fillMaxWidth().height(360.dp),
                )
                Spacer(Modifier.height(12.dp))
                Button(
                    enabled = !saving && (!creating || draftName.isNotBlank()),
                    onClick = {
                        haptics.tap()
                        scope.launch {
                            saving = true
                            status = null
                            val target = if (creating) draftName.trim() else (d?.name ?: name)
                            val ok = client.saveTopicDoc(target, draft, creating)
                            saving = false
                            if (ok) {
                                haptics.confirm()
                                if (creating) {
                                    onBack()
                                } else {
                                    editing = false
                                    status = "저장됨"
                                    doc = client.readTopicDoc(target)
                                }
                            } else {
                                status = "저장 실패"
                            }
                        }
                    },
                ) { Text(if (saving) "저장 중…" else "저장") }
                status?.let {
                    Spacer(Modifier.height(8.dp))
                    Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            } else if (d != null) {
                DenebMarkdown(d.content.ifBlank { "(빈 문서)" })
            }
            Spacer(Modifier.height(24.dp))
        }
    }
}
