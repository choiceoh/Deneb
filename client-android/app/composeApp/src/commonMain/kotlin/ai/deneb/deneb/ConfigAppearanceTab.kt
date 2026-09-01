package ai.deneb.deneb

import ai.deneb.data.AppSettings
import ai.deneb.defaultUiScale
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
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
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import kotlin.math.roundToInt

/**
 * Settings hub "화면" tab. Theme is no longer here — the app ships one palette,
 * OLED black (ADR 0007), so the only thing left to choose is UI scale. It is wired
 * live in App.kt (which observes [AppSettings.uiScaleFlow]), so moving the slider
 * rescales the whole app immediately. Hosted by [DenebConfigScreen]'s pager.
 */
@Composable
internal fun AppearanceTab(appSettings: AppSettings) {
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
        DenebSectionLabel("화면 배율", topPadding = 0.dp)
        SettingsCard {
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
                // steps snaps the value, so a changed value IS a crossed notch —
                // ticking on every onValueChange would buzz per pixel instead.
                onValueChange = {
                    if (it != sliderValue) haptics.segmentTick()
                    sliderValue = it
                },
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
