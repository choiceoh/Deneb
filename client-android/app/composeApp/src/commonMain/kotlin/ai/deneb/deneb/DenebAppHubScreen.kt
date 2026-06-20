package ai.deneb.deneb

import ai.deneb.DenebBrowser
import ai.deneb.DenebCalendar
import ai.deneb.DenebCategories
import ai.deneb.DenebConfig
import ai.deneb.DenebDashboard
import ai.deneb.DenebDiary
import ai.deneb.DenebFiles
import ai.deneb.DenebMail
import ai.deneb.DenebNotebooks
import ai.deneb.DenebOrgChart
import ai.deneb.DenebSearch
import ai.deneb.DenebTodo
import ai.deneb.Home
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Chat
import androidx.compose.material.icons.automirrored.outlined.MenuBook
import androidx.compose.material.icons.outlined.AccountTree
import androidx.compose.material.icons.outlined.Book
import androidx.compose.material.icons.outlined.CalendarMonth
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Dashboard
import androidx.compose.material.icons.outlined.Email
import androidx.compose.material.icons.outlined.GridView
import androidx.compose.material.icons.outlined.Public
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp

private data class AppTile(
    val label: String,
    val dest: Any,
    val icon: ImageVector,
    // 업무 데이터 tile: hidden in the 챗봇 workspace (메일·달력·검색·조직도·파트별현황·
    // 카테고리·노트북·파일). 채팅·할일·일기·설정 stay in both workspaces.
    val workData: Boolean = false,
)

// 자체앱 — Deneb's own mini-apps as a home-screen launcher grid. This is the single
// source for the launcher; it absorbed the old 더보기 list (메일·달력·검색·할일·일기·
// 조직도·파트별현황·카테고리·노트북·파일·설정) plus 채팅. 채팅 leads (used most, so top-
// left and eye-catching). 브라우저 = the in-app translation browser (DenebBrowser, blank
// start): it lives here as a tile because the 인터넷 bottom tab now launches Samsung
// Internet (the external browser) instead — so this tile keeps the in-app translating
// browser reachable. Add a tile by appending here.
private val appHubTiles = listOf(
    AppTile("채팅", Home, Icons.AutoMirrored.Outlined.Chat),
    AppTile("메일", DenebMail, Icons.Outlined.Email, workData = true),
    AppTile("달력", DenebCalendar, Icons.Outlined.CalendarMonth, workData = true),
    AppTile("검색", DenebSearch, Icons.Outlined.Search, workData = true),
    AppTile("할일", DenebTodo, Icons.Outlined.CheckCircle),
    AppTile("일기", DenebDiary, Icons.AutoMirrored.Outlined.MenuBook),
    AppTile("조직도", DenebOrgChart, Icons.Outlined.AccountTree, workData = true),
    AppTile("파트별 업무 현황", DenebDashboard, Icons.Outlined.Dashboard, workData = true),
    AppTile("카테고리", DenebCategories, Icons.Outlined.GridView, workData = true),
    AppTile("노트북", DenebNotebooks, Icons.Outlined.Book, workData = true),
    AppTile("파일", DenebFiles, Icons.Outlined.Storage, workData = true),
    // In-app translating browser (blank start — the address bar takes over). Distinct
    // from the 인터넷 tab, which launches the external Samsung Internet app.
    AppTile("브라우저", DenebBrowser(""), Icons.Outlined.Public),
    AppTile("설정", DenebConfig, Icons.Outlined.Settings),
)

/**
 * The 자체앱 launcher (bottom tab center): Deneb's own mini-apps as rounded monochrome
 * tiles (home-screen idiom — controls stay Material, the skin stays Deneb-calm). It is
 * reached from the bottom bar (업무 workspace only), so the host renders the bar; this
 * screen just lists the tiles. [chatMode] hides the 업무 데이터 tiles, mirroring the old
 * 더보기 filter (the 챗봇 workspace keeps only 채팅·할일·일기·설정). The stateless
 * [DenebAppHubContent] is split out so renderPreviews can exercise the grid with no client.
 */
@Composable
fun DenebAppHubScreen(onBack: () -> Unit, onOpen: (Any) -> Unit, chatMode: Boolean = false) {
    DenebScreenScaffold(title = "자체앱", onBack = onBack) {
        DenebAppHubContent(onOpen = onOpen, chatMode = chatMode)
    }
}

@Composable
fun DenebAppHubContent(onOpen: (Any) -> Unit, chatMode: Boolean = false) {
    val haptics = rememberHaptics()
    val tiles = if (chatMode) appHubTiles.filterNot { it.workData } else appHubTiles
    // Fixed 4 columns (user's call: a 4–5 wide launcher grid). At phone width (412dp)
    // four columns give ~88dp tiles — wide enough for the icon chip and a two-line
    // Korean label ("파트별 업무 현황"). Fixed (not Adaptive) so the column count is
    // stable across phone widths instead of drifting with minSize rounding.
    LazyVerticalGrid(
        columns = GridCells.Fixed(4),
        contentPadding = PaddingValues(16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
        modifier = Modifier.fillMaxWidth(),
    ) {
        items(tiles) { tile ->
            AppTileItem(
                tile = tile,
                onClick = {
                    haptics.tap()
                    onOpen(tile.dest)
                },
            )
        }
    }
}

@Composable
private fun AppTileItem(tile: AppTile, onClick: () -> Unit) {
    Column(
        horizontalAlignment = Alignment.CenterHorizontally,
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .clickable(onClick = onClick)
            .padding(vertical = 8.dp),
    ) {
        Box(
            modifier = Modifier
                .size(60.dp)
                .clip(RoundedCornerShape(16.dp))
                // Faint monochrome wash (no color fill) — keeps the AMOLED/mono idiom.
                .background(MaterialTheme.colorScheme.onBackground.copy(alpha = 0.06f)),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                tile.icon,
                contentDescription = tile.label,
                tint = MaterialTheme.colorScheme.onBackground,
                modifier = Modifier.size(28.dp),
            )
        }
        Spacer(Modifier.height(8.dp))
        Text(
            tile.label,
            style = DenebType.meta,
            color = MaterialTheme.colorScheme.onBackground,
            textAlign = TextAlign.Center,
            // Two lines so "파트별 업무 현황" wraps cleanly instead of truncating.
            maxLines = 2,
        )
    }
}
