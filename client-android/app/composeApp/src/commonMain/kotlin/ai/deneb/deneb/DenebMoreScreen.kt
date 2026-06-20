package ai.deneb.deneb

import ai.deneb.DenebApps
import ai.deneb.DenebBrowser
import ai.deneb.DenebCategories
import ai.deneb.DenebConfig
import ai.deneb.DenebContacts
import ai.deneb.DenebDashboard
import ai.deneb.DenebDiary
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
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.MenuBook
import androidx.compose.material.icons.outlined.AccountTree
import androidx.compose.material.icons.outlined.Apps
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
    val desc: String,
    // 업무 데이터 entry: hidden in the 챗봇 workspace (검색·카테고리). 할일·일기 stay.
    val workData: Boolean = false,
)

private val moreEntries = listOf(
    MoreEntry("앱", DenebApps, Icons.Outlined.Apps, "설치된 앱 실행"),
    MoreEntry("파트별 업무 현황", DenebDashboard, Icons.Outlined.Dashboard, "팀·파트별 일정·업무 한눈에", workData = true),
    MoreEntry("조직도", DenebOrgChart, Icons.Outlined.AccountTree, "그룹·회사·팀 구조 보기·편집", workData = true),
    MoreEntry("검색", DenebSearch, Icons.Outlined.Search, "메일·위키 통합 검색", workData = true),
    MoreEntry("할일", DenebTodo, Icons.Outlined.CheckCircle, "할 일 목록"),
    MoreEntry("일기", DenebDiary, Icons.AutoMirrored.Outlined.MenuBook, "일기 기록"),
    MoreEntry("카테고리", DenebCategories, Icons.Outlined.GridView, "위키 분류, 사람", workData = true),
    MoreEntry("전체 연락처", DenebContacts, Icons.Outlined.Contacts, "주소록 전체, 이름·번호 검색", workData = true),
    MoreEntry("노트북", DenebNotebooks, Icons.Outlined.Book, "딜·프로젝트 자료 모음", workData = true),
    MoreEntry("파일", DenebFiles, Icons.Outlined.Storage, "로컬 파일 탐색·검색·업로드", workData = true),
    MoreEntry("브라우저", DenebBrowser(""), Icons.Outlined.Public, "웹 페이지 열기 · 한국어 번역"),
    MoreEntry("설정", DenebConfig, Icons.Outlined.Settings, "환경설정, 모델, 플릿"),
)

/**
 * The 더보기 screen — the secondary sections that don't fit the four-tab bottom bar,
 * as a full page (not a sheet) so it reads like the other sections and keeps the
 * 더보기 tab in the navigation model (the bottom bar stays visible with 더보기 active).
 *
 * Uses the same grouped inset-card idiom as the settings hub (DenebGroup +
 * DenebListRow: leading icon, title, one-line summary, chevron) so 더보기 and 설정
 * read identically.
 */
@Composable
fun DenebMoreScreen(onBack: () -> Unit, onOpen: (Any) -> Unit, chatMode: Boolean = false) {
    val entries = if (chatMode) moreEntries.filterNot { it.workData } else moreEntries
    // Live voice dictation (system speech recognizer → chat). It is an input action,
    // not a file, so it lives here rather than cluttering the attach (+) button — that
    // opens a single picker and auto-routes by file type. Android-only (captures
    // present); hidden on desktop/iOS. It is the last row of the *same* group as the
    // nav entries (not a separate DenebGroup) — two stacked groups have no vertical
    // gap, so an extra group read as a second box jammed flush against the first.
    val captures = LocalCaptureActions.current
    DenebScreenScaffold(title = "더보기", onBack = onBack) {
        Column(
            Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(top = 4.dp, bottom = 24.dp),
        ) {
            DenebGroup {
                entries.forEachIndexed { i, entry ->
                    DenebListRow(
                        title = entry.label,
                        onClick = { onOpen(entry.dest) },
                        icon = entry.icon,
                        subtitle = entry.desc,
                        // Keep a divider on the last nav entry when the voice row follows it.
                        divider = i < entries.lastIndex || captures != null,
                    )
                }
                if (captures != null) {
                    DenebListRow(
                        title = "음성 입력",
                        onClick = captures.onVoiceInput,
                        icon = Icons.Outlined.KeyboardVoice,
                        subtitle = "말하면 받아써서 보냅니다",
                        divider = false,
                        chevron = false,
                    )
                }
            }
        }
    }
}
