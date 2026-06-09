package com.inspiredandroid.kai.ui.chat.composables

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import com.inspiredandroid.kai.DenebCalendar
import com.inspiredandroid.kai.DenebCategories
import com.inspiredandroid.kai.DenebConfig
import com.inspiredandroid.kai.DenebMail
import com.inspiredandroid.kai.DenebPeople
import com.inspiredandroid.kai.DenebSearch
import com.inspiredandroid.kai.Home
import com.inspiredandroid.kai.ui.components.rememberHaptics
import com.inspiredandroid.kai.ui.denebHairline
import com.inspiredandroid.kai.ui.denebHint
import com.inspiredandroid.kai.ui.handCursor

/**
 * Desktop's always-visible left navigation rail — the persistent counterpart to the
 * mobile [DenebDrawerSheet]. Same typographic, icon-free idiom (ultralight lowercase
 * words), but fixed at 240dp, highlighting the current section instead of an overlay
 * that opens/closes. Used by App.kt only when [com.inspiredandroid.kai.currentPlatform]
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

@Composable
fun DenebSidebar(
    navController: NavHostController,
    currentRoute: String?,
    modifier: Modifier = Modifier,
) {
    SidebarContent(currentRoute = currentRoute, modifier = modifier) { dest ->
        // popUpTo(start) + restoreState keeps section switches from stacking up and
        // preserves each section's back stack (chat included, so no duplicate Home).
        navController.navigate(dest) {
            popUpTo(navController.graph.startDestinationId) { saveState = true }
            launchSingleTop = true
            restoreState = true
        }
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
    Text(
        text = label,
        fontSize = 20.sp,
        lineHeight = 34.sp,
        fontWeight = FontWeight.ExtraLight,
        color = if (selected) MaterialTheme.colorScheme.onBackground else denebHint(),
        modifier = Modifier
            .clickable { haptics.tap(); onClick() }
            .handCursor()
            .padding(vertical = 8.dp),
    )
}
