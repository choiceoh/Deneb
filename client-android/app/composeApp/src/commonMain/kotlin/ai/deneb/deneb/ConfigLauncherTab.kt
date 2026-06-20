package ai.deneb.deneb

import ai.deneb.LauncherMode
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebListRow
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Home
import androidx.compose.material.icons.outlined.OpenInNew
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

/**
 * 런처 settings: turn home-launcher mode on/off and jump to the system home-app
 * chooser. The toggle drives the HOME activity-alias directly ([LauncherMode]); the
 * alias's enabled state is the source of truth, so the switch always reflects reality
 * even across restarts. Only reachable when [LauncherMode.supported] (foss Android) —
 * DenebConfigScreen hides the section otherwise.
 */
@Composable
internal fun LauncherTab(launcherMode: LauncherMode) {
    var enabled by remember { mutableStateOf(launcherMode.isEnabled()) }
    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(top = 4.dp, bottom = 24.dp),
    ) {
        DenebGroup(label = "홈 런처") {
            DenebListRow(
                title = "런처 모드",
                onClick = {
                    val next = !enabled
                    launcherMode.setEnabled(next)
                    enabled = next
                },
                icon = Icons.Outlined.Home,
                subtitle = "홈 버튼으로 Deneb를 띄웁니다",
                selected = enabled,
                chevron = false,
                divider = enabled,
                trailing = {
                    Switch(
                        checked = enabled,
                        onCheckedChange = {
                            launcherMode.setEnabled(it)
                            enabled = it
                        },
                    )
                },
            )
            if (enabled) {
                DenebListRow(
                    title = "기본 홈 앱으로 설정",
                    onClick = { launcherMode.openHomeAppSettings() },
                    icon = Icons.Outlined.OpenInNew,
                    subtitle = "시스템 설정에서 Deneb를 홈 앱으로 선택",
                    divider = false,
                )
            }
        }
        Spacer(Modifier.height(12.dp))
        Text(
            text = "켜면 Deneb가 홈 앱 후보가 됩니다. 시스템 설정에서 기본 홈 앱으로 Deneb를 선택하세요. " +
                "끄면 후보에서 빠져 원래 런처로 돌아갑니다.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(horizontal = 20.dp),
        )
    }
}
