package ai.deneb.deneb

import ai.deneb.DenebBrowser
import ai.deneb.DenebCategories
import ai.deneb.DenebConfig
import ai.deneb.DenebContacts
import ai.deneb.DenebDashboard
import ai.deneb.DenebFiles
import ai.deneb.DenebNotebooks
import ai.deneb.DenebOrgChart
import ai.deneb.DenebSearch
import ai.deneb.DenebTodo
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebListRow
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.chat.composables.LocalCaptureActions
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AccountTree
import androidx.compose.material.icons.outlined.Book
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Contacts
import androidx.compose.material.icons.outlined.Dashboard
import androidx.compose.material.icons.outlined.GridView
import androidx.compose.material.icons.outlined.KeyboardVoice
import androidx.compose.material.icons.outlined.Public
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp

private data class MoreEntry(
    val label: String,
    val dest: Any,
    val icon: ImageVector,
    // 업무 데이터 entry: hidden in the 챗봇 workspace. 할일·브라우저·설정 stay in both.
    val workData: Boolean = false,
)

// Voice dictation (Android-only input action) tails this group.
private const val TOOLS_GROUP = "할일 · 도구"

// The secondary sections, grouped into labeled inset cards — the same idiom as the
// settings hub (DenebConfigScreen.configGroups), so 더보기 and 설정 read identically.
// 채팅·피드·메일·달력 are first-class bottom-bar tabs, not here. 일기 is omitted (reachable
// via 카테고리). Icon + title only — no one-line descriptions (a hub you visit often reads
// cleaner without them). Add a section by appending to the right group.
private val moreGroups: List<Pair<String, List<MoreEntry>>> = listOf(
    "업무 · 지식" to listOf(
        MoreEntry("파트별 업무 현황", DenebDashboard, Icons.Outlined.Dashboard, workData = true),
        MoreEntry("조직도", DenebOrgChart, Icons.Outlined.AccountTree, workData = true),
        MoreEntry("검색", DenebSearch, Icons.Outlined.Search, workData = true),
        MoreEntry("카테고리", DenebCategories, Icons.Outlined.GridView, workData = true),
        MoreEntry("전체 연락처", DenebContacts, Icons.Outlined.Contacts, workData = true),
        MoreEntry("노트북", DenebNotebooks, Icons.Outlined.Book, workData = true),
    ),
    TOOLS_GROUP to listOf(
        MoreEntry("할일", DenebTodo, Icons.Outlined.CheckCircle),
        MoreEntry("파일", DenebFiles, Icons.Outlined.Storage, workData = true),
        MoreEntry("브라우저", DenebBrowser(""), Icons.Outlined.Public),
    ),
    "시스템" to listOf(
        MoreEntry("설정", DenebConfig, Icons.Outlined.Settings),
    ),
)

/**
 * The 더보기 screen — the secondary sections that don't fit the five-slot bottom bar
 * (피드·메일·채팅·달력·더보기), as a full page so it reads like the other sections and keeps
 * the 더보기 tab in the navigation model (the bottom bar stays visible with 더보기 active).
 *
 * Grouped labeled inset cards (DenebGroup(label) + DenebListRow), matching the settings
 * hub. [chatMode] hides the 업무 데이터 entries; a group that empties out is skipped.
 */
@Composable
fun DenebMoreScreen(onBack: () -> Unit, onOpen: (Any) -> Unit, chatMode: Boolean = false) {
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
                val entries = if (chatMode) all.filterNot { it.workData } else all
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
