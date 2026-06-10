package ai.deneb.ui.chat.composables

import androidx.compose.animation.animateColorAsState
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsHoveredAsState
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import ai.deneb.DenebCalendar
import ai.deneb.DenebCategories
import ai.deneb.DenebConfig
import ai.deneb.DenebMail
import ai.deneb.DenebPeople
import ai.deneb.DenebSearch
import ai.deneb.Home
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.handCursor

/**
 * Desktop's always-visible left navigation rail — the persistent counterpart to the
 * mobile [DenebDrawerSheet]. Same typographic, icon-free idiom (ultralight lowercase
 * words), but fixed at 240dp, highlighting the current section instead of an overlay
 * that opens/closes. Used by App.kt only when [ai.deneb.currentPlatform]
 * is a Desktop platform; mobile keeps the modal drawer.
 *
 * The width is a hard 240dp (not constraint-derived) on purpose: the headless desktop
 * harness reports a bogus maxWidth, so any BoxWithConstraints/widthIn cap misfires —
 * a fixed width is the only reliable size here (see DenebDesign.denebContentWidthModifier).
 */

private data class SidebarItem(val label: String, val route: String, val dest: Any)

// [route] is the destination @SerialName (matches currentBackStackEntry.destination.route
// for highlighting); [dest] is the typed route object passed to navController.navigate —
// nav-compose 2.9 routes by the @Serializable object, not a bare route string.
private val sidebarItems = listOf(
    SidebarItem("chat", "home", Home),
    SidebarItem("mail", "deneb_mail", DenebMail),
    SidebarItem("calendar", "deneb_calendar", DenebCalendar),
    SidebarItem("search", "deneb_search", DenebSearch),
    SidebarItem("people", "deneb_people", DenebPeople),
    SidebarItem("categories", "deneb_categories", DenebCategories),
    SidebarItem("settings", "deneb_config", DenebConfig),
)

/**
 * Ordered section destinations, top to bottom as the sidebar shows them. The desktop
 * entry point maps Ctrl/Cmd+1..N onto this list so keyboard switching and the rail
 * can never disagree about what "section 3" is.
 */
val denebSectionDestinations: List<Any> = sidebarItems.map { it.dest }

/**
 * Switch to a top-level section without stacking destinations: state of the current
 * section is saved, the target's is restored, and repeated switches don't grow the
 * back stack. Shared by the sidebar rows and the desktop keyboard shortcuts.
 */
fun navigateToDenebSection(navController: NavHostController, dest: Any) {
    navController.navigate(dest) {
        popUpTo(navController.graph.startDestinationId) { saveState = true }
        launchSingleTop = true
        restoreState = true
    }
}

@Composable
fun DenebSidebar(
    navController: NavHostController,
    currentRoute: String?,
    modifier: Modifier = Modifier,
) {
    SidebarContent(currentRoute = currentRoute, modifier = modifier) { dest ->
        navigateToDenebSection(navController, dest)
    }
}

@Composable
private fun SidebarContent(
    currentRoute: String?,
    modifier: Modifier = Modifier,
    onNavigate: (Any) -> Unit,
) {
    val hairline = denebHairline()
    Surface(color = MaterialTheme.colorScheme.background, modifier = modifier) {
        Column(
            Modifier
                .width(240.dp)
                .fillMaxHeight()
                .drawBehind {
                    val x = size.width - 0.5.dp.toPx()
                    drawLine(hairline, Offset(x, 0f), Offset(x, size.height), strokeWidth = 1.dp.toPx())
                }
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 20.dp, vertical = 28.dp),
        ) {
            sidebarItems.forEach { item ->
                SidebarRow(
                    label = item.label,
                    selected = currentRoute == item.route,
                    onClick = { onNavigate(item.dest) },
                )
            }
        }
    }
}

@Composable
private fun SidebarRow(label: String, selected: Boolean, onClick: () -> Unit) {
    val haptics = rememberHaptics()
    // Hover feedback by color only — `indication = null` because the default ripple
    // also draws a focus overlay, and on desktop a mouse click focuses the row,
    // leaving a gray box stuck on the label until focus moves elsewhere.
    val interaction = remember { MutableInteractionSource() }
    val hovered by interaction.collectIsHoveredAsState()
    val color by animateColorAsState(
        when {
            selected -> MaterialTheme.colorScheme.onBackground
            hovered -> MaterialTheme.colorScheme.onBackground.copy(alpha = 0.8f)
            else -> denebHint()
        },
    )
    Text(
        text = label,
        fontSize = 20.sp,
        lineHeight = 34.sp,
        fontWeight = FontWeight.ExtraLight,
        color = color,
        modifier = Modifier
            // Full row width: the whole 200dp band is the click target, not just the glyphs.
            .fillMaxWidth()
            .clickable(interactionSource = interaction, indication = null) { haptics.tap(); onClick() }
            .handCursor()
            .padding(vertical = 8.dp),
    )
}
