package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.data.AppSettings
import com.inspiredandroid.kai.data.NotificationRecord
import com.inspiredandroid.kai.data.NotificationStore
import com.inspiredandroid.kai.tools.NotificationListenerController
import com.inspiredandroid.kai.ui.components.rememberHaptics
import com.inspiredandroid.kai.ui.handCursor
import com.inspiredandroid.kai.ui.settings.SettingsCard
import kotlinx.coroutines.launch
import org.koin.compose.koinInject

/**
 * Deneb hub + settings as a tabbed screen (the "더보기" surface): gateway config
 * plus the secondary surfaces — model, cron, topic docs — each as its
 * own tab so they live here instead of crowding the chat top bar.
 *
 * People browsing is NOT a tab here: it is a content destination with its own
 * drawer entry + full screen (DenebPeopleScreen), like mail and calendar. The
 * settings hub stays configuration-only.
 */
@Composable
fun DenebConfigScreen(
    appSettings: AppSettings,
    onBack: () -> Unit,
    denebClient: DenebGatewayClient? = null,
    onOpenTopicDoc: (String) -> Unit = {},
    onOpenCron: (String) -> Unit = {},
    onOpenKaiSettings: () -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var tab by remember { mutableStateOf(0) }
    val haptics = rememberHaptics()
    val tabs = listOf("게이트웨이", "모델", "크론", "토픽문서", "알림")

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
            // Pill-style tabs (no underline) — mirrors the Kai "고급 설정" tab selector:
            // each tab is a rounded Surface, the selected one gets a soft primary tint.
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .horizontalScroll(rememberScrollState())
                    .padding(horizontal = 12.dp, vertical = 4.dp),
                horizontalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                tabs.forEachIndexed { i, label ->
                    val isSelected = tab == i
                    Surface(
                        modifier = Modifier
                            .handCursor()
                            .clip(RoundedCornerShape(50))
                            .clickable { haptics.tap(); tab = i },
                        shape = RoundedCornerShape(50),
                        color = if (isSelected) {
                            MaterialTheme.colorScheme.primary.copy(alpha = 0.2f)
                        } else {
                            Color.Transparent
                        },
                    ) {
                        Text(
                            text = label,
                            modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                            color = MaterialTheme.colorScheme.primary,
                            style = MaterialTheme.typography.labelLarge,
                            maxLines = 1,
                        )
                    }
                }
            }
            Box(Modifier.weight(1f).fillMaxWidth()) {
                when (tab) {
                    0 -> GatewayTab(appSettings, onBack, onOpenKaiSettings, denebClient)
                    1 -> denebClient?.let { ModelTab(it) }
                    2 -> denebClient?.let { CronTab(it, onOpenCron) }
                    3 -> denebClient?.let { TopicDocsTab(it, onOpenTopicDoc) }
                    4 -> denebClient?.let { NotificationsTab(it) }
                }
            }
        }
    }
}

@Composable
private fun GatewayTab(
    appSettings: AppSettings,
    onBack: () -> Unit,
    onOpenKaiSettings: () -> Unit,
    denebClient: DenebGatewayClient?,
) {
    var url by remember { mutableStateOf(appSettings.settings.getString(KEY_URL, "")) }
    var token by remember { mutableStateOf(appSettings.settings.getString(KEY_TOKEN, "")) }
    val scope = rememberCoroutineScope()
    val uriHandler = LocalUriHandler.current
    var checking by remember { mutableStateOf(false) }
    var checked by remember { mutableStateOf(false) }
    var update by remember { mutableStateOf<UpdateInfo?>(null) }
    var showPatchNotes by remember { mutableStateOf(false) }
    Column(
        modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        SettingsCard {
            Text(
                "게이트웨이 연결",
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
                color = MaterialTheme.colorScheme.onBackground,
            )
            Spacer(Modifier.height(12.dp))
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
            Spacer(Modifier.height(16.dp))
            Button(
                onClick = {
                    appSettings.settings.putString(KEY_URL, url.trim())
                    appSettings.settings.putString(KEY_TOKEN, token.trim())
                    onBack()
                },
                modifier = Modifier.fillMaxWidth(),
            ) { Text("저장") }
        }
        SettingsCard(onClick = onOpenKaiSettings) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(
                        "고급 설정 (Kai)",
                        style = MaterialTheme.typography.titleMedium,
                        color = MaterialTheme.colorScheme.onBackground,
                    )
                    Text(
                        "제공자 · MCP · 추론 등 Kai 원본 설정",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Text(
                    "›",
                    style = MaterialTheme.typography.titleLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        SettingsCard {
            Text(
                "버전",
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
                color = MaterialTheme.colorScheme.onBackground,
            )
            Spacer(Modifier.height(8.dp))
            Text(
                "현재 v$DENEB_VERSION_NAME ($DENEB_VERSION_CODE)",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Spacer(Modifier.height(4.dp))
            Text(
                "패치노트 보기",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.primary,
                modifier = Modifier.handCursor().clickable { showPatchNotes = true },
            )
            val info = update
            if (info != null) {
                Spacer(Modifier.height(12.dp))
                Text(
                    "새 버전 v${info.versionName} 사용 가능",
                    style = MaterialTheme.typography.bodyMedium,
                    fontWeight = FontWeight.SemiBold,
                    color = MaterialTheme.colorScheme.primary,
                )
                if (info.notes.isNotBlank()) {
                    Text(
                        info.notes,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Spacer(Modifier.height(8.dp))
                Button(onClick = { uriHandler.openUri(info.apkUrl) }, modifier = Modifier.fillMaxWidth()) {
                    Text("다운로드 후 설치")
                }
            } else if (checked && !checking) {
                Spacer(Modifier.height(8.dp))
                Text(
                    "최신 버전입니다.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Spacer(Modifier.height(12.dp))
            OutlinedButton(
                onClick = {
                    val c = denebClient ?: return@OutlinedButton
                    scope.launch {
                        checking = true
                        update = c.checkUpdate()
                        checked = true
                        checking = false
                    }
                },
                enabled = !checking && denebClient != null,
                modifier = Modifier.fillMaxWidth(),
            ) { Text(if (checking) "확인 중…" else "업데이트 확인") }
        }
    }
    if (showPatchNotes) {
        PatchNotesSheet(onDismiss = { showPatchNotes = false })
    }
}

/**
 * Bottom sheet listing the compiled-in [DENEB_PATCH_NOTES], newest first, with the
 * running build flagged. Opened from the "버전" card — no auto-popup on update.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun PatchNotesSheet(onDismiss: () -> Unit) {
    ModalBottomSheet(
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        onDismissRequest = onDismiss,
    ) {
        LazyColumn(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 20.dp)
                .padding(bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(20.dp),
        ) {
            item {
                Text(
                    "패치노트",
                    style = MaterialTheme.typography.headlineSmall,
                    fontWeight = FontWeight.SemiBold,
                    color = MaterialTheme.colorScheme.onSurface,
                )
            }
            items(DENEB_PATCH_NOTES) { note ->
                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(
                            "v${note.version}",
                            style = MaterialTheme.typography.titleMedium,
                            fontWeight = FontWeight.SemiBold,
                            color = MaterialTheme.colorScheme.primary,
                        )
                        if (note.code == DENEB_VERSION_CODE) {
                            Spacer(Modifier.width(8.dp))
                            Text(
                                "현재 버전",
                                style = MaterialTheme.typography.labelSmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                    note.highlights.forEach { line ->
                        Row {
                            Text(
                                "· ",
                                style = MaterialTheme.typography.bodyMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                            Text(
                                line,
                                style = MaterialTheme.typography.bodyMedium,
                                color = MaterialTheme.colorScheme.onSurface,
                            )
                        }
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ModelTab(client: DenebGatewayClient) {
    val models by client.denebModels.collectAsState()
    val roleModels by client.denebRoleModels.collectAsState()
    val scope = rememberCoroutineScope()
    var role by remember { mutableStateOf("main") }
    LaunchedEffect(Unit) { client.refreshModels() }
    if (models.isEmpty()) {
        DenebLoading()
        return
    }
    val roleLabels = listOf("main" to "메인", "lightweight" to "경량", "fallback" to "폴백")
    val currentForRole = roleModels[role]
    Column(
        modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(
            "역할별 모델 — 메인=채팅, 경량=메일 분석·요약, 폴백=메인 실패 시",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
            roleLabels.forEachIndexed { i, (key, label) ->
                SegmentedButton(
                    selected = role == key,
                    onClick = { role = key },
                    shape = SegmentedButtonDefaults.itemShape(i, roleLabels.size),
                ) { Text(label) }
            }
        }
        if (currentForRole != null) {
            Text(
                "현재: $currentForRole",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.primary,
            )
        }
        SettingsCard(innerPadding = false) {
            models.forEachIndexed { i, model ->
                val isCurrent = model.id == currentForRole
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable(enabled = !isCurrent) { scope.launch { client.setRoleModel(model.id, role) } }
                        .padding(horizontal = 16.dp, vertical = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        if (isCurrent) "● " else "○ ",
                        color = if (isCurrent) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
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
                if (i < models.lastIndex) {
                    HorizontalDivider(
                        Modifier.padding(start = 16.dp),
                        color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f),
                    )
                }
            }
        }
    }
}

@Composable
private fun CronTab(client: DenebGatewayClient, onOpenCron: (String) -> Unit) {
    val crons by client.denebScheduledTasks.collectAsState()
    LaunchedEffect(Unit) { client.getScheduledTasks() }
    if (crons.isEmpty()) {
        EmptyTab("예약된 작업이 없습니다.")
        return
    }
    LazyColumn(Modifier.fillMaxSize()) {
        items(crons, key = { it.id }) { cron ->
            Column(
                Modifier.animateItem().fillMaxWidth().clickable { onOpenCron(cron.id) }.padding(horizontal = 16.dp, vertical = 14.dp),
            ) {
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
            HorizontalDivider(Modifier.padding(start = 16.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
        }
    }
}

// Android-native: list other apps' notifications the listener captured and tap
// one to send it into the Deneb chat for triage. Cross-platform-safe — non-FOSS
// / non-Android builds report unsupported and show a hint.
@Composable
private fun NotificationsTab(client: DenebGatewayClient) {
    val controller = koinInject<NotificationListenerController>()
    val store = koinInject<NotificationStore>()
    val appSettings = koinInject<AppSettings>()
    val scope = rememberCoroutineScope()
    var access by remember { mutableStateOf(controller.isAccessGranted()) }
    var records by remember { mutableStateOf<List<NotificationRecord>>(emptyList()) }
    var sentId by remember { mutableStateOf<String?>(null) }
    // Capture allowlist: empty ⇒ all apps (default). Toggling a chip narrows it.
    var allowlist by remember { mutableStateOf(appSettings.getNotificationCaptureAllowlist()) }
    LaunchedEffect(access) {
        if (access) records = store.getStore().sortedByDescending { it.postedAtEpochMs }
    }
    // Apps seen in capture history, for the picker (packageName → label).
    val knownApps = remember(records) {
        records.associate { it.packageName to it.appLabel.ifBlank { it.packageName } }
            .toList().sortedBy { it.second.lowercase() }
    }
    when {
        !controller.isSupported() -> EmptyTab("이 빌드는 알림 캡처를 지원하지 않습니다.")
        !access -> Column(
            Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text(
                "타 앱 알림(카톡·메일·캘린더 등)을 Deneb가 읽으려면 '알림 접근' 권한이 필요합니다. Telegram이 못 하는 네이티브 기능입니다.",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Button(onClick = { controller.openAccessSettings() }, modifier = Modifier.fillMaxWidth()) { Text("알림 접근 권한 열기") }
            OutlinedButton(onClick = { access = controller.isAccessGranted() }, modifier = Modifier.fillMaxWidth()) { Text("권한 부여 후 새로고침") }
        }
        records.isEmpty() -> EmptyTab("캡처된 알림이 없습니다.")
        else -> Column(
            Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            // Capture-allowlist picker: choose which apps Deneb captures. Empty
            // = all apps. Listed apps are those seen in capture history.
            if (knownApps.isNotEmpty()) {
                SettingsCard {
                    Text(
                        "캡처할 앱",
                        style = MaterialTheme.typography.titleSmall,
                        fontWeight = FontWeight.SemiBold,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Text(
                        if (allowlist.isEmpty()) {
                            "모든 앱의 알림을 받습니다. 아래에서 특정 앱만 고르면 그 앱만 받습니다."
                        } else {
                            "선택한 ${allowlist.size}개 앱의 알림만 받습니다."
                        },
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Spacer(Modifier.height(4.dp))
                    knownApps.forEach { (pkg, label) ->
                        val on = allowlist.isEmpty() || pkg in allowlist
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clickable {
                                    // Toggle: from "all" (empty) the first tap selects
                                    // just this app; thereafter add/remove from the set.
                                    val base = if (allowlist.isEmpty()) knownApps.map { it.first }.toSet() else allowlist
                                    val next = if (pkg in base) base - pkg else base + pkg
                                    // Selecting every known app collapses back to "all".
                                    allowlist = if (next.size == knownApps.size) emptySet() else next
                                    appSettings.setNotificationCaptureAllowlist(allowlist)
                                }
                                .padding(vertical = 10.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(
                                if (on) "☑" else "☐",
                                modifier = Modifier.padding(end = 10.dp),
                                color = if (on) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                            Text(
                                label,
                                style = MaterialTheme.typography.bodyMedium,
                                color = MaterialTheme.colorScheme.onSurface,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                                modifier = Modifier.weight(1f),
                            )
                        }
                    }
                    if (allowlist.isNotEmpty()) {
                        Spacer(Modifier.height(4.dp))
                        TextButton(onClick = {
                            allowlist = emptySet()
                            appSettings.setNotificationCaptureAllowlist(allowlist)
                        }) { Text("모든 앱 받기로 초기화") }
                    }
                }
                Spacer(Modifier.height(4.dp))
            }
            Text(
                "탭하면 Deneb 채팅으로 보내 트리아지합니다.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            records.forEach { rec ->
                SettingsCard(
                    onClick = {
                        scope.launch {
                            // A captured BigPicture image rides the OCR capture
                            // path; text-only notifications send as text. The
                            // bytes live in the in-memory cache, so a record that
                            // outlived them (process restart) falls back to text.
                            val image = if (rec.hasImage) store.getImage(rec.id) else null
                            if (image != null) {
                                client.captureImage(image, "image/jpeg", caption = "📲 ${rec.appLabel} — ${rec.title}".trim())
                            } else {
                                client.ask("📲 ${rec.appLabel} 알림 — ${rec.title}\n${rec.text}".trim(), emptyList(), null)
                            }
                            sentId = rec.id
                        }
                    },
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Column(Modifier.weight(1f)) {
                            Text(
                                rec.appLabel + if (rec.title.isNotBlank()) "  ·  ${rec.title}" else "",
                                style = MaterialTheme.typography.bodyMedium,
                                fontWeight = FontWeight.SemiBold,
                                color = MaterialTheme.colorScheme.onSurface,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                            if (rec.text.isNotBlank()) {
                                Text(
                                    rec.text,
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    maxLines = 2,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                        }
                        if (sentId == rec.id) {
                            Text("✓ 보냄", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.primary)
                        }
                    }
                }
            }
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
                    modifier = Modifier.animateItem().fillMaxWidth().clickable { onOpenTopicDoc(doc.name) }.padding(horizontal = 16.dp, vertical = 14.dp),
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
