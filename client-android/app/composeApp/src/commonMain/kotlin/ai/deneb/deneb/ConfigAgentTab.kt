package ai.deneb.deneb

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import ai.deneb.data.AppSettings
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.handCursor
import ai.deneb.ui.settings.SettingsCard

/**
 * Settings hub "에이전트" tab: the master toggles the agent's own system prompt and
 * tooling already reference as "Settings → Agent" (ChatSystemPromptBuilder,
 * CommonTools, TaskScheduler), but which had no UI. Memory gates fact/preference
 * learning; scheduling gates cron + scheduled tasks. Both gate client-side tool
 * registration, so a change takes effect from the next conversation turn (not a
 * gateway prompt, so unrelated to the prompt-cache doctrine). Hosted by
 * [DenebConfigScreen]'s pager.
 */
@Composable
internal fun AgentTab(appSettings: AppSettings) {
    var memoryEnabled by remember { mutableStateOf(appSettings.isMemoryEnabled()) }
    var schedulingEnabled by remember { mutableStateOf(appSettings.isSchedulingEnabled()) }
    val haptics = rememberHaptics()
    Column(
        modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        SettingsCard {
            SettingToggleRow(
                title = "기억",
                subtitle = "대화에서 사실·선호를 학습해 다음 대화에 활용합니다. 끄면 새로 기억하지 않습니다.",
                checked = memoryEnabled,
                onCheckedChange = {
                    haptics.tap()
                    memoryEnabled = it
                    appSettings.setMemoryEnabled(it)
                },
            )
        }
        SettingsCard {
            SettingToggleRow(
                title = "자동 스케줄링",
                subtitle = "예약 작업과 크론을 실행합니다. 끄면 모든 예약 작업이 멈춥니다.",
                checked = schedulingEnabled,
                onCheckedChange = {
                    haptics.tap()
                    schedulingEnabled = it
                    appSettings.setSchedulingEnabled(it)
                },
            )
        }
        Text(
            "변경은 다음 대화 turn부터 도구 구성에 반영됩니다.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(horizontal = 4.dp),
        )
    }
}

/**
 * A title/subtitle settings row with a trailing Material [Switch]. The whole row is
 * [toggleable] (Role.Switch) for accessibility, so the Switch itself takes
 * onCheckedChange = null to avoid double-handling the tap.
 */
@Composable
private fun SettingToggleRow(
    title: String,
    subtitle: String,
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .toggleable(value = checked, role = Role.Switch, onValueChange = onCheckedChange)
            .handCursor(),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(
                title,
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
                color = MaterialTheme.colorScheme.onBackground,
            )
            Spacer(Modifier.height(4.dp))
            Text(
                subtitle,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Spacer(Modifier.width(12.dp))
        Switch(checked = checked, onCheckedChange = null)
    }
}
