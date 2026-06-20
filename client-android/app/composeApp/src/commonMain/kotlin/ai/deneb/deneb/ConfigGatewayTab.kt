package ai.deneb.deneb

import ai.deneb.Platform
import ai.deneb.contacts.ContactsReader
import ai.deneb.currentPlatform
import ai.deneb.data.AppSettings
import ai.deneb.tools.ContactsPermissionController
import ai.deneb.tools.LocationPermissionController
import ai.deneb.ui.DenebType
import ai.deneb.ui.settings.SettingsCard
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import org.koin.compose.koinInject

/**
 * Settings hub "게이트웨이" tab: connection (URL + client token), live gateway
 * status, and address-book sync. Version/OTA and fleet are their own settings
 * sections now ([VersionTab], DenebFleetScreen). Hosted by [DenebConfigScreen].
 */
@Composable
internal fun GatewayTab(
    appSettings: AppSettings,
    onBack: () -> Unit,
    denebClient: DenebGatewayClient?,
) {
    var url by remember { mutableStateOf(appSettings.settings.getString(KEY_URL, "")) }
    var token by remember { mutableStateOf(appSettings.settings.getString(KEY_TOKEN, "")) }
    val scope = rememberCoroutineScope()
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
                    val newUrl = url.trim()
                    val newToken = token.trim()
                    // The transcript/mail caches and the live client state are keyed
                    // globally (no account scope), so a gateway/account switch must drop
                    // both — otherwise the prior account's private chat and mail surface
                    // under the new credentials (cached copy on cold start, in-memory
                    // StateFlows until each screen refreshes).
                    val credsChanged = newUrl != appSettings.settings.getString(KEY_URL, "") ||
                        newToken != appSettings.settings.getString(KEY_TOKEN, "")
                    val client = denebClient
                    if (credsChanged && client != null) {
                        // Atomic in the client (bump epoch, purge caches, persist creds,
                        // reset all account state) so there is no window where the fence
                        // is inconsistent with the stored credentials.
                        client.onCredentialsChanged(newUrl, newToken)
                    } else {
                        // No live client to fence (or no change): persist directly, and
                        // still purge caches first so a crash can't pair new creds with
                        // the old account's caches.
                        if (credsChanged) appSettings.clearCachedContent()
                        appSettings.settings.putString(KEY_URL, newUrl)
                        appSettings.settings.putString(KEY_TOKEN, newToken)
                    }
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
        val contactsPermission = koinInject<ContactsPermissionController>()
        val contactsReader = koinInject<ContactsReader>()
        // Hidden on builds that don't declare READ_CONTACTS (everything but the
        // Android foss flavor): isSupported() probes the merged manifest.
        if (contactsReader.isSupported()) {
            ContactsSyncCard(denebClient, contactsPermission, contactsReader)
        }
        // Location sensing is Android-only (the FINE/COARSE permission is declared in the
        // foss flavor; readCurrentLocation gates on the grant at runtime).
        if (currentPlatform is Platform.Mobile.Android) {
            LocationSensingCard(koinInject<LocationPermissionController>())
        }
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
                "게이트웨이 v${status.version.ifBlank { "확인 불가" }} · 네이티브 API ${status.nativeApiVersion}",
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

// Location-sensing opt-in (Android foss). Requesting the FINE permission activates the
// on-demand location push (DenebGatewayClient.maybeForwardLocation, on each sync), which
// the gateway caches so phone_read("location") answers without an SSH round-trip. No
// background tracking — the read happens only while the app is active.
@Composable
private fun LocationSensingCard(permission: LocationPermissionController) {
    val scope = rememberCoroutineScope()
    var working by remember { mutableStateOf(false) }
    var msg by remember { mutableStateOf<String?>(null) }
    val granted = permission.hasPermission()
    SettingsCard {
        Text(
            "위치 센싱",
            style = DenebType.cardTitle,
            color = MaterialTheme.colorScheme.onBackground,
        )
        Spacer(Modifier.height(8.dp))
        Text(
            "위치 권한을 허용하면 앱이 동기화할 때 현재 위치를 게이트웨이로 보고합니다. " +
                "비서가 \"지금 어디?\"에 SSH 없이 답할 수 있습니다. 백그라운드 추적은 하지 않습니다.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(16.dp))
        Button(
            onClick = {
                scope.launch {
                    working = true
                    msg = null
                    val ok = permission.requestPermission()
                    msg = if (ok) "위치 권한이 허용되었습니다. 동기화 시 위치를 보고합니다." else "위치 권한이 거부되었습니다."
                    working = false
                }
            },
            enabled = !working && !granted,
            modifier = Modifier.fillMaxWidth(),
        ) {
            Text(
                if (granted) {
                    "위치 센싱 켜짐"
                } else if (working) {
                    "요청 중…"
                } else {
                    "위치 센싱 켜기"
                },
            )
        }
        val m = msg
        if (m != null) {
            Spacer(Modifier.height(8.dp))
            Text(
                m,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

// Settings keys for the gateway connection. Must stay in lockstep with
// DenebGatewayClient's companion copies and DurableMirrorSettings.GATEWAY_KEYS,
// which pin the same strings.
private const val KEY_URL = "deneb.gatewayUrl"
private const val KEY_TOKEN = "deneb.clientToken"
