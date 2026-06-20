package ai.deneb.ui.launcher

import ai.deneb.deneb.DenebEmpty
import ai.deneb.deneb.DenebLoading
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebSearchField
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebPressable
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

/**
 * One launchable app for the work-launcher app drawer (Phase 0). [icon] is null on
 * platforms that have no installed-app list (the desktop/iOS stubs, or when the
 * icon can't be loaded) and renders a lettered placeholder instead.
 */
data class LauncherAppEntry(
    val label: String,
    val packageName: String,
    val icon: ImageBitmap? = null,
)

/**
 * The work launcher's app drawer: a live-filtered grid of installed apps in the
 * Deneb idiom (flat AMOLED, DenebType labels). Pure presentation — the platform
 * supplies [apps] (Android = PackageManager; desktop/iOS = stub) and [onLaunch]
 * fires the launch intent. This is the offline-first shell layer: it never touches
 * the gateway, so the home can always reach other apps even when the server is down.
 */
@Composable
fun AppDrawer(
    apps: List<LauncherAppEntry>,
    onLaunch: (String) -> Unit,
    modifier: Modifier = Modifier,
    loaded: Boolean = true,
) {
    var query by remember { mutableStateOf("") }
    val filtered = remember(apps, query) {
        val q = query.trim()
        if (q.isEmpty()) {
            apps
        } else {
            apps.filter { it.label.contains(q, ignoreCase = true) }
        }
    }
    Column(modifier.fillMaxSize()) {
        DenebSearchField(
            query = query,
            onQueryChange = { query = it },
            placeholder = "앱 검색",
            clearContentDescription = "검색 지우기",
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            // Launcher idiom: when the query narrows to a single app, the keyboard's
            // Search/Enter launches it directly — type a few letters, hit enter, gone.
            onSearch = { filtered.singleOrNull()?.let { onLaunch(it.packageName) } },
        )
        when {
            // Distinct loading vs empty: the provider loads off-thread, so without this
            // the drawer flashed "앱 없음" on every open before the grid populated.
            !loaded -> DenebLoading()

            filtered.isEmpty() -> DenebEmpty(if (apps.isEmpty()) "앱이 없습니다" else "검색 결과 없음")

            else -> LazyVerticalGrid(
                columns = GridCells.Adaptive(minSize = 84.dp),
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(horizontal = 12.dp, vertical = 8.dp),
            ) {
                items(filtered, key = { it.packageName }) { app ->
                    AppTile(app = app, onLaunch = onLaunch)
                }
            }
        }
    }
}

@Composable
private fun AppTile(app: LauncherAppEntry, onLaunch: (String) -> Unit) {
    Column(
        Modifier
            .fillMaxWidth()
            .denebPressable(onClick = { onLaunch(app.packageName) })
            .padding(vertical = 12.dp, horizontal = 4.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        if (app.icon != null) {
            Image(bitmap = app.icon, contentDescription = app.label, modifier = Modifier.size(48.dp))
        } else {
            // No real icon (desktop/iOS stub, or load failure): a lettered disc.
            Box(
                Modifier.size(48.dp).clip(CircleShape).background(denebHint().copy(alpha = 0.18f)),
                contentAlignment = Alignment.Center,
            ) {
                Text(app.label.take(1).uppercase(), style = DenebType.subject, color = denebHint())
            }
        }
        Spacer(Modifier.height(6.dp))
        Text(
            app.label,
            style = DenebType.meta,
            color = MaterialTheme.colorScheme.onBackground,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}
