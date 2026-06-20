package ai.deneb.deneb

import ai.deneb.DenebBrowser
import ai.deneb.DenebCalendar
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
import androidx.compose.material.icons.outlined.CalendarMonth
import androidx.compose.material.icons.outlined.Public
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
)

// 자체앱 — Deneb's own mini-apps as a home-screen-style launcher, distinct from
// 더보기's settings/utility list. Browser and chat live here (chat is also a bottom
// tab per the user's call); calendar moved off the bottom bar into this grid. Add a
// tile by appending here — this list is the single source for the launcher.
private val appHubTiles = listOf(
    AppTile("브라우저", DenebBrowser(""), Icons.Outlined.Public),
    AppTile("채팅", Home, Icons.AutoMirrored.Outlined.Chat),
    AppTile("달력", DenebCalendar, Icons.Outlined.CalendarMonth),
)

/**
 * The 자체앱 launcher (bottom tab 3): Deneb's own mini-apps as rounded monochrome
 * tiles (home-screen idiom — controls stay Material, the skin stays Deneb-calm). It
 * is reached from the bottom bar (업무 workspace only), so the host renders the bar;
 * this screen just lists the tiles. The stateless [DenebAppHubContent] is split out
 * so renderPreviews can exercise the grid with no client.
 */
@Composable
fun DenebAppHubScreen(onBack: () -> Unit, onOpen: (Any) -> Unit) {
    DenebScreenScaffold(title = "자체앱", onBack = onBack) {
        DenebAppHubContent(onOpen = onOpen)
    }
}

@Composable
fun DenebAppHubContent(onOpen: (Any) -> Unit) {
    val haptics = rememberHaptics()
    LazyVerticalGrid(
        columns = GridCells.Adaptive(minSize = 92.dp),
        contentPadding = PaddingValues(16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
        modifier = Modifier.fillMaxWidth(),
    ) {
        items(appHubTiles) { tile ->
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
            maxLines = 1,
        )
    }
}
