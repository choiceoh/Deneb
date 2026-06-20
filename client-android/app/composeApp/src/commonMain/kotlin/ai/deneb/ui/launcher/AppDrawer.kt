package ai.deneb.ui.launcher

import ai.deneb.deneb.DenebEmpty
import ai.deneb.deneb.DenebLoading
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebSearchField
import ai.deneb.ui.components.SectionedScrubList
import ai.deneb.ui.denebPressable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

/** One launchable app for the work-launcher app drawer (label + package — the
 *  Niagara text list shows labels, not icons). */
data class LauncherAppEntry(
    val label: String,
    val packageName: String,
)

/**
 * The work launcher's app drawer — a Niagara-style alphabetical TEXT list with a
 * ㄱㄴㄷ/A–Z scrub index ([SectionedScrubList]), in the Deneb idiom (flat AMOLED,
 * monochrome, no icon grid). Pure presentation — the platform supplies [apps]
 * (Android = PackageManager; desktop/iOS = stub) and [onLaunch] fires the launch
 * intent. Offline-first shell: never touches the gateway, so the home can reach
 * other apps even with the server down.
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
        if (q.isEmpty()) apps else apps.filter { it.label.contains(q, ignoreCase = true) }
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
            // the drawer flashed "앱 없음" on every open before the list populated.
            !loaded -> DenebLoading()

            filtered.isEmpty() -> DenebEmpty(if (apps.isEmpty()) "앱이 없습니다" else "검색 결과 없음")

            else -> SectionedScrubList(
                items = filtered,
                label = { it.label },
                key = { it.packageName },
            ) { app -> AppRow(app, onLaunch) }
        }
    }
}

@Composable
private fun AppRow(app: LauncherAppEntry, onLaunch: (String) -> Unit) {
    Text(
        app.label,
        style = DenebType.rowTitle,
        color = MaterialTheme.colorScheme.onBackground,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
        modifier = Modifier
            .fillMaxWidth()
            .denebPressable(onClick = { onLaunch(app.packageName) })
            .padding(horizontal = 20.dp, vertical = 11.dp),
    )
}
