package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.ScrollableTabRow
import androidx.compose.material3.Surface
import androidx.compose.material3.Tab
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
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
import com.inspiredandroid.kai.data.AppSettings
import kotlinx.coroutines.launch

/**
 * Deneb hub + settings as a tabbed screen (the "더보기" surface): gateway config
 * plus the secondary surfaces — model, people, cron, topic docs — each as its
 * own tab so they live here instead of crowding the chat top bar.
 */
@Composable
fun DenebConfigScreen(
    appSettings: AppSettings,
    onBack: () -> Unit,
    denebClient: DenebGatewayClient? = null,
    onOpenPerson: (String) -> Unit = {},
    onOpenTopicDoc: (String) -> Unit = {},
    onOpenKaiSettings: () -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var tab by remember { mutableStateOf(0) }
    val tabs = listOf("게이트웨이", "모델", "사람", "크론", "토픽문서")

    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(Modifier.fillMaxSize().statusBarsPadding()) {
            if (navigationTabBar != null) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) { navigationTabBar() }
            }
            Row(
                modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp, top = 12.dp, bottom = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text("설정", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
                TextButton(onClick = onBack) { Text("닫기") }
            }
            ScrollableTabRow(selectedTabIndex = tab, edgePadding = 16.dp) {
                tabs.forEachIndexed { i, label ->
                    Tab(selected = tab == i, onClick = { tab = i }, text = { Text(label) })
                }
            }
            Box(Modifier.weight(1f).fillMaxWidth()) {
                when (tab) {
                    0 -> GatewayTab(appSettings, onBack, onOpenKaiSettings)
                    1 -> denebClient?.let { ModelTab(it) }
                    2 -> denebClient?.let { PeopleTab(it, onOpenPerson) }
                    3 -> denebClient?.let { CronTab(it) }
                    4 -> denebClient?.let { TopicDocsTab(it, onOpenTopicDoc) }
                }
            }
        }
    }
}

@Composable
private fun GatewayTab(appSettings: AppSettings, onBack: () -> Unit, onOpenKaiSettings: () -> Unit) {
    var url by remember { mutableStateOf(appSettings.settings.getString(KEY_URL, "")) }
    var token by remember { mutableStateOf(appSettings.settings.getString(KEY_TOKEN, "")) }
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp)) {
        OutlinedTextField(
            value = url,
            onValueChange = { url = it },
            label = { Text("Gateway URL") },
            placeholder = { Text("http://100.x.x.x:18789") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        Spacer(Modifier.height(12.dp))
        OutlinedTextField(
            value = token,
            onValueChange = { token = it },
            label = { Text("Client Token") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        Spacer(Modifier.height(8.dp))
        Text(
            "게이트웨이 호스트에서 deneb-client-token으로 생성한 값을 붙여넣으세요.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(20.dp))
        Button(
            onClick = {
                appSettings.settings.putString(KEY_URL, url.trim())
                appSettings.settings.putString(KEY_TOKEN, token.trim())
                onBack()
            },
            modifier = Modifier.fillMaxWidth(),
        ) { Text("저장") }
        Spacer(Modifier.height(24.dp))
        TextButton(onClick = onOpenKaiSettings) { Text("고급 설정 (Kai)") }
    }
}

@Composable
private fun ModelTab(client: DenebGatewayClient) {
    val models by client.denebModels.collectAsState()
    val scope = rememberCoroutineScope()
    LaunchedEffect(Unit) { client.refreshModels() }
    if (models.isEmpty()) {
        DenebLoading()
        return
    }
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp)) {
        Text(
            "기본 채팅 모델 — 모든 채널에 공통 적용됩니다.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(8.dp))
        models.forEach { model ->
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clickable(enabled = !model.current) { scope.launch { client.setMainModel(model.id) } }
                    .padding(vertical = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    if (model.current) "● " else "○ ",
                    color = if (model.current) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Column(Modifier.weight(1f)) {
                    Text(model.display, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
                    Text(
                        model.id + if (model.health.equals("offline", ignoreCase = true)) "  ·  오프라인" else "",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

@Composable
private fun PeopleTab(client: DenebGatewayClient, onOpenPerson: (String) -> Unit) {
    var people by remember { mutableStateOf<List<PersonHit>?>(null) }
    LaunchedEffect(Unit) { people = client.fetchPeople() }
    val list = people
    when {
        list == null -> DenebLoading()
        list.isEmpty() -> EmptyTab("최근 연락이 없습니다.")
        else -> LazyColumn(Modifier.fillMaxSize()) {
            items(list, key = { it.email.ifBlank { it.name } }) { person ->
                Column(
                    Modifier.fillMaxWidth().clickable { onOpenPerson(person.email.ifBlank { person.name }) }
                        .padding(horizontal = 16.dp, vertical = 12.dp),
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(
                            person.name.ifBlank { "(이름 없음)" },
                            style = MaterialTheme.typography.bodyLarge,
                            fontWeight = FontWeight.Medium,
                            color = MaterialTheme.colorScheme.onSurface,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f),
                        )
                        Text("${person.messageCount}통", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary)
                    }
                    if (person.lastSubject.isNotBlank()) {
                        Text(person.lastSubject, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                }
                HorizontalDivider(Modifier.padding(start = 16.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
            }
        }
    }
}

@Composable
private fun CronTab(client: DenebGatewayClient) {
    val crons by client.denebScheduledTasks.collectAsState()
    val scope = rememberCoroutineScope()
    LaunchedEffect(Unit) { client.getScheduledTasks() }
    if (crons.isEmpty()) {
        EmptyTab("예약된 작업이 없습니다.")
        return
    }
    LazyColumn(Modifier.fillMaxSize()) {
        items(crons, key = { it.id }) { cron ->
            Row(
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(Modifier.weight(1f)) {
                    Text(
                        cron.description.ifBlank { cron.id },
                        style = MaterialTheme.typography.bodyLarge,
                        fontWeight = FontWeight.Medium,
                        color = MaterialTheme.colorScheme.onSurface,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    cron.cron?.let {
                        Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                TextButton(onClick = { scope.launch { client.runCron(cron.id) } }) { Text("실행") }
                TextButton(onClick = { scope.launch { client.cancelScheduledTask(cron.id) } }) { Text("삭제") }
            }
            HorizontalDivider(Modifier.padding(start = 16.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
        }
    }
}

@Composable
private fun TopicDocsTab(client: DenebGatewayClient, onOpenTopicDoc: (String) -> Unit) {
    var docs by remember { mutableStateOf<List<TopicDocFile>?>(null) }
    LaunchedEffect(Unit) { docs = client.fetchTopicDocs() }
    Column(Modifier.fillMaxSize()) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp, top = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                "토픽별 주입 문서",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.weight(1f),
            )
            TextButton(onClick = { onOpenTopicDoc("") }) { Text("+ 새 문서") }
        }
        TopicDocsList(docs, onOpenTopicDoc)
    }
}

@Composable
private fun TopicDocsList(list: List<TopicDocFile>?, onOpenTopicDoc: (String) -> Unit) {
    when {
        list == null -> DenebLoading()
        list.isEmpty() -> EmptyTab("토픽 문서가 없습니다.")
        else -> LazyColumn(Modifier.fillMaxSize()) {
            items(list, key = { it.name }) { doc ->
                Row(
                    modifier = Modifier.fillMaxWidth().clickable { onOpenTopicDoc(doc.name) }.padding(horizontal = 16.dp, vertical = 14.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        doc.name,
                        style = MaterialTheme.typography.bodyLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.weight(1f),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    if (doc.modified.isNotBlank()) {
                        Text(doc.modified.take(10), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                HorizontalDivider(Modifier.padding(start = 16.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
            }
        }
    }
}

@Composable
private fun EmptyTab(text: String) {
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        Text(text, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

private const val KEY_URL = "deneb.gatewayUrl"
private const val KEY_TOKEN = "deneb.clientToken"
