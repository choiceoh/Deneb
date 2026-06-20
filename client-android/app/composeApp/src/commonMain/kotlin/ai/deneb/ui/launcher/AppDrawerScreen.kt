package ai.deneb.ui.launcher

import ai.deneb.data.AppSettings
import ai.deneb.getBackgroundDispatcher
import ai.deneb.ui.DenebScreenScaffold
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.withContext
import org.koin.compose.koinInject

/**
 * Stateful shell for the work-launcher app drawer: loads the installed-app list off
 * the main thread from the platform [LauncherApps] provider, then renders the pure
 * [AppDrawer]. The provider is local (PackageManager on Android), so this screen
 * works with the gateway down — it is the offline-first launcher shell, not a
 * gateway-backed view.
 */
@Composable
fun AppDrawerScreen(
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val appSettings = koinInject<AppSettings>()
    val launcher = remember { createLauncherApps() }
    var apps by remember { mutableStateOf<List<LauncherAppEntry>>(emptyList()) }
    var loaded by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) {
        apps = withContext(getBackgroundDispatcher()) { launcher.installed() }
        loaded = true
    }
    val pinned by appSettings.pinnedAppsFlow.collectAsStateWithLifecycle()
    val pinnedSet = remember(pinned) { pinned.toSet() }
    DenebScreenScaffold(title = "앱", onBack = onBack, tabBar = navigationTabBar) {
        // Pull DOWN at the top of the list → exit back to 자체앱 (reverse of the
        // swipe-UP that opened the drawer). Same target as the back arrow.
        // Long-press a row → pin/unpin it to the 자체앱 favorites home.
        AppDrawer(
            apps = apps,
            onLaunch = { launcher.launch(it) },
            loaded = loaded,
            onExit = onBack,
            pinned = pinnedSet,
            onTogglePin = { appSettings.togglePinnedApp(it) },
        )
    }
}
