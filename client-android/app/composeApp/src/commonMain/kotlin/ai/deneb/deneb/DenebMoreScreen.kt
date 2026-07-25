package ai.deneb.deneb

import ai.deneb.DenebApprovals
import ai.deneb.DenebBrowser
import ai.deneb.DenebCategories
import ai.deneb.DenebConfig
import ai.deneb.DenebContacts
import ai.deneb.DenebContactsDedup
import ai.deneb.DenebDashboard
import ai.deneb.DenebFiles
import ai.deneb.DenebGroupware
import ai.deneb.DenebNotebooks
import ai.deneb.DenebOrgChart
import ai.deneb.DenebProjectDigests
import ai.deneb.DenebRsi
import ai.deneb.DenebSearch
import ai.deneb.DenebSiteMap
import ai.deneb.DenebUsage
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebListRow
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.chat.composables.LocalCaptureActions
import ai.deneb.ui.icons.outlined.AccountTree
import ai.deneb.ui.icons.outlined.Assignment
import ai.deneb.ui.icons.outlined.AutoAwesome
import ai.deneb.ui.icons.outlined.Autorenew
import ai.deneb.ui.icons.outlined.Book
import ai.deneb.ui.icons.outlined.Business
import ai.deneb.ui.icons.outlined.Contacts
import ai.deneb.ui.icons.outlined.Dashboard
import ai.deneb.ui.icons.outlined.GridView
import ai.deneb.ui.icons.outlined.Insights
import ai.deneb.ui.icons.outlined.KeyboardVoice
import ai.deneb.ui.icons.outlined.Public
import ai.deneb.ui.icons.outlined.Storage
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Place
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp

internal data class MoreEntry(
    val label: String,
    val dest: Any,
    val icon: ImageVector,
    // A STABLE identity key (decoupled from [label], which can be renamed): matches the
    // destination's @SerialName route. Used to persist which tiles the user hid (settings
    // store the keys, never the labels) and to mark a tile as 항상 표시 (alwaysShown).
    val key: String,
    // Cannot be hidden via 설정. 설정 itself (hiding it would lock out the un-hide control)
    // — and 채팅, the assistant core, is a bottom-bar tab so it never appears here anyway.
    val alwaysShown: Boolean = false,
)

// Voice dictation (Android-only input action) tails this group.
private const val TOOLS_GROUP = "도구"

// The secondary sections, grouped into labeled inset cards — the same idiom as the
// settings hub (DenebConfigScreen.configGroups), so 더보기 and 설정 read identically.
// 채팅·피드·메일·달력 are first-class bottom-bar tabs, not here. 일기 is omitted (reachable
// via 카테고리). Icon + title only — no one-line descriptions (a hub you visit often reads
// cleaner without them). Add a section by appending to the right group.
//
// [MoreEntry.key] is the destination @SerialName (App.kt) — a stable id used both to filter
// out user-hidden tiles (설정 → "더보기 표시 항목") and, for 설정, to mark it 항상 표시.
internal val moreGroups: List<Pair<String, List<MoreEntry>>> = listOf(
    "업무 · 지식" to listOf(
        MoreEntry("결재", DenebApprovals, Icons.Outlined.Assignment, key = "deneb_approvals"),
        MoreEntry("그룹웨어", DenebGroupware, Icons.Outlined.Business, key = "deneb_groupware"),
        MoreEntry("파트별 업무 현황", DenebDashboard, Icons.Outlined.Dashboard, key = "deneb_dashboard"),
        MoreEntry("재귀적 자가개선", DenebRsi, Icons.Outlined.Autorenew, key = "deneb_rsi"),
        MoreEntry("사용량", DenebUsage, Icons.Outlined.Insights, key = "deneb_usage"),
        MoreEntry("프로젝트 진행상황", DenebProjectDigests, Icons.Outlined.Insights, key = "deneb_project_digests"),
        MoreEntry("현장 지도", DenebSiteMap, Icons.Outlined.Place, key = "deneb_site_map"),
        MoreEntry("조직도", DenebOrgChart, Icons.Outlined.AccountTree, key = "deneb_org"),
        MoreEntry("검색", DenebSearch, Icons.Outlined.Search, key = "deneb_search"),
        MoreEntry("카테고리", DenebCategories, Icons.Outlined.GridView, key = "deneb_categories"),
        MoreEntry("전체 연락처", DenebContacts, Icons.Outlined.Contacts, key = "deneb_contacts"),
        MoreEntry("연락처 정리", DenebContactsDedup, Icons.Outlined.AutoAwesome, key = "deneb_contacts_dedup"),
        MoreEntry("노트북", DenebNotebooks(), Icons.Outlined.Book, key = "deneb_notebooks"),
    ),
    TOOLS_GROUP to listOf(
        MoreEntry("파일", DenebFiles, Icons.Outlined.Storage, key = "deneb_files"),
        MoreEntry("브라우저", DenebBrowser(""), Icons.Outlined.Public, key = "deneb_browser"),
    ),
    "시스템" to listOf(
        MoreEntry("설정", DenebConfig, Icons.Outlined.Settings, key = "deneb_config", alwaysShown = true),
    ),
)

/** The tiles the user is allowed to hide — every entry that isn't [MoreEntry.alwaysShown].
 *  Drives the 설정 → "더보기 표시 항목" list; the keys here are the only ones a hidden-set
 *  persists. Flattened in display order. */
internal val hideableMoreEntries: List<MoreEntry> = moreGroups.flatMap { it.second }.filterNot { it.alwaysShown }

/**
 * Filter a group's entries for display: [hidden] hides the user's 설정-chosen tiles by
 * their stable [MoreEntry.key]. An [MoreEntry.alwaysShown] tile (설정) is never removed
 * even if its key somehow appears in [hidden]. Pure + deterministic so it is
 * unit-testable and preview-able.
 */
internal fun visibleMoreEntries(all: List<MoreEntry>, hidden: Set<String>): List<MoreEntry> = all.filter { entry ->
    if (!entry.alwaysShown && entry.key in hidden) return@filter false
    true
}

/**
 * The 더보기 screen — the secondary sections that don't fit the five-slot bottom bar
 * (피드·메일·채팅·달력·더보기), as a full page so it reads like the other sections and keeps
 * the 더보기 tab in the navigation model (the bottom bar stays visible with 더보기 active).
 *
 * Grouped labeled inset cards (DenebGroup(label) + DenebListRow), matching the settings
 * hub. [hiddenTiles] hides the user's 설정-chosen tiles (by stable key). A group that
 * empties out is skipped.
 */
@Composable
fun DenebMoreScreen(
    onBack: () -> Unit,
    onOpen: (Any) -> Unit,
    hiddenTiles: Set<String> = emptySet(),
) {
    // Live voice dictation (system speech recognizer → chat). An input action, not a
    // file, so it lives here rather than cluttering the attach (+) button. Android-only
    // (captures present); hidden on desktop/iOS.
    val captures = LocalCaptureActions.current
    DenebScreenScaffold(title = "더보기", onBack = onBack) {
        Column(
            Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(top = 4.dp, bottom = 24.dp),
        ) {
            moreGroups.forEach { (label, all) ->
                val entries = visibleMoreEntries(all, hiddenTiles)
                val withVoice = label == TOOLS_GROUP && captures != null
                if (entries.isEmpty() && !withVoice) return@forEach
                DenebGroup(label = label) {
                    entries.forEachIndexed { i, entry ->
                        DenebListRow(
                            title = entry.label,
                            onClick = { onOpen(entry.dest) },
                            icon = entry.icon,
                            divider = i < entries.lastIndex || withVoice,
                        )
                    }
                    if (withVoice) {
                        DenebListRow(
                            title = "음성 입력",
                            onClick = captures.onVoiceInput,
                            icon = Icons.Outlined.KeyboardVoice,
                            divider = false,
                            chevron = false,
                        )
                    }
                }
                Spacer(Modifier.height(20.dp))
            }
        }
    }
}
