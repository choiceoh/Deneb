package ai.deneb.ui.chat.composables

import ai.deneb.DenebCalendar
import ai.deneb.DenebCategories
import ai.deneb.DenebConfig
import ai.deneb.DenebDashboard
import ai.deneb.DenebFeed
import ai.deneb.DenebFleet
import ai.deneb.DenebMail
import ai.deneb.DenebSearch
import ai.deneb.Home
import androidx.navigation.NavHostController

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
    DenebFeed,
    DenebDashboard,
    Home,
    DenebMail,
    DenebCalendar,
    DenebSearch,
    DenebCategories,
    DenebFleet,
    DenebConfig,
)

/**
 * Switch to a top-level section without stacking destinations: state of the current
 * section is saved, the target's is restored, and repeated switches don't grow the back
 * stack. Shared by the bottom bar and the desktop harness keyboard shortcuts (Cmd/Ctrl+N).
 *
 * [restoreState] is true by default (each tab remembers where you were). Pass false for a
 * tab that should always land on its ROOT — the 더보기 hub does this so that pressing 더보기
 * from a section drilled into it (검색·할일·…) returns to the hub instead of dumping you
 * back on the last section.
 */
fun navigateToDenebSection(navController: NavHostController, dest: Any, restoreState: Boolean = true) {
    navController.navigate(dest) {
        popUpTo(navController.graph.startDestinationId) { saveState = true }
        launchSingleTop = true
        this.restoreState = restoreState
    }
}
