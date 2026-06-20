package ai.deneb.deneb

import ai.deneb.openUrl
import ai.deneb.ui.DenebType
import ai.deneb.ui.handCursor
import ai.deneb.ui.settings.SettingsCard
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
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/**
 * Settings hub "버전" tab: current build, patch notes, and the OTA update
 * check / install. Split out of [GatewayTab] into its own section so version
 * and update live on a dedicated page (hosted by [DenebConfigScreen]).
 */
@Composable
internal fun VersionTab(denebClient: DenebGatewayClient?) {
    val scope = rememberCoroutineScope()
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
                    onClick = { installAppUpdate(info.apkUrl) { openUrl(info.apkUrl) } },
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
    }
    if (showPatchNotes) {
        PatchNotesSheet(onDismiss = { showPatchNotes = false })
    }
}

/**
 * Bottom sheet listing the compiled-in [DENEB_PATCH_NOTES], newest first.
 * Opened from the "버전" card — no auto-popup on update.
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
