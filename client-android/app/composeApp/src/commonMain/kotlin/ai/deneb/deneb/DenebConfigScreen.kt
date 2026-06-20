package ai.deneb.deneb

import ai.deneb.PlatformBackHandler
import ai.deneb.data.AppSettings
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebListRow
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.launcher.LauncherMode
import ai.deneb.ui.launcher.LauncherTab
import ai.deneb.ui.launcher.createLauncherMode
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Article
import androidx.compose.material.icons.outlined.Code
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Extension
import androidx.compose.material.icons.outlined.Home
import androidx.compose.material.icons.outlined.Hub
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.Memory
import androidx.compose.material.icons.outlined.Palette
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
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
 * system prompt) has no section of its own: it is edited inside the 프롬프트 코너
 * ([PromptsTab]), which reuses its prompt editor for that one file-backed doc via
 * miniapp.topicdocs.* (the topic is .md-backed, not prompt-override-JSON backed, so
 * it cannot be a prompts-store entry without breaking injection).
 *
 * People browsing is NOT a section here: it is a content destination with its
 * own drawer entry + full screen (DenebPeopleScreen), like mail and calendar.
 * Fleet HAS a section row, but its detail is the standalone DenebFleetScreen
 * (its own pager + scaffold) — the row routes to onOpenFleet rather than
 * pushing an in-place detail into this screen's shell.
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
    // Home-launcher mode is foss-Android-only; on every other target the section is
    // hidden (the alias doesn't exist, so there's nothing to toggle).
    val launcherMode = remember { createLauncherMode() }

    if (selected == null) {
        // Level 1 — the section list. Keeps the app navigation tab bar (this is a
        // top-level destination); back exits settings.
        DenebScreenScaffold(
            title = "설정",
            onBack = onBack,
            tabBar = navigationTabBar,
        ) {
            ConfigSectionList(
                launcherSupported = launcherMode.supported,
                onOpen = {
                    // Fleet opens its own full screen (its own pager + scaffold);
                    // every other section pushes into the in-place detail below.
                    if (it == ConfigTab.FLEET) onOpenFleet() else selectedOrdinal = it.ordinal
                },
            )
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
            ConfigTab.GATEWAY -> GatewayTab(appSettings, onBack, denebClient)

            ConfigTab.LAUNCHER -> LauncherTab(launcherMode)

            ConfigTab.APPEARANCE -> AppearanceTab(appSettings)

            ConfigTab.MODEL -> denebClient?.let { ModelTab(it) } ?: NotConnectedTab()

            ConfigTab.SKILLS -> denebClient?.let { SkillsTab(it, onOpenSkill) } ?: NotConnectedTab()

            ConfigTab.SELF_IMPROVEMENT_CODING -> denebClient?.let { SelfImprovementCodingTab(it) } ?: NotConnectedTab()

            ConfigTab.CRON -> denebClient?.let { CronTab(it, onOpenCron) } ?: NotConnectedTab()

            ConfigTab.PROMPTS -> denebClient?.let { PromptsTab(it) } ?: NotConnectedTab()

            ConfigTab.OBSERVE -> denebClient?.let { ObserveTab(it) } ?: NotConnectedTab()

            ConfigTab.WORMHOLE -> denebClient?.let { WormholeTab(it) } ?: NotConnectedTab()

            ConfigTab.VERSION -> VersionTab(denebClient)

            // FLEET never becomes an in-place detail: its row opens the standalone
            // DenebFleetScreen (onOpenFleet), so this branch is unreachable.
            ConfigTab.FLEET -> Unit
        }
    }
}

/** Display-only grouping of [ConfigTab]s into inset cards. The enum order/ordinals
 *  are unchanged (saved detail ordinals survive); this only decides which rows share
 *  a [DenebGroup]. */
private val configGroups: List<Pair<String, List<ConfigTab>>> = listOf(
    "시스템" to listOf(ConfigTab.GATEWAY, ConfigTab.APPEARANCE, ConfigTab.MODEL),
    "기기" to listOf(ConfigTab.LAUNCHER),
    "자동화 · 관찰" to listOf(ConfigTab.SKILLS, ConfigTab.SELF_IMPROVEMENT_CODING, ConfigTab.CRON, ConfigTab.PROMPTS, ConfigTab.OBSERVE),
    "라우팅 · 인프라" to listOf(ConfigTab.WORMHOLE, ConfigTab.FLEET),
    "정보" to listOf(ConfigTab.VERSION),
)

/** The settings-hub section list, in the grouped inset-card idiom ([DenebGroup] +
 *  [DenebListRow]): each [ConfigTab] is a row with its icon, label, and one-line
 *  summary. Tapping pushes into that section's detail. */
@Composable
private fun ConfigSectionList(launcherSupported: Boolean, onOpen: (ConfigTab) -> Unit) {
    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(top = 4.dp, bottom = 24.dp),
    ) {
        configGroups.forEach { (label, tabs) ->
            // The 런처 section only exists where Deneb can be a home app (foss Android);
            // drop the row — and any group it empties — everywhere else.
            val rows = tabs.filter { it != ConfigTab.LAUNCHER || launcherSupported }
            if (rows.isEmpty()) return@forEach
            DenebGroup(label = label) {
                rows.forEachIndexed { i, tab ->
                    DenebListRow(
                        title = tab.label,
                        onClick = { onOpen(tab) },
                        icon = tab.icon,
                        subtitle = tab.desc,
                        divider = i < rows.lastIndex,
                    )
                }
            }
            Spacer(Modifier.height(20.dp))
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
private enum class ConfigTab(val label: String, val desc: String, val icon: ImageVector) {
    GATEWAY("게이트웨이", "연결, 상태, 연락처 동기화", Icons.Outlined.Dns),
    APPEARANCE("화면", "테마, UI 배율", Icons.Outlined.Palette),
    MODEL("모델", "역할별 모델 지정, 엔드포인트", Icons.Outlined.Memory),
    SKILLS("스킬", "설치된 스킬, Propus", Icons.Outlined.Extension),
    CRON("크론", "예약 작업", Icons.Outlined.Schedule),
    OBSERVE("관찰", "게이트웨이 동작, 로그", Icons.Outlined.Visibility),
    WORMHOLE("Wormhole", "모델 라우터 상태, 기능 토글", Icons.Outlined.Hub),

    // Appended at the end so existing saved detail ordinals (rotation / process
    // death) keep pointing at the same section across this change.
    FLEET("플릿", "GPU 노드 상태, 모델 기동/중지, 작업 로그", Icons.Outlined.Storage),
    VERSION("버전", "현재 빌드, 패치노트, 업데이트", Icons.Outlined.Info),
    PROMPTS("프롬프트 코너", "자동 분석·도구 프롬프트, 토픽 배경 편집", Icons.Outlined.Article),
    SELF_IMPROVEMENT_CODING("자가개선 코딩", "코딩 수정 후보, 적용 대기열", Icons.Outlined.Code),
    LAUNCHER("런처", "홈 화면으로 사용", Icons.Outlined.Home),
}
