package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.clickable
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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.data.AppSettings
import kotlinx.coroutines.launch

/**
 * Minimal Deneb gateway configuration: the gateway URL and the standalone client
 * token that [DenebGatewayClient] reads, plus a model switcher backed by the
 * gateway's model registry. Self-contained (talks to AppSettings directly) so it
 * survives any future gut of Kai's own settings screen.
 */
@Composable
fun DenebConfigScreen(
    appSettings: AppSettings,
    onBack: () -> Unit,
    denebClient: DenebGatewayClient? = null,
    onOpenMail: () -> Unit = {},
    onOpenKaiSettings: () -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var url by remember { mutableStateOf(appSettings.settings.getString(KEY_URL, "")) }
    var token by remember { mutableStateOf(appSettings.settings.getString(KEY_TOKEN, "")) }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .statusBarsPadding()
            .padding(16.dp)
            .verticalScroll(rememberScrollState()),
    ) {
        if (navigationTabBar != null) {
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) {
                navigationTabBar()
            }
            Spacer(Modifier.height(16.dp))
        }
        Text("Deneb 게이트웨이", style = MaterialTheme.typography.headlineSmall)
        Spacer(Modifier.height(16.dp))

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
        Spacer(Modifier.height(8.dp))
        OutlinedButton(onClick = onBack, modifier = Modifier.fillMaxWidth()) { Text("취소") }

        if (denebClient != null) {
            Spacer(Modifier.height(28.dp))
            Text("Deneb", style = MaterialTheme.typography.titleMedium)
            Spacer(Modifier.height(8.dp))
            Button(onClick = onOpenMail, modifier = Modifier.fillMaxWidth()) { Text("📧  받은 메일") }
            ModelSection(denebClient)
        }

        Spacer(Modifier.height(24.dp))
        TextButton(onClick = onOpenKaiSettings) { Text("고급 설정") }
    }
}

/**
 * Deneb model switcher. Lists the gateway's models and switches the default chat
 * model (`models.set role=main`) — which changes chat across every Deneb surface.
 */
@Composable
private fun ModelSection(client: DenebGatewayClient) {
    val models by client.denebModels.collectAsState()
    val scope = rememberCoroutineScope()
    LaunchedEffect(Unit) { client.refreshModels() }

    if (models.isEmpty()) return

    Spacer(Modifier.height(28.dp))
    Text("모델", style = MaterialTheme.typography.titleMedium)
    Spacer(Modifier.height(2.dp))
    Text(
        "Deneb의 기본 채팅 모델 — 모든 채널(텔레그램·미니앱·이 앱)에 공통 적용됩니다.",
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    Spacer(Modifier.height(8.dp))
    models.forEach { model ->
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clickable(enabled = !model.current) {
                    scope.launch { client.setMainModel(model.id) }
                }
                .padding(vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                if (model.current) "● " else "○ ",
                color = if (model.current) {
                    MaterialTheme.colorScheme.primary
                } else {
                    MaterialTheme.colorScheme.onSurfaceVariant
                },
            )
            Column(Modifier.weight(1f)) {
                Text(model.display, style = MaterialTheme.typography.bodyLarge)
                Text(
                    model.id + if (model.health.equals("offline", ignoreCase = true)) "  ·  오프라인" else "",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

private const val KEY_URL = "deneb.gatewayUrl"
private const val KEY_TOKEN = "deneb.clientToken"
