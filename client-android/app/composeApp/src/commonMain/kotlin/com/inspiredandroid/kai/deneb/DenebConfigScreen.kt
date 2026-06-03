package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.background
import androidx.compose.foundation.border
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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
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
import androidx.compose.material3.Switch
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
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.data.AppSettings
import com.inspiredandroid.kai.data.NotificationRecord
import com.inspiredandroid.kai.data.NotificationStore
import com.inspiredandroid.kai.contacts.ContactsReader
import com.inspiredandroid.kai.tools.ContactsPermissionController
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
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var tab by remember { mutableStateOf(ConfigTab.GATEWAY) }
    val haptics = rememberHaptics()

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
                ConfigTab.entries.forEach { entry ->
                    val isSelected = tab == entry
                    Surface(
                        modifier = Modifier
                            .handCursor()
                            .clip(RoundedCornerShape(50))
                            .selectable(
                                selected = isSelected,
                                role = Role.Tab,
                                onClick = { haptics.tap(); tab = entry },
                            ),
                        shape = RoundedCornerShape(50),
                        color = if (isSelected) {
                            MaterialTheme.colorScheme.primary.copy(alpha = 0.2f)
                        } else {
                            Color.Transparent
                        },
                    ) {
                        Text(
                            text = entry.label,
                            modifier = Modifier.padding(horizontal = 16.dp, vertical = 10.dp),
                            color = if (isSelected) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
                            style = MaterialTheme.typography.labelLarge,
                            fontWeight = if (isSelected) FontWeight.SemiBold else FontWeight.Normal,
                            maxLines = 1,
                        )
                    }
                }
            }
            Box(Modifier.weight(1f).fillMaxWidth()) {
                when (tab) {
                    ConfigTab.GATEWAY -> GatewayTab(appSettings, onBack, denebClient)
                    ConfigTab.MODEL -> denebClient?.let { ModelTab(it) }
                    ConfigTab.CRON -> denebClient?.let { CronTab(it, onOpenCron) }
                    ConfigTab.TOPIC_DOCS -> denebClient?.let { TopicDocsTab(it, onOpenTopicDoc) }
                    ConfigTab.NOTIFICATIONS -> denebClient?.let { NotificationsTab(it) }
                }
            }
        }
    }
}

@Composable
private fun GatewayTab(
    appSettings: AppSettings,
    onBack: () -> Unit,
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
    var statusChecking by remember { mutableStateOf(false) }
    val gatewayStatus = if (denebClient != null) denebClient.clientStatus.collectAsState().value else null
    val topics = if (denebClient != null) denebClient.denebTopics.collectAsState().value else emptyList()
    LaunchedEffect(denebClient) {
        denebClient?.refreshClientStatus()
        denebClient?.refreshTopics()
    }
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
                label = { Text("게이트웨이 주소") },
                placeholder = { Text("http://100.x.x.x:18789") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(12.dp))
            OutlinedTextField(
                value = token,
                onValueChange = { token = it },
                label = { Text("클라이언트 토큰") },
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
        GatewayStatusCard(
            status = gatewayStatus,
            topics = topics,
            checking = statusChecking,
            enabled = denebClient != null,
            onRefresh = {
                denebClient?.let { c ->
                    scope.launch {
                        statusChecking = true
                        try {
                            c.refreshClientStatus()
                            c.refreshTopics()
                        } finally {
                            statusChecking = false
                        }
                    }
                }
            },
        )
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
        val contactsPermission = koinInject<ContactsPermissionController>()
        val contactsReader = koinInject<ContactsReader>()
        // Hidden on builds that don't declare READ_CONTACTS (everything but the
        // Android foss flavor): isSupported() probes the merged manifest.
        if (contactsReader.isSupported()) {
            ContactsSyncCard(denebClient, contactsPermission, contactsReader)
        }
    }
    if (showPatchNotes) {
        PatchNotesSheet(onDismiss = { showPatchNotes = false })
    }
}

@Composable
private fun GatewayStatusCard(
    status: ClientStatus?,
    topics: List<ClientTopic>,
    checking: Boolean,
    enabled: Boolean,
    onRefresh: () -> Unit,
) {
    SettingsCard {
        Text(
            "게이트웨이 상태",
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.SemiBold,
            color = MaterialTheme.colorScheme.onBackground,
        )
        Spacer(Modifier.height(8.dp))
        if (status == null) {
            Text(
                "아직 확인되지 않았습니다.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        } else {
            Text(
                "Gateway v${status.version.ifBlank { "unknown" }} · Native API ${status.nativeApiVersion}",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
            if (status.model.isNotBlank()) {
                Spacer(Modifier.height(4.dp))
                Text(
                    "현재 모델: ${status.model}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            val active = status.capabilities
                .filterValues { it }
                .keys
                .sorted()
                .joinToString(" · ")
            if (active.isNotBlank()) {
                Spacer(Modifier.height(8.dp))
                Text(
                    active,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            if (topics.isNotEmpty()) {
                Spacer(Modifier.height(8.dp))
                Text(
                    "토픽: " + topics.take(5).joinToString(" · ") { it.label },
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        Spacer(Modifier.height(12.dp))
        OutlinedButton(
            onClick = onRefresh,
            enabled = enabled && !checking,
            modifier = Modifier.fillMaxWidth(),
        ) { Text(if (checking) "확인 중…" else "상태 새로고침") }
    }
}

/**
 * "주소록 동기화" card: requests READ_CONTACTS, reads the device address book, and
 * ships it to the gateway. The gateway enriches only people already in its wiki
 * (phone/email/org) — it never creates pages — so this sharpens ASR proper-noun
 * bias and "whose number is this?" lookups. The reply lands in the chat transcript.
 */
@Composable
private fun ContactsSyncCard(
    denebClient: DenebGatewayClient?,
    permission: ContactsPermissionController,
    reader: ContactsReader,
) {
    val scope = rememberCoroutineScope()
    var syncing by remember { mutableStateOf(false) }
    var syncMsg by remember { mutableStateOf<String?>(null) }
    SettingsCard {
        Text(
            "주소록 동기화",
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.SemiBold,
            color = MaterialTheme.colorScheme.onBackground,
        )
        Spacer(Modifier.height(8.dp))
        Text(
            "이 기기 연락처 중 위키에 이미 등록된 인물의 전화·이메일·회사를 보강합니다. " +
                "회의 전사 고유명사 교정과 인물 조회에 쓰입니다. 전체 주소록을 새로 저장하지는 않습니다.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(16.dp))
        Button(
            onClick = {
                val c = denebClient ?: return@Button
                scope.launch {
                    syncing = true
                    syncMsg = null
                    val granted = permission.requestPermission()
                    if (!granted) {
                        syncMsg = "연락처 권한이 거부되었습니다."
                        syncing = false
                        return@launch
                    }
                    val contacts = reader.readAll()
                    if (contacts.isEmpty()) {
                        syncMsg = "읽을 연락처가 없습니다."
                        syncing = false
                        return@launch
                    }
                    c.captureContacts(contacts)
                    syncMsg = "${contacts.size}개 연락처를 게이트웨이로 보냈습니다. 결과는 대화에 표시됩니다."
                    syncing = false
                }
            },
            enabled = !syncing && denebClient != null,
            modifier = Modifier.fillMaxWidth(),
        ) { Text(if (syncing) "동기화 중…" else "주소록 동기화") }
        val msg = syncMsg
        if (msg != null) {
            Spacer(Modifier.height(8.dp))
            Text(
                msg,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
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
    val haptics = rememberHaptics()
    var role by remember { mutableStateOf(ModelRole.MAIN) }
    var switching by remember { mutableStateOf(false) }
    var switchFailed by remember { mutableStateOf(false) }
    var addBaseUrl by remember { mutableStateOf("") }
    var addModel by remember { mutableStateOf("") }
    var adding by remember { mutableStateOf(false) }
    var addError by remember { mutableStateOf<String?>(null) }
    var pendingDelete by remember { mutableStateOf<ModelOption?>(null) }
    LaunchedEffect(Unit) { client.refreshModels() }
    if (models.isEmpty()) {
        DenebLoading()
        return
    }
    val currentForRole = roleModels[role.wire]
    Column(
        modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(
            "역할별 모델 — 메인=채팅, 경량=메일 분석·요약, 폴백=메인 실패 시",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        // Legend for the per-model response-status dot.
        Row(
            horizontalArrangement = Arrangement.spacedBy(14.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            HealthLegendItem(ModelHealth.ONLINE, "응답 가능")
            HealthLegendItem(ModelHealth.OFFLINE, "응답 없음")
            HealthLegendItem(ModelHealth.UNKNOWN, "미확인")
        }
        SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
            ModelRole.entries.forEachIndexed { i, r ->
                SegmentedButton(
                    selected = role == r,
                    onClick = { role = r },
                    shape = SegmentedButtonDefaults.itemShape(i, ModelRole.entries.size),
                ) { Text(r.label) }
            }
        }
        if (currentForRole != null) {
            Text(
                "현재: $currentForRole",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.primary,
            )
        }
        if (switchFailed) {
            Text(
                "모델 전환에 실패했어요. 다시 시도해 주세요.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error,
            )
        }
        SettingsCard(innerPadding = false) {
            models.forEachIndexed { i, model ->
                val isCurrent = model.id == currentForRole
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable(enabled = !isCurrent && !switching) {
                            haptics.tap()
                            scope.launch {
                                switching = true
                                switchFailed = !client.setRoleModel(model.id, role.wire)
                                switching = false
                            }
                        }
                        .padding(horizontal = 16.dp, vertical = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    // Color = response status (online/offline/unknown); filled = the
                    // model currently selected for this role, ring = not selected.
                    HealthDot(health = ModelHealth.parse(model.health), selected = isCurrent)
                    Spacer(Modifier.width(12.dp))
                    Column(Modifier.weight(1f)) {
                        Text(
                            model.display,
                            style = MaterialTheme.typography.bodyLarge,
                            fontWeight = if (isCurrent) FontWeight.SemiBold else FontWeight.Normal,
                            color = MaterialTheme.colorScheme.onSurface,
                        )
                        Text(
                            model.id + ModelHealth.parse(model.health).suffix,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    // User-added models can be removed; built-in/role models can't.
                    if (model.custom) {
                        TextButton(
                            onClick = {
                                haptics.tap()
                                pendingDelete = model
                            },
                            enabled = !switching,
                        ) {
                            Text("삭제", color = MaterialTheme.colorScheme.error)
                        }
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

        // Add an OpenAI-compatible endpoint (vLLM / LM Studio / etc.) by base URL
        // + model name. No auth key here — keyed providers go in deneb.json.
        Spacer(Modifier.height(4.dp))
        Text(
            "OpenAI 호환 모델 직접 추가",
            style = MaterialTheme.typography.titleSmall,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Text(
            "Base URL과 모델 이름으로 vLLM·LM Studio 같은 OpenAI 호환 엔드포인트를 추가합니다. 인증 키가 필요 없는 엔드포인트용입니다.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        OutlinedTextField(
            value = addBaseUrl,
            onValueChange = { addBaseUrl = it; addError = null },
            label = { Text("Base URL") },
            placeholder = { Text("http://127.0.0.1:8000/v1") },
            singleLine = true,
            enabled = !adding,
            isError = addError != null,
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(
            value = addModel,
            onValueChange = { addModel = it; addError = null },
            label = { Text("모델 이름") },
            placeholder = { Text("예: qwen2.5-coder-7b") },
            singleLine = true,
            enabled = !adding,
            isError = addError != null,
            modifier = Modifier.fillMaxWidth(),
        )
        addError?.let {
            Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
        }
        Button(
            onClick = {
                haptics.tap()
                scope.launch {
                    adding = true
                    addError = null
                    val ok = client.addCustomModel(addBaseUrl.trim(), addModel.trim())
                    if (ok) {
                        addBaseUrl = ""
                        addModel = ""
                    } else {
                        addError = "추가에 실패했어요. Base URL과 모델 이름을 확인해 주세요."
                    }
                    adding = false
                }
            },
            enabled = !adding && addBaseUrl.isNotBlank() && addModel.isNotBlank(),
            modifier = Modifier.fillMaxWidth(),
        ) {
            Text(if (adding) "추가 중…" else "모델 추가")
        }
    }

    pendingDelete?.let { target ->
        AlertDialog(
            onDismissRequest = { pendingDelete = null },
            title = { Text("모델 삭제") },
            text = {
                Text("'${target.display}' 모델을 목록에서 삭제할까요? 이 모델에 연결된 역할은 기본값으로 되돌아갑니다.")
            },
            confirmButton = {
                TextButton(onClick = {
                    val id = target.id
                    pendingDelete = null
                    scope.launch { client.deleteCustomModel(id) }
                }) { Text("삭제", color = MaterialTheme.colorScheme.error) }
            },
            dismissButton = {
                TextButton(onClick = { pendingDelete = null }) { Text("취소") }
            },
        )
    }
}

// Response-status dot. Color = health (online/offline/unknown); a filled circle
// marks the model currently selected for the role, a ring marks the rest.
@Composable
private fun HealthDot(health: ModelHealth, selected: Boolean) {
    val color = health.color
    val base = Modifier.size(10.dp)
    Box(
        modifier = if (selected) {
            base.clip(CircleShape).background(color)
        } else {
            base.border(1.5.dp, color, CircleShape)
        },
    )
}

@Composable
private fun HealthLegendItem(health: ModelHealth, label: String) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(8.dp).clip(CircleShape).background(health.color))
        Spacer(Modifier.width(5.dp))
        Text(label, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** Model response status. Color: online → green, offline → red, unknown/unprobed → amber.
 *  [suffix] is appended to the model id line. Parsed once from the wire string. */
private enum class ModelHealth(val color: Color, val suffix: String) {
    ONLINE(Color(0xFF4CAF50), ""),
    OFFLINE(Color(0xFFE53935), "  ·  응답 없음"),
    UNKNOWN(Color(0xFFFFB300), "  ·  상태 미확인"),
    ;

    companion object {
        fun parse(health: String): ModelHealth = when (health.lowercase()) {
            "online" -> ONLINE
            "offline" -> OFFLINE
            else -> UNKNOWN
        }
    }
}

@Composable
private fun CronTab(client: DenebGatewayClient, onOpenCron: (String) -> Unit) {
    val crons by client.denebScheduledTasks.collectAsState()
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()
    var loadFailed by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { loadFailed = !client.loadScheduledTasks() }
    when {
        crons.isEmpty() && loadFailed -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            DenebError(
                "예약 작업을 불러오지 못했습니다.",
                onRetry = { scope.launch { loadFailed = !client.loadScheduledTasks() } },
            )
        }
        crons.isEmpty() -> EmptyTab("예약된 작업이 없습니다.")
        else -> LazyColumn(Modifier.fillMaxSize()) {
            items(crons, key = { it.id }) { cron ->
                Column(
                    Modifier.animateItem().fillMaxWidth().clickable { haptics.tap(); onOpenCron(cron.id) }.padding(horizontal = 16.dp, vertical = 14.dp),
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
    val haptics = rememberHaptics()
    var access by remember { mutableStateOf(controller.isAccessGranted()) }
    var records by remember { mutableStateOf<List<NotificationRecord>>(emptyList()) }
    var sentId by remember { mutableStateOf<String?>(null) }
    // Capture allowlist: empty ⇒ all apps (default). Toggling a chip narrows it.
    var allowlist by remember { mutableStateOf(appSettings.getNotificationCaptureAllowlist()) }
    // Auto-inject: on ⇒ a captured notification triages itself immediately;
    // off ⇒ it only queues until the user taps it below (manual injection).
    var autoInject by remember { mutableStateOf(appSettings.isNotificationAutoInjectEnabled()) }
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
                "타 앱 알림(카톡·메일·캘린더 등)을 Deneb가 읽으려면 '알림 접근' 권한이 필요합니다. 네이티브 앱에서만 가능한 기기 통합 기능입니다.",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Button(onClick = { controller.openAccessSettings() }, modifier = Modifier.fillMaxWidth()) { Text("알림 접근 권한 열기") }
            OutlinedButton(onClick = { access = controller.isAccessGranted() }, modifier = Modifier.fillMaxWidth()) { Text("권한 부여 후 새로고침") }
        }
        else -> Column(
            Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            // Auto-inject toggle: shown whenever access is granted, even before
            // any notification is captured.
            SettingsCard {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text(
                            "도착 즉시 주입",
                            style = MaterialTheme.typography.titleSmall,
                            fontWeight = FontWeight.SemiBold,
                            color = MaterialTheme.colorScheme.onSurface,
                        )
                        Text(
                            if (autoInject) {
                                "캡처된 알림을 도착 즉시 에이전트에게 보내 트리아지합니다."
                            } else {
                                "캡처만 하고, 아래 목록에서 직접 탭할 때만 채팅으로 보냅니다."
                            },
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    Switch(
                        checked = autoInject,
                        onCheckedChange = {
                            autoInject = it
                            appSettings.setNotificationAutoInjectEnabled(it)
                        },
                    )
                }
            }
            Spacer(Modifier.height(4.dp))
            if (records.isEmpty()) {
                Text(
                    "캡처된 알림이 없습니다.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
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
                                .toggleable(
                                    value = on,
                                    role = Role.Checkbox,
                                    onValueChange = {
                                        haptics.tap()
                                        // Toggle: from "all" (empty) the first tap selects
                                        // just this app; thereafter add/remove from the set.
                                        val base = if (allowlist.isEmpty()) knownApps.map { it.first }.toSet() else allowlist
                                        val next = if (pkg in base) base - pkg else base + pkg
                                        // Selecting every known app collapses back to "all".
                                        allowlist = if (next.size == knownApps.size) emptySet() else next
                                        appSettings.setNotificationCaptureAllowlist(allowlist)
                                    },
                                )
                                .padding(vertical = 10.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Checkbox(
                                checked = on,
                                onCheckedChange = null,
                                modifier = Modifier.padding(end = 10.dp),
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
                        haptics.tap()
                        scope.launch {
                            // A captured BigPicture image rides the OCR capture
                            // path; text-only notifications send as text. The
                            // bytes live in the in-memory cache, so a record that
                            // outlived them (process restart) falls back to text.
                            val image = if (rec.hasImage) store.getImage(rec.id) else null
                            val context = "📲 ${rec.appLabel} 알림 — ${rec.title}\n${rec.text}".trim()
                            val dispatched = if (image != null) {
                                // Forward the picture AND the notification's text
                                // context (sender/title/body) so the OCR turn sees both.
                                client.captureImage(image, "image/jpeg", caption = context)
                            } else {
                                client.ask(context, emptyList(), null)
                                true
                            }
                            // Only mark "보냄" when it actually dispatched (image path
                            // returns false when the gateway token is unset).
                            if (dispatched) sentId = rec.id
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
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()
    var docs by remember { mutableStateOf<List<TopicDocFile>?>(null) }
    var loadFailed by remember { mutableStateOf(false) }
    suspend fun load() {
        loadFailed = false
        docs = null
        val fetched = client.fetchTopicDocs()
        if (fetched == null) loadFailed = true else docs = fetched
    }
    LaunchedEffect(Unit) { load() }
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
            TextButton(onClick = { haptics.tap(); onOpenTopicDoc("") }) { Text("+ 새 문서") }
        }
        TopicDocsList(docs, loadFailed, onRetry = { scope.launch { load() } }, onOpenTopicDoc = onOpenTopicDoc)
    }
}

@Composable
private fun TopicDocsList(
    list: List<TopicDocFile>?,
    loadFailed: Boolean,
    onRetry: () -> Unit,
    onOpenTopicDoc: (String) -> Unit,
) {
    val haptics = rememberHaptics()
    when {
        loadFailed -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            DenebError("토픽 문서를 불러오지 못했습니다.", onRetry = onRetry)
        }
        list == null -> DenebLoading()
        list.isEmpty() -> EmptyTab("토픽 문서가 없습니다.")
        else -> LazyColumn(Modifier.fillMaxSize()) {
            items(list, key = { it.name }) { doc ->
                Row(
                    modifier = Modifier.animateItem().fillMaxWidth().clickable { haptics.tap(); onOpenTopicDoc(doc.name) }.padding(horizontal = 16.dp, vertical = 14.dp),
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

/** The settings-hub tabs, in display order. The screen renders [entries] as the
 *  pill row and switches content by enum, so a reorder/rename happens in one place. */
private enum class ConfigTab(val label: String) {
    GATEWAY("게이트웨이"),
    MODEL("모델"),
    CRON("크론"),
    TOPIC_DOCS("토픽문서"),
    NOTIFICATIONS("알림"),
}

/** Model-assignment roles. [wire] is the gateway's role key (sent on the RPC and
 *  used to look up the current model); [label] is the Korean segmented-button text. */
private enum class ModelRole(val wire: String, val label: String) {
    MAIN("main", "메인"),
    LIGHTWEIGHT("lightweight", "경량"),
    FALLBACK("fallback", "폴백"),
}

private const val KEY_URL = "deneb.gatewayUrl"
private const val KEY_TOKEN = "deneb.clientToken"
