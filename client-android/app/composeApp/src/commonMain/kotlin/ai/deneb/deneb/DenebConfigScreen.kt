package ai.deneb.deneb

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import ai.deneb.Platform
import ai.deneb.currentPlatform
import ai.deneb.data.AppSettings
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.handCursor
import kotlinx.coroutines.launch

/**
 * Deneb hub + settings as a tabbed screen (the "더보기" surface): gateway config
 * plus the secondary surfaces — model, cron, observability — each as its
 * own tab so they live here instead of crowding the chat top bar.
 *
 * This file is only the frame (header + pill tab bar + pager); each tab's content
 * lives in its own Config*Tab.kt file ([GatewayTab], [ModelTab], [SkillsTab],
 * [CronTab], [ObserveTab]) so a tab can grow without re-bloating this screen.
 *
 * The per-topic knowledge doc (workspace/topics/&lt;key&gt;.md, injected into the
 * system prompt) has no tab on purpose: it is edited by asking the agent in chat
 * — the injected prompt block carries its source path (gateway system_prompt.go).
 *
 * People browsing is NOT a tab here: it is a content destination with its own
 * drawer entry + full screen (DenebPeopleScreen), like mail and calendar. The
 * settings hub stays configuration-only.
 */
@Composable
fun DenebConfigScreen(
    appSettings: AppSettings,
    onBack: () -> Unit,
    denebClient: DenebGatewayClient? = null,
    onOpenCron: (String) -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val pagerState = rememberPagerState(pageCount = { ConfigTab.entries.size })
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()

    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        // imePadding shrinks the column (and its weighted pager) above the soft
        // keyboard, so a focused settings field low in a tab stays visible instead of
        // hiding behind it (edge-to-edge: the app owns the IME inset).
        Column(Modifier.fillMaxSize().statusBarsPadding().imePadding()) {
            if (navigationTabBar != null) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) { navigationTabBar() }
            }
            Row(
                modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp, top = 12.dp, bottom = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text("설정", style = DenebType.viewTitle, modifier = Modifier.weight(1f))
                // Desktop: the persistent sidebar is the navigation — a close
                // affordance on a top-level section is redundant there.
                if (currentPlatform !is Platform.Desktop) {
                    TextButton(onClick = onBack) { Text("닫기") }
                }
            }
            // Pill-style tabs (no underline) — mirrors the upstream "고급 설정" tab selector:
            // each tab is a rounded Surface, the selected one gets a soft primary tint.
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .horizontalScroll(rememberScrollState())
                    .padding(horizontal = 12.dp, vertical = 4.dp),
                horizontalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                ConfigTab.entries.forEachIndexed { idx, entry ->
                    val isSelected = pagerState.currentPage == idx
                    Surface(
                        modifier = Modifier
                            .handCursor()
                            .clip(RoundedCornerShape(50))
                            .selectable(
                                selected = isSelected,
                                role = Role.Tab,
                                onClick = { haptics.tap(); scope.launch { pagerState.animateScrollToPage(idx) } },
                            ),
                        shape = RoundedCornerShape(50),
                        color = if (isSelected) {
                            MaterialTheme.colorScheme.primary.copy(alpha = 0.2f)
                        } else {
                            Color.Transparent
                        },
                    ) {
                        Text(
                            text = entry.label,
                            modifier = Modifier.padding(horizontal = 16.dp, vertical = 10.dp),
                            color = if (isSelected) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
                            style = MaterialTheme.typography.labelLarge,
                            fontWeight = if (isSelected) FontWeight.SemiBold else FontWeight.Normal,
                            maxLines = 1,
                        )
                    }
                }
            }
            // Swipe left/right to move between tabs; the pager claims horizontal
            // drags while each tab's own column keeps its vertical scroll. Tapping a
            // pill animates here too, so the bar and pages stay in lockstep.
            HorizontalPager(
                state = pagerState,
                modifier = Modifier.weight(1f).fillMaxWidth(),
            ) { page ->
                when (ConfigTab.entries[page]) {
                    ConfigTab.GATEWAY -> GatewayTab(appSettings, onBack, denebClient)
                    ConfigTab.APPEARANCE -> AppearanceTab(appSettings)
                    ConfigTab.MODEL -> denebClient?.let { ModelTab(it) }
                    ConfigTab.SKILLS -> denebClient?.let { SkillsTab(it) }
                    ConfigTab.CRON -> denebClient?.let { CronTab(it, onOpenCron) }
                    ConfigTab.OBSERVE -> denebClient?.let { ObserveTab(it) }
                }
            }
        }
    }
}

/** Centered one-line empty state shared by the settings-hub list tabs. */
@Composable
internal fun EmptyTab(text: String) {
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        Text(text, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** The settings-hub tabs, in display order. The screen renders [entries] as the
 *  pill row and switches content by enum, so a reorder/rename happens in one place. */
private enum class ConfigTab(val label: String) {
    GATEWAY("게이트웨이"),
    APPEARANCE("화면"),
    MODEL("모델"),
    SKILLS("스킬"),
    CRON("크론"),
    OBSERVE("관찰"),
}
