package ai.deneb.deneb

import ai.deneb.Platform
import ai.deneb.PlatformBackHandler
import ai.deneb.currentPlatform
import ai.deneb.data.AppSettings
import ai.deneb.ui.DenebRow
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

/**
 * Deneb hub + settings as a two-level master/detail screen (the "더보기" surface):
 * gateway config plus the secondary surfaces — appearance, model, skills, cron,
 * observability.
 *
 * Level 1 is a plain list of sections (Android-Settings style): tapping a row
 * pushes into that section's detail screen, which carries its own `←` back
 * affordance (and honors system back) to return to the list. The earlier pill
 * tab bar grew unwieldy as sections piled up; a list scales to any number of
 * sections without horizontal crowding.
 *
 * This file is only the frame (list + detail shell); each section's content
 * lives in its own Config*Tab.kt file ([GatewayTab], [AppearanceTab], [ModelTab],
 * [SkillsTab], [CronTab], [ObserveTab]) so a section can grow without re-bloating
 * this screen.
 *
 * The per-topic knowledge doc (workspace/topics/&lt;key&gt;.md, injected into the
 * system prompt) has no section on purpose: it is edited by asking the agent in
 * chat — the injected prompt block carries its source path (gateway
 * system_prompt.go).
 *
 * People browsing and fleet management are NOT sections here: fleet is an
 * operational surface with its own full screen (DenebFleetScreen, same frame as
 * this one); people is a content destination with its own drawer entry + full
 * screen (DenebPeopleScreen), like mail and calendar. The settings hub stays
 * configuration-only.
 */
@Composable
fun DenebConfigScreen(
    appSettings: AppSettings,
    onBack: () -> Unit,
    denebClient: DenebGatewayClient? = null,
    onOpenSkill: (String) -> Unit = {},
    onOpenCron: (String) -> Unit = {},
    onOpenFleet: () -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    // -1 = the section list; otherwise the open section's ordinal. Saved so the
    // detail survives rotation / process death like any pushed screen.
    var selectedOrdinal by rememberSaveable { mutableStateOf(-1) }
    val selected = ConfigTab.entries.getOrNull(selectedOrdinal)

    if (selected == null) {
        // Level 1 — the section list. Keeps the app navigation tab bar (this is a
        // top-level destination); back exits settings (dropped on Desktop, where
        // the persistent sidebar is the navigation).
        DenebScreenScaffold(
            title = "설정",
            onBack = onBack,
            tabBar = navigationTabBar,
            showBack = currentPlatform !is Platform.Desktop,
        ) {
            ConfigSectionList(onOpen = { selectedOrdinal = it.ordinal })
        }
        return
    }

    // Level 2 — the section detail. Back returns to the list (both the top-bar `←`
    // and system back), so it always shows a back affordance even on Desktop.
    PlatformBackHandler(enabled = true) { selectedOrdinal = -1 }
    DenebScreenScaffold(
        title = selected.label,
        onBack = { selectedOrdinal = -1 },
        showBack = true,
    ) {
        when (selected) {
            ConfigTab.GATEWAY -> GatewayTab(appSettings, onBack, denebClient, onOpenFleet)
            ConfigTab.APPEARANCE -> AppearanceTab(appSettings)
            ConfigTab.MODEL -> denebClient?.let { ModelTab(it) } ?: NotConnectedTab()
            ConfigTab.SKILLS -> denebClient?.let { SkillsTab(it, onOpenSkill) } ?: NotConnectedTab()
            ConfigTab.CRON -> denebClient?.let { CronTab(it, onOpenCron) } ?: NotConnectedTab()
            ConfigTab.OBSERVE -> denebClient?.let { ObserveTab(it) } ?: NotConnectedTab()
        }
    }
}

/** The settings-hub section list: one tappable [DenebRow] per [ConfigTab], in
 *  display order, each with its label + a one-line summary of what it holds. */
@Composable
private fun ConfigSectionList(onOpen: (ConfigTab) -> Unit) {
    val haptics = rememberHaptics()
    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp),
    ) {
        ConfigTab.entries.forEach { entry ->
            DenebRow(onClick = {
                haptics.tap()
                onOpen(entry)
            }) {
                Text(
                    entry.label,
                    style = DenebType.rowTitle,
                    color = MaterialTheme.colorScheme.onBackground,
                )
                Text(
                    entry.desc,
                    style = DenebType.rowSubtitle,
                    color = denebHint(),
                )
            }
        }
    }
}

/** Centered one-line empty state shared by the settings-hub list sections. */
@Composable
internal fun EmptyTab(text: String) {
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        Text(text, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** Shown when a section needs the gateway client but it is not connected yet. */
@Composable
private fun NotConnectedTab() = EmptyTab("게이트웨이에 연결되지 않았습니다")

/** The settings-hub sections, in display order. The screen renders [entries] as
 *  the section list and switches detail content by enum, so a reorder/rename
 *  happens in one place. [desc] is the one-line summary under each list row. */
private enum class ConfigTab(val label: String, val desc: String) {
    GATEWAY("게이트웨이", "연결, 버전, 연락처 동기화"),
    APPEARANCE("화면", "테마, UI 배율"),
    MODEL("모델", "역할별 모델 지정, 엔드포인트"),
    SKILLS("스킬", "설치된 스킬, 수명 주기"),
    CRON("크론", "예약 작업"),
    OBSERVE("관찰", "게이트웨이 동작, 로그"),
}
