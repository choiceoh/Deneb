package ai.deneb.deneb

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.deneb.data.AppSettings
import ai.deneb.contacts.ContactsReader
import ai.deneb.tools.ContactsPermissionController
import ai.deneb.ui.DenebType
import ai.deneb.ui.handCursor
import ai.deneb.ui.settings.SettingsCard
import kotlinx.coroutines.launch
import org.koin.compose.koinInject

/**
 * Settings hub "게이트웨이" tab: connection (URL + client token), live gateway
 * status, version / OTA update, patch notes, and address-book sync. Hosted by
 * [DenebConfigScreen]'s pager.
 */
@Composable
internal fun GatewayTab(
    appSettings: AppSettings,
    onBack: () -> Unit,
    denebClient: DenebGatewayClient?,
    onOpenFleet: () -> Unit = {},
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
                style = DenebType.cardTitle,
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
        // Fleet management lives on its own full screen (DenebFleetScreen) — the
        // hub stays configuration-only. This entry is the mobile path to it; the
        // desktop sidebar has its own "fleet" row.
        SettingsCard {
            Text(
                "플릿",
                style = DenebType.cardTitle,
                color = MaterialTheme.colorScheme.onBackground,
            )
            Spacer(Modifier.height(8.dp))
            Text(
                "GPU 노드 상태, 모델(레시피) 기동/중지, 작업 로그를 관리합니다.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(12.dp))
            OutlinedButton(onClick = onOpenFleet, modifier = Modifier.fillMaxWidth()) {
                Text("플릿 관리 열기")
            }
        }
        SettingsCard {
            Text(
                "버전",
                style = DenebType.cardTitle,
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
            style = DenebType.cardTitle,
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
            style = DenebType.cardTitle,
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
                    style = DenebType.subject,
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

// Settings keys for the gateway connection. Must stay in lockstep with
// DenebGatewayClient's companion copies and DurableMirrorSettings.GATEWAY_KEYS,
// which pin the same strings.
private const val KEY_URL = "deneb.gatewayUrl"
private const val KEY_TOKEN = "deneb.clientToken"
