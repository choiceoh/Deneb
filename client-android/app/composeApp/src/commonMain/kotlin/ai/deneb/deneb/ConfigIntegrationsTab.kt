package ai.deneb.deneb

import ai.deneb.openUrl
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import ai.deneb.ui.denebHint
import ai.deneb.ui.handCursor
import ai.deneb.ui.settings.SettingsCard
import androidx.compose.foundation.border
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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RichTooltip
import androidx.compose.material3.Text
import androidx.compose.material3.TooltipBox
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
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

// Settings hub "연동" tab: external-service account linking. Dropbox is the first
// (and currently only) integration — a PKCE OAuth wizard that replaces the
// host-side deneb-dropbox-auth CLI. Two flows by platform: on Android the
// consent redirects back via a deep link and the code is captured automatically
// ([DropboxAuthBridge]); elsewhere the user pastes the out-of-band code Dropbox
// shows. Once linked, the dropbox chat tool (list/search/download/analyze/backup)
// works. The refresh token lives on the gateway host.
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun IntegrationsTab(client: DenebGatewayClient) {
    var status by remember { mutableStateOf<DropboxStatusOut?>(null) }
    var loading by remember { mutableStateOf(true) }
    var appKey by remember { mutableStateOf("") }
    var code by remember { mutableStateOf("") }
    var authOpened by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    // Android → deep-link URI (Dropbox auto-returns the code); other platforms →
    // null (user pastes the out-of-band code).
    val redirectUri = remember { dropboxRedirectUri() }
    val auto = redirectUri != null

    suspend fun reload() {
        loading = true
        status = client.dropboxStatus()
        loading = false
    }
    LaunchedEffect(Unit) { reload() }

    suspend fun exchange(authCode: String) {
        busy = true
        error = null
        val ok = client.dropboxComplete(authCode.trim())
        busy = false
        // Re-read the host as the source of truth. An authorization code is
        // one-time: if the deep link delivers it twice, the 1st exchange links
        // the account and the 2nd fails (code already used / verifier cleared).
        // Don't surface that false error when we're actually connected.
        reload()
        if (ok || status?.connected == true) {
            code = ""
            appKey = ""
            authOpened = false
        } else {
            error = "인증 코드 교환에 실패했습니다. 다시 시도해 주세요."
        }
    }

    // Auto-capture: the Android deep-link handler delivers the code here after the
    // user approves in the browser. Consume it, then exchange for a token. Guard
    // against a duplicate delivery of the same one-time code re-running exchange.
    var lastExchanged by remember { mutableStateOf<String?>(null) }
    val pendingCode by DropboxAuthBridge.code.collectAsState()
    LaunchedEffect(pendingCode) {
        val c = pendingCode ?: return@LaunchedEffect
        DropboxAuthBridge.code.value = null
        if (c == lastExchanged) return@LaunchedEffect
        lastExchanged = c
        exchange(c)
    }

    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(vertical = 12.dp),
    ) {
        DenebSectionLabel("Dropbox", Modifier.padding(horizontal = 16.dp))
        SettingsCard(Modifier.padding(horizontal = 16.dp)) {
            val s = status
            when {
                loading -> Text("불러오는 중…", style = DenebType.body, color = denebHint())

                s?.connected == true -> {
                    // What the connection unlocks lives behind a "?" tooltip rather
                    // than a permanent gray caption: once connected it reads as
                    // clutter, so the connected state stays clean and the detail is
                    // one tap away for the curious. (Mirrors ConfigModelTab's "?".)
                    val infoTooltip = rememberTooltipState(isPersistent = true)
                    val tooltipSpacingPx = with(LocalDensity.current) { 4.dp.roundToPx() }
                    val clampedTooltipPosition = remember(tooltipSpacingPx) {
                        ClampedTooltipPositionProvider(tooltipSpacingPx)
                    }
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        Text("● 연결됨", style = DenebType.rowTitleStrong, color = MaterialTheme.colorScheme.primary)
                        TooltipBox(
                            positionProvider = clampedTooltipPosition,
                            tooltip = {
                                RichTooltip(title = { Text("Dropbox 연동") }) {
                                    Text("채팅에서 dropbox 도구(파일 목록·검색·다운로드·분석·백업)를 사용할 수 있습니다.")
                                }
                            },
                            state = infoTooltip,
                        ) {
                            Box(
                                modifier = Modifier
                                    .size(18.dp)
                                    .border(1.dp, MaterialTheme.colorScheme.outline, CircleShape)
                                    .clickable { scope.launch { infoTooltip.show() } }
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
                    Spacer(Modifier.height(12.dp))
                    // Re-link without leaving the screen: flip back to the wizard.
                    // The App key is saved, so begin reuses it.
                    Button(onClick = {
                        authOpened = false
                        code = ""
                        error = null
                        status = s.copy(connected = false)
                    }) { Text("다시 연동") }
                }

                else -> {
                    Text(
                        "Dropbox 계정을 연결합니다. Dropbox App Console에서 만든 앱의 App key가 필요합니다.",
                        style = DenebType.body,
                        color = denebHint(),
                    )
                    Spacer(Modifier.height(12.dp))
                    // Step 1 — App key (skipped once one is saved on the host).
                    if (s?.appConfigured != true) {
                        OutlinedTextField(
                            value = appKey,
                            onValueChange = { appKey = it },
                            label = { Text("Dropbox App key") },
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                        Spacer(Modifier.height(10.dp))
                    }
                    // Step 2 — open the consent page in a browser.
                    Button(
                        enabled = !busy && (s?.appConfigured == true || appKey.isNotBlank()),
                        onClick = {
                            scope.launch {
                                busy = true
                                error = null
                                val url = client.dropboxBegin(appKey.trim(), redirectUri ?: "")
                                busy = false
                                if (url == null) {
                                    error = "연동을 시작하지 못했습니다. App key를 확인하세요."
                                } else {
                                    openUrl(url)
                                    authOpened = true
                                }
                            }
                        },
                    ) { Text(if (authOpened) "승인 페이지 다시 열기" else "승인 페이지 열기") }

                    // Step 3 — finish. Android auto-captures via the deep link;
                    // other platforms paste the out-of-band code.
                    if (authOpened) {
                        Spacer(Modifier.height(14.dp))
                        if (auto) {
                            Text(
                                if (busy) "연결 중…" else "승인을 마치면 앱으로 돌아와 자동으로 연결됩니다.",
                                style = DenebType.body,
                                color = denebHint(),
                            )
                        } else {
                            Text(
                                "승인 후 Dropbox가 보여주는 인증 코드를 붙여넣으세요.",
                                style = DenebType.body,
                                color = denebHint(),
                            )
                            Spacer(Modifier.height(8.dp))
                            OutlinedTextField(
                                value = code,
                                onValueChange = { code = it },
                                label = { Text("인증 코드") },
                                singleLine = true,
                                modifier = Modifier.fillMaxWidth(),
                            )
                            Spacer(Modifier.height(10.dp))
                            Button(
                                enabled = !busy && code.isNotBlank(),
                                onClick = { scope.launch { exchange(code) } },
                            ) { Text("연동 완료") }
                        }
                    }
                }
            }
            error?.let {
                Spacer(Modifier.height(10.dp))
                Text(it, style = DenebType.body, color = MaterialTheme.colorScheme.error)
            }
        }
    }
}
