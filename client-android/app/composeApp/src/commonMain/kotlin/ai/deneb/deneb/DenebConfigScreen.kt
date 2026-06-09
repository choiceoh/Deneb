package ai.deneb.deneb

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
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
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
import androidx.compose.material3.RichTooltip
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TooltipBox
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.material3.rememberTooltipState
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
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntRect
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.PopupPositionProvider
import ai.deneb.data.AppSettings
import ai.deneb.deneb.generated.SkillRow
import ai.deneb.contacts.ContactsReader
import ai.deneb.tools.ContactsPermissionController
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.handCursor
import ai.deneb.ui.settings.SettingsCard
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
    val pagerState = rememberPagerState(pageCount = { ConfigTab.entries.size })
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()

    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        // imePadding shrinks the column (and its weighted pager) above the soft
        // keyboard, so a focused settings field low in a tab stays visible instead of
        // hiding behind it (edge-to-edge: the app owns the IME inset).
        Column(Modifier.fillMaxSize().statusBarsPadding().imePadding()) {
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
            // Pill-style tabs (no underline) — mirrors the upstream "고급 설정" tab selector:
            // each tab is a rounded Surface, the selected one gets a soft primary tint.
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .horizontalScroll(rememberScrollState())
                    .padding(horizontal = 12.dp, vertical = 4.dp),
                horizontalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                ConfigTab.entries.forEachIndexed { idx, entry ->
                    val isSelected = pagerState.currentPage == idx
                    Surface(
                        modifier = Modifier
                            .handCursor()
                            .clip(RoundedCornerShape(50))
                            .selectable(
                                selected = isSelected,
                                role = Role.Tab,
                                onClick = { haptics.tap(); scope.launch { pagerState.animateScrollToPage(idx) } },
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
            // Swipe left/right to move between tabs; the pager claims horizontal
            // drags while each tab's own column keeps its vertical scroll. Tapping a
            // pill animates here too, so the bar and pages stay in lockstep.
            HorizontalPager(
                state = pagerState,
                modifier = Modifier.weight(1f).fillMaxWidth(),
            ) { page ->
                when (ConfigTab.entries[page]) {
                    ConfigTab.GATEWAY -> GatewayTab(appSettings, onBack, denebClient)
                    ConfigTab.MODEL -> denebClient?.let { ModelTab(it) }
                    ConfigTab.SKILLS -> denebClient?.let { SkillsTab(it) }
                    ConfigTab.CRON -> denebClient?.let { CronTab(it, onOpenCron) }
                    ConfigTab.TOPIC_DOCS -> denebClient?.let { TopicDocsTab(it, onOpenTopicDoc) }
                    ConfigTab.OBSERVE -> denebClient?.let { ObserveTab(it) }
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
    var tokenVisible by remember { mutableStateOf(false) }
    val gatewayStatus = if (denebClient != null) denebClient.clientStatus.collectAsState().value else null
    LaunchedEffect(denebClient) {
        denebClient?.refreshClientStatus()
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
                // Tokens are secrets — mask by default so the value isn't exposed
                // over the shoulder; a 보기/숨기기 toggle reveals it for pasting.
                visualTransformation = if (tokenVisible) VisualTransformation.None else PasswordVisualTransformation(),
                trailingIcon = {
                    TextButton(onClick = { tokenVisible = !tokenVisible }) {
                        Text(
                            if (tokenVisible) "숨기기" else "보기",
                            style = MaterialTheme.typography.labelMedium,
                        )
                    }
                },
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
            checking = statusChecking,
            enabled = denebClient != null,
            onRefresh = {
                denebClient?.let { c ->
                    scope.launch {
                        statusChecking = true
                        try {
                            c.refreshClientStatus()
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
                "현재 빌드 $DENEB_VERSION_CODE",
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
                    "새 빌드 ${info.buildLabel} 사용 가능",
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
                Button(
                    // OTA: download the APK in-app and launch the installer. Falls back to
                    // opening the URL in a browser if install can't proceed (no permission,
                    // non-Android platform).
                    onClick = { installAppUpdate(info.apkUrl) { uriHandler.openUri(info.apkUrl) } },
                    modifier = Modifier.fillMaxWidth(),
                ) {
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
                Spacer(Modifier.height(4.dp))
                Text(
                    "현재 빌드 $DENEB_VERSION_CODE",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            // versionName was removed — the sheet is a flat reverse-chronological
            // changelog. Each entry is one build's highlights, separated by spacing
            // (no version label, no "현재 버전" badge).
            items(DENEB_PATCH_NOTES) { note ->
                Column(
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                    modifier = Modifier.padding(bottom = 8.dp),
                ) {
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
        // Header + a "?" affordance: tapping the circled question mark opens a
        // rich tooltip explaining what each of the five roles does. Descriptions
        // live on ModelRole.desc so the segmented buttons and the tooltip stay in
        // sync from one source.
        val roleTooltip = rememberTooltipState(isPersistent = true)
        // Material3's default rich-tooltip position provider computes a negative x
        // when a wide tooltip is anchored near the left edge: as an off-screen
        // fallback it centers the tooltip on the tiny "?" anchor, so the tooltip
        // clips off the left of narrow phone screens. Use a provider that
        // left-aligns to the anchor and clamps fully into the window — the same
        // approach the chat ServiceSelector's AnchorAbovePositionProvider takes.
        val tooltipSpacingPx = with(LocalDensity.current) { 4.dp.roundToPx() }
        val clampedTooltipPosition = remember(tooltipSpacingPx) {
            ClampedTooltipPositionProvider(tooltipSpacingPx)
        }
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Text(
                "역할별 모델",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            TooltipBox(
                positionProvider = clampedTooltipPosition,
                tooltip = {
                    RichTooltip(title = { Text("모델 역할") }) {
                        Text(ModelRole.entries.joinToString("\n") { "${it.label} — ${it.desc}" })
                    }
                },
                state = roleTooltip,
            ) {
                Box(
                    modifier = Modifier
                        .size(18.dp)
                        .border(1.dp, MaterialTheme.colorScheme.outline, CircleShape)
                        .clickable { scope.launch { roleTooltip.show() } }
                        .handCursor(),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        "?",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
        // Legend for the per-model response-status dot.
        Row(
            horizontalArrangement = Arrangement.spacedBy(14.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            HealthLegendItem(ModelHealth.ONLINE, "응답 가능")
            HealthLegendItem(ModelHealth.OFFLINE, "응답 없음")
            HealthLegendItem(ModelHealth.UNKNOWN, "미확인")
        }
        // Role summary: every role and its currently-assigned model at a glance,
        // so you don't have to click through each segment to see what's wired.
        // Tapping a row selects that role for the model list below.
        SettingsCard(innerPadding = false) {
            ModelRole.entries.forEachIndexed { i, r ->
                val assignedId = roleModels[r.wire]
                val assignedName = models.firstOrNull { it.id == assignedId }?.display
                    ?: assignedId ?: "미설정"
                val isSel = role == r
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable { haptics.tap(); role = r }
                        .handCursor()
                        .padding(horizontal = 16.dp, vertical = 10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        r.label,
                        style = MaterialTheme.typography.labelLarge,
                        fontWeight = if (isSel) FontWeight.SemiBold else FontWeight.Normal,
                        color = if (isSel) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.width(56.dp),
                    )
                    Spacer(Modifier.width(8.dp))
                    Text(
                        assignedName,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                }
                if (i < ModelRole.entries.lastIndex) {
                    HorizontalDivider(
                        Modifier.padding(start = 16.dp),
                        color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f),
                    )
                }
            }
        }
        if (switchFailed) {
            Text(
                "모델 전환에 실패했어요. 다시 시도해 주세요.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error,
            )
        }
        Text(
            "'${role.label}' 역할에 사용할 모델",
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        // Model list grouped by provider (the id prefix before "/"), so local
        // vLLM, custom endpoints, and cloud providers don't blur into one flat
        // list. Tapping a row assigns that model to the role selected above.
        SettingsCard(innerPadding = false) {
            val grouped = remember(models) { models.groupBy { modelProviderLabel(it.id) } }
            grouped.entries.forEachIndexed { gi, (provider, groupModels) ->
                Text(
                    provider,
                    style = MaterialTheme.typography.labelMedium,
                    fontWeight = FontWeight.SemiBold,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(
                        start = 16.dp,
                        top = if (gi == 0) 12.dp else 18.dp,
                        bottom = 2.dp,
                    ),
                )
                groupModels.forEachIndexed { mi, model ->
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
                                    haptics.reject()
                                    pendingDelete = model
                                },
                                enabled = !switching,
                            ) {
                                Text("삭제", color = MaterialTheme.colorScheme.error)
                            }
                        }
                    }
                    if (mi < groupModels.lastIndex) {
                        HorizontalDivider(
                            Modifier.padding(start = 16.dp),
                            color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f),
                        )
                    }
                }
            }
        }

        // Add an OpenAI-compatible endpoint (vLLM / LM Studio / etc.) by base URL
        // + model name in its own card, matching the gateway-connection card so
        // the form doesn't float on the bare background below the model list.
        SettingsCard {
            Text(
                "OpenAI 호환 모델 직접 추가",
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
                color = MaterialTheme.colorScheme.onBackground,
            )
            Spacer(Modifier.height(4.dp))
            Text(
                "Base URL과 모델 이름으로 vLLM·LM Studio 같은 OpenAI 호환 엔드포인트를 추가합니다. 인증 키가 필요 없는 엔드포인트용입니다.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(12.dp))
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
            Spacer(Modifier.height(12.dp))
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
                Spacer(Modifier.height(8.dp))
                Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
            }
            Spacer(Modifier.height(12.dp))
            Button(
                onClick = {
                    haptics.confirm()
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
                    haptics.reject()
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

// Skills the agent can use (read-only). Mirrors the system-prompt skill list via
// miniapp.skills.list — name, description, category, source, and whether the skill
// is user-invocable (rendered with a leading slash). No toggles: discovery is
// filesystem-driven, so the list reflects what's installed on the gateway host.
@Composable
private fun SkillsTab(client: DenebGatewayClient) {
    val skills by client.denebSkills.collectAsState()
    val scope = rememberCoroutineScope()
    var loadFailed by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { loadFailed = !client.refreshSkills() }
    when {
        skills.isEmpty() && loadFailed -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            DenebError(
                "스킬을 불러오지 못했습니다.",
                onRetry = { scope.launch { loadFailed = !client.refreshSkills() } },
            )
        }
        skills.isEmpty() -> EmptyTab("사용할 수 있는 스킬이 없습니다.")
        else -> LazyColumn(Modifier.fillMaxSize()) {
            items(skills, key = { it.name }) { skill ->
                Column(
                    Modifier.animateItem().fillMaxWidth().padding(horizontal = 16.dp, vertical = 14.dp),
                ) {
                    // Skill name only — no runnable slash command. The live slash
                    // dispatcher matches a lowercased raw name (not a sanitized
                    // command) and only for local/system skills, so showing a
                    // command here would risk advertising one that doesn't route.
                    Text(
                        skill.name,
                        style = MaterialTheme.typography.bodyLarge,
                        fontWeight = FontWeight.Medium,
                        color = MaterialTheme.colorScheme.onSurface,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    if (skill.description.isNotBlank()) {
                        Text(
                            skill.description,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 2,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                    val meta = skillMetaLine(skill)
                    if (meta.isNotBlank()) {
                        Spacer(Modifier.height(2.dp))
                        Text(
                            meta,
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
                HorizontalDivider(Modifier.padding(start = 16.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
            }
        }
    }
}

// skillSourceLabel maps the gateway's discovery-origin string to a Korean label,
// matching DenebConfigScreen's literal-string convention (this screen doesn't use
// stringResource). Falls back to the raw value for origins we don't surface yet.
private fun skillSourceLabel(source: String): String = when (source) {
    "managed" -> "관리형"
    "workspace" -> "워크스페이스"
    "agents-skills-personal" -> "개인"
    "agents-skills-project" -> "프로젝트"
    "bundled" -> "기본 제공"
    "plugin" -> "플러그인"
    "extra" -> "추가"
    else -> source
}

// skillMetaLine renders "category · source", omitting whichever is blank.
private fun skillMetaLine(skill: SkillRow): String = listOfNotNull(
    skill.category.takeIf { it.isNotBlank() },
    skillSourceLabel(skill.source).takeIf { it.isNotBlank() },
).joinToString(" · ")

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

// 관찰(Observe) tab: the gateway's own behavior + recent warn/error logs via
// miniapp.observe.*. The native adapter over the observe plane (CLI and chat
// tool are the other two). Read-only — an operator dashboard, not controls.
@Composable
private fun ObserveTab(client: DenebGatewayClient) {
    var behavior by remember { mutableStateOf<ObserveBehavior?>(null) }
    var logs by remember { mutableStateOf<List<ObserveLogLine>>(emptyList()) }
    var loading by remember { mutableStateOf(true) }
    var failed by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    suspend fun load() {
        loading = true
        failed = false
        val b = client.observeBehavior(7)
        val l = client.observeLogs("warn", 40)
        behavior = b
        logs = l?.lines ?: emptyList()
        failed = b == null && l == null
        loading = false
    }
    LaunchedEffect(Unit) { load() }
    when {
        loading -> DenebLoading()
        failed -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            DenebError("관찰 데이터를 불러오지 못했습니다.", onRetry = { scope.launch { load() } })
        }
        else -> LazyColumn(Modifier.fillMaxSize()) {
            behavior?.let { b ->
                item {
                    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 14.dp)) {
                        Text("최근 7일 동작", style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold, color = MaterialTheme.colorScheme.onSurface)
                        Text(
                            "실행 ${b.runs}회 · 능동 ${b.proactiveRuns} · 압축 ${b.compactedRuns}",
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    HorizontalDivider(Modifier.padding(start = 16.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
                }
                if (b.tools.isNotEmpty()) {
                    item { ObserveSectionHeader("도구 사용") }
                    items(b.tools, key = { it.name }) { t ->
                        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp)) {
                            Text(t.name, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
                            Text(
                                if (t.errors > 0) "${t.calls}회 · ${t.errors} 오류 · 평균 ${t.avgMs}ms" else "${t.calls}회 · 평균 ${t.avgMs}ms",
                                style = MaterialTheme.typography.bodySmall,
                                color = if (t.errors > 0) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        HorizontalDivider(Modifier.padding(start = 16.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
                    }
                }
            }
            if (logs.isNotEmpty()) {
                item { ObserveSectionHeader("최근 경고 / 오류") }
                items(logs.size) { i ->
                    val l = logs[i]
                    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp)) {
                        Text(
                            l.level,
                            style = MaterialTheme.typography.labelSmall,
                            fontWeight = FontWeight.SemiBold,
                            color = if (l.level == "ERROR") MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.tertiary,
                        )
                        Text(l.msg, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurface, maxLines = 3, overflow = TextOverflow.Ellipsis)
                    }
                    HorizontalDivider(Modifier.padding(start = 16.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
                }
            }
            if ((behavior?.runs ?: 0) == 0 && logs.isEmpty()) {
                item {
                    Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) {
                        Text("아직 관찰된 동작이 없습니다.", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        }
    }
}

@Composable
private fun ObserveSectionHeader(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.labelMedium,
        fontWeight = FontWeight.SemiBold,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 18.dp, bottom = 6.dp),
    )
}

/** The settings-hub tabs, in display order. The screen renders [entries] as the
 *  pill row and switches content by enum, so a reorder/rename happens in one place. */
private enum class ConfigTab(val label: String) {
    GATEWAY("게이트웨이"),
    MODEL("모델"),
    SKILLS("스킬"),
    CRON("크론"),
    TOPIC_DOCS("토픽문서"),
    OBSERVE("관찰"),
}

/** Model-assignment roles. [wire] is the gateway's role key (sent on the RPC and
 *  used to look up the current model); [label] is the Korean segmented-button text;
 *  [desc] is the one-line role explanation shown in the "?" tooltip. Descriptions
 *  mirror the gateway's modelrole registry (main / tiny / lightweight / analysis /
 *  fallback). */
private enum class ModelRole(val wire: String, val label: String, val desc: String) {
    MAIN("main", "메인", "채팅·분석·도구 호출 등 주 대화를 담당하는 기본 모델"),
    TINY("tiny", "초경량", "간단한 분류·추출 같은 가벼운 작업"),
    LIGHTWEIGHT("lightweight", "경량", "메일 요약 등 분량이 정해진 요약 작업"),
    ANALYSIS("analysis", "분석", "추론이 필요한 고품질 작업"),
    FALLBACK("fallback", "폴백", "메인 모델이 실패했을 때 대신 쓰는 모델"),
}

/**
 * Positions a rich tooltip above its anchor (falling back to below when there is
 * no room above), left-aligned to the anchor but clamped fully inside the window
 * so a wide tooltip never spills off a screen edge.
 *
 * Material3's default rich-tooltip position provider returns a negative x for a
 * wide tooltip anchored near the left edge (its off-screen fallback centers the
 * tooltip on the small anchor), which clipped the model-role "?" tooltip off the
 * left of narrow phone screens. Mirrors [ServiceSelector]'s clamping approach.
 * Marked internal so the clamping can be unit-tested without a live window.
 */
internal class ClampedTooltipPositionProvider(
    private val verticalSpacing: Int,
) : PopupPositionProvider {
    override fun calculatePosition(
        anchorBounds: IntRect,
        windowSize: IntSize,
        layoutDirection: LayoutDirection,
        popupContentSize: IntSize,
    ): IntOffset {
        val maxX = (windowSize.width - popupContentSize.width).coerceAtLeast(0)
        val x = anchorBounds.left.coerceIn(0, maxX)
        val above = anchorBounds.top - popupContentSize.height - verticalSpacing
        val y = if (above >= 0) {
            above
        } else {
            val maxY = (windowSize.height - popupContentSize.height).coerceAtLeast(0)
            (anchorBounds.bottom + verticalSpacing).coerceAtMost(maxY)
        }
        return IntOffset(x, y)
    }
}

// modelProviderLabel maps a model id ("vllm/step3p7", "custom/gemma…") to a
// human label for grouping the model list by provider. The prefix before the
// first "/" is the provider key; unknown providers fall back to the raw prefix
// so a newly-added provider still groups sensibly instead of vanishing.
private fun modelProviderLabel(id: String): String {
    val p = id.substringBefore('/', "")
    return when {
        p.isEmpty() -> "기타"
        p == "vllm" -> "로컬 (vLLM)"
        p.startsWith("custom") -> "커스텀"
        p == "google" -> "Google"
        p == "anthropic" -> "Anthropic"
        p == "openai" -> "OpenAI"
        p == "zai" -> "Z.ai"
        else -> p
    }
}

private const val KEY_URL = "deneb.gatewayUrl"
private const val KEY_TOKEN = "deneb.clientToken"
