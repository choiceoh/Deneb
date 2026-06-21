package ai.deneb.deneb

import ai.deneb.data.AppSettings
import ai.deneb.data.ThemeMode
import ai.deneb.defaultUiScale
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.handCursor
import ai.deneb.ui.settings.SettingsCard
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import kotlin.math.roundToInt

/**
 * Settings hub "화면" tab: theme + UI scale. Both are already wired live in App.kt
 * (it observes [AppSettings.themeModeFlow] / [AppSettings.uiScaleFlow]), so changing
 * them here recolors / rescales the whole app immediately — this tab just exposes the
 * controls that were missing. Controls are Material (RadioButton / Slider) per the
 * native design doctrine; grouping uses the local [SettingsCard] idiom. Hosted by
 * [DenebConfigScreen]'s pager.
 */
@Composable
internal fun AppearanceTab(appSettings: AppSettings) {
    val themeMode by appSettings.themeModeFlow.collectAsState()
    val uiScale by appSettings.uiScaleFlow.collectAsState()
    val haptics = rememberHaptics()
    // Slider stays local while dragging so the app density only changes on release
    // (a live density change mid-drag would rescale the slider under the finger).
    // Re-seeded from the flow whenever a committed value lands (incl. the reset).
    var sliderValue by remember(uiScale) { mutableStateOf(uiScale) }
    Column(
        modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        SettingsCard {
            Text(
                "테마",
                style = DenebType.cardTitle,
                color = MaterialTheme.colorScheme.onBackground,
            )
            Spacer(Modifier.height(8.dp))
            Text(
                "앱 전체 배색을 즉시 바꿉니다.",
                style = DenebType.hint,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        SettingsCard(innerPadding = false) {
            val options = listOf(
                Triple(ThemeMode.System, "시스템", "기기 설정을 따릅니다"),
                Triple(ThemeMode.Light, "라이트", "항상 밝게"),
                Triple(ThemeMode.Dark, "다크", "항상 어둡게"),
                Triple(ThemeMode.OledBlack, "OLED 블랙", "완전한 검정 배경 (전력 절약)"),
            )
            options.forEachIndexed { i, (mode, label, desc) ->
                val isSel = themeMode == mode
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .selectable(
                            selected = isSel,
                            role = Role.RadioButton,
                            onClick = {
                                haptics.tap()
                                appSettings.setThemeMode(mode)
                            },
                        )
                        .handCursor()
                        .padding(horizontal = 16.dp, vertical = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(Modifier.weight(1f)) {
                        Text(
                            label,
                            style = DenebType.rowTitle,
                            fontWeight = if (isSel) FontWeight.SemiBold else FontWeight.Normal,
                            color = if (isSel) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface,
                        )
                        Text(
                            desc,
                            style = DenebType.hint,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    // onClick = null: the Row owns the click (Role.RadioButton above),
                    // so the button reflects state without double-handling the tap.
                    RadioButton(selected = isSel, onClick = null)
                }
                if (i < options.lastIndex) {
                    HorizontalDivider(
                        modifier = Modifier.padding(start = 16.dp),
                        color = denebHairline(),
                    )
                }
            }
        }
        SettingsCard {
            Text(
                "화면 배율",
                style = DenebType.cardTitle,
                color = MaterialTheme.colorScheme.onBackground,
            )
            Spacer(Modifier.height(4.dp))
            Text(
                "글자와 요소 크기를 조절합니다. (${(sliderValue * 100).roundToInt()}%)",
                style = DenebType.hint,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(8.dp))
            // The scale is an absolute density multiplier (App.kt: density * uiScale).
            // On desktop Linux the default can be a HiDPI value up to ~4.4
            // (defaultUiScale = 1.1 * GDK env factor), so a fixed 0.8–1.3 range would
            // pin a HiDPI user at the max and let any move only shrink the app. Span
            // from 0.8 up to the platform default (plus headroom) and never below the
            // current value, keeping ~0.05 increments.
            val minScale = 0.8f
            val maxScale = maxOf(1.3f, defaultUiScale + 0.3f, uiScale)
            val sliderSteps = (((maxScale - minScale) / 0.05f).roundToInt() - 1).coerceAtLeast(0)
            Slider(
                value = sliderValue,
                onValueChange = { sliderValue = it },
                onValueChangeFinished = { appSettings.setUiScale(sliderValue) },
                valueRange = minScale..maxScale,
                steps = sliderSteps,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(8.dp))
            // Reset to the platform default (1.0 on phone, the HiDPI-derived value on
            // desktop Linux), not a hard-coded 100%.
            TextButton(
                onClick = {
                    haptics.tap()
                    appSettings.setUiScale(defaultUiScale)
                },
                enabled = uiScale != defaultUiScale,
            ) { Text("기본값(${(defaultUiScale * 100).roundToInt()}%)으로") }
        }
    }
}
