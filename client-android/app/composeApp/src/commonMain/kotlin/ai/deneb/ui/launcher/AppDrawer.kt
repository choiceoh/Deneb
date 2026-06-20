package ai.deneb.ui.launcher

import ai.deneb.deneb.DenebEmpty
import ai.deneb.deneb.DenebLoading
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.SectionedScrubList
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebPressable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PushPin
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.input.nestedscroll.NestedScrollConnection
import androidx.compose.ui.input.nestedscroll.NestedScrollSource
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Velocity
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
 *
 * [onExit] is the reverse of the swipe-UP that opened this drawer: pulling DOWN at
 * the very top of the list (overscroll) returns to 자체앱. See [exitOnTopOverscroll].
 */
@Composable
fun AppDrawer(
    apps: List<LauncherAppEntry>,
    onLaunch: (String) -> Unit,
    modifier: Modifier = Modifier,
    loaded: Boolean = true,
    onExit: () -> Unit = {},
    // Package names pinned to the 자체앱 favorites home — pinned rows show a marker, and
    // long-pressing any row toggles its pin (the only place pins are added/removed).
    pinned: Set<String> = emptySet(),
    onTogglePin: (String) -> Unit = {},
) {
    Column(modifier.fillMaxSize().nestedScroll(exitOnTopOverscroll(onExit))) {
        when {
            // Distinct loading vs empty: the provider loads off-thread, so without this
            // the drawer flashed "앱 없음" on every open before the list populated.
            !loaded -> DenebLoading()

            apps.isEmpty() -> DenebEmpty("앱이 없습니다")

            else -> SectionedScrubList(
                // Section/scrub by the Korean reading (YouTube→유튜브→ㅇ) so the index is
                // a unified ㄱㄴㄷ, not a mixed ㄱㄴㄷ+A–Z. The row still shows the raw label.
                items = apps,
                label = { koreanAppSortKey(it.label) },
                key = { it.packageName },
            ) { app -> AppRow(app, app.packageName in pinned, onLaunch, onTogglePin) }
        }
    }
}

/**
 * A [NestedScrollConnection] that fires [onExit] when the user pulls DOWN past a
 * commit distance at the very top of the list — the mirror of the swipe-UP that
 * opened the drawer, so the same flick reverses it.
 *
 * Why nested scroll and not a pointerInput wrapper: this only reacts to the
 * downward delta the inner LazyColumn leaves *unconsumed* at the top (overscroll),
 * so it never steals a normal scroll drag. And [onPostScroll] runs before the
 * platform stretch-overscroll effect consumes that leftover (overscroll wraps the
 * scroll+nested dispatch), so we both measure the pull and still let the stretch
 * glow render as feedback — we return [Offset.Zero] and consume nothing. Fling
 * momentum (source != [NestedScrollSource.UserInput]) is excluded, so flicking up
 * to the top never accidentally triggers an exit.
 */
@Composable
private fun exitOnTopOverscroll(onExit: () -> Unit): NestedScrollConnection {
    val commitPx = with(LocalDensity.current) { 80.dp.toPx() }
    val latestExit by rememberUpdatedState(onExit)
    return remember(commitPx) {
        object : NestedScrollConnection {
            private var pulled = 0f

            override fun onPreScroll(available: Offset, source: NestedScrollSource): Offset {
                // Dragging back up means we're scrolling into the list — drop any pending pull.
                if (available.y < 0f) pulled = 0f
                return Offset.Zero
            }

            override fun onPostScroll(consumed: Offset, available: Offset, source: NestedScrollSource): Offset {
                // available.y > 0 here = downward drag the list couldn't consume (at top).
                if (source == NestedScrollSource.UserInput && available.y > 0f) {
                    pulled += available.y
                    if (pulled >= commitPx) {
                        pulled = 0f
                        latestExit()
                    }
                }
                return Offset.Zero
            }

            override suspend fun onPreFling(available: Velocity): Velocity {
                // Released without committing — reset so the next gesture starts clean.
                pulled = 0f
                return Velocity.Zero
            }
        }
    }
}

@Composable
private fun AppRow(
    app: LauncherAppEntry,
    pinned: Boolean,
    onLaunch: (String) -> Unit,
    onTogglePin: (String) -> Unit,
) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            // Tap launches; long-press toggles the pin to the 자체앱 favorites home.
            .denebPressable(
                onClick = { onLaunch(app.packageName) },
                onLongClick = { onTogglePin(app.packageName) },
            )
            .padding(horizontal = 20.dp, vertical = 11.dp),
    ) {
        Text(
            app.label,
            style = DenebType.rowTitle,
            color = MaterialTheme.colorScheme.onBackground,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
        if (pinned) {
            Icon(
                Icons.Filled.PushPin,
                contentDescription = "핀고정됨",
                tint = denebHint(),
                modifier = Modifier.size(15.dp),
            )
        }
    }
}
