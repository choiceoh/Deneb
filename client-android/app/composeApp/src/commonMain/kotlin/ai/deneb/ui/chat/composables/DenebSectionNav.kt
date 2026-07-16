package ai.deneb.ui.chat.composables

import ai.deneb.DenebCalendar
import ai.deneb.DenebCategories
import ai.deneb.DenebConfig
import ai.deneb.DenebDashboard
import ai.deneb.DenebFeed
import ai.deneb.DenebFleet
import ai.deneb.DenebMail
import ai.deneb.DenebMore
import ai.deneb.DenebSearch
import ai.deneb.Home
import androidx.navigation.NavHostController
import kotlinx.coroutines.flow.MutableSharedFlow

/**
 * Top-level section nav shared across the app. The desktop product (a persistent rail)
 * moved to a separate workstation app (Andromeda), so the native client is mobile-only
 * — the phone bottom bar (DenebBottomBar) is the live navigation surface. These helpers
 * remain because the section order and the no-stack navigation are shared by the bottom
 * bar and the desktop verification harness (main.kt) alike.
 *
 * "people" is not a section: the merged people surface (recent contacts + 인물 wiki) is
 * reached through categories' pinned "사람" row.
 */
val denebSectionDestinations: List<Any> = listOf(
    DenebFeed(),
    DenebDashboard,
    Home,
    DenebMail,
    DenebCalendar,
    DenebSearch,
    DenebCategories,
    DenebFleet,
    DenebConfig,
)

/** True when [dest] is one of the five always-alive bottom-bar tabs (LiveTabPane) —
 *  those are selected, not navigated: they have no NavHost destination. */
fun isDenebLiveTab(dest: Any): Boolean = dest is DenebFeed || dest == Home || dest == DenebMail || dest == DenebCalendar || dest == DenebMore

/**
 * Live-tab select requests. The five tabs render outside the NavHost (App.kt's
 * LiveTabPane), so callers that only hold a NavHostController — the desktop harness
 * keyboard shortcuts, [navigateToDenebSection] — funnel tab switches through this
 * bus; AppContent collects it and flips the pane. Buffered so a tryEmit from a
 * non-suspend context (key handler) never drops.
 */
val denebLiveTabRequests = MutableSharedFlow<Any>(extraBufferCapacity = 8)

/**
 * Switch to a top-level section. The five live tabs are *selected* (via
 * [denebLiveTabRequests] — no back-stack entry, the pane keeps every tab alive);
 * every other section navigates without stacking: state of the current section is
 * saved, the target's is restored, and repeated switches don't grow the back stack.
 *
 * [restoreState] applies to navigated (non-tab) sections only.
 */
fun navigateToDenebSection(navController: NavHostController, dest: Any, restoreState: Boolean = true) {
    if (isDenebLiveTab(dest)) {
        denebLiveTabRequests.tryEmit(dest)
        return
    }
    navController.navigate(dest) {
        popUpTo(navController.graph.startDestinationId) { saveState = true }
        launchSingleTop = true
        this.restoreState = restoreState
    }
}
