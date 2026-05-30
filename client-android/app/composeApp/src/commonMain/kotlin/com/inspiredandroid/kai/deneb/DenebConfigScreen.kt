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
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.data.AppSettings

/**
 * Minimal Deneb gateway configuration: the gateway URL and the standalone client
 * token that [DenebGatewayClient] reads. Self-contained (talks to AppSettings
 * directly) so it survives any future gut of Kai's own settings screen.
 */
@Composable
fun DenebConfigScreen(
    appSettings: AppSettings,
    onBack: () -> Unit,
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

        Spacer(Modifier.height(24.dp))
        TextButton(onClick = onOpenKaiSettings) { Text("Kai 고급 설정") }
    }
}

private const val KEY_URL = "deneb.gatewayUrl"
private const val KEY_TOKEN = "deneb.clientToken"
