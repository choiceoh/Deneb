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
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.handCursor
import androidx.compose.animation.animateColorAsState
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsHoveredAsState
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material3.Badge
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import androidx.compose.material.icons.automirrored.filled.Chat as ChatFilled
import androidx.compose.material.icons.automirrored.outlined.Chat as ChatOutlined
import androidx.compose.material.icons.filled.CalendarMonth as CalFilled
import androidx.compose.material.icons.filled.Dashboard as DashboardFilled
import androidx.compose.material.icons.filled.Dns as DnsFilled
import androidx.compose.material.icons.filled.Email as EmailFilled
import androidx.compose.material.icons.filled.GridView as GridFilled
import androidx.compose.material.icons.filled.Notifications as NotificationsFilled
import androidx.compose.material.icons.filled.Search as SearchFilled
import androidx.compose.material.icons.filled.Settings as SettingsFilled
import androidx.compose.material.icons.outlined.CalendarMonth as CalOutlined
import androidx.compose.material.icons.outlined.Dashboard as DashboardOutlined
import androidx.compose.material.icons.outlined.Dns as DnsOutlined
import androidx.compose.material.icons.outlined.Email as EmailOutlined
import androidx.compose.material.icons.outlined.GridView as GridOutlined
import androidx.compose.material.icons.outlined.Notifications as NotificationsOutlined
import androidx.compose.material.icons.outlined.Search as SearchOutlined
import androidx.compose.material.icons.outlined.Settings as SettingsOutlined

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

private data class SidebarItem(
    val label: String,
    val route: String,
    val dest: Any,
    val outlined: ImageVector,
    val filled: ImageVector,
    // 업무 데이터 section: hidden from the rail in the 챗봇 workspace (see DenebBottomBar).
    val workData: Boolean = false,
)

// [route] is the destination @SerialName (matches currentBackStackEntry.destination.route
// for highlighting); [dest] is the typed route object passed to navController.navigate —
// nav-compose 2.9 routes by the @Serializable object, not a bare route string.
// "people" is not a rail item: the merged people surface (recent contacts +
// 인물 wiki) is reached through categories' pinned "사람" row.
private val sidebarItems = listOf(
    SidebarItem("feed", "deneb_feed", DenebFeed, Icons.Outlined.NotificationsOutlined, Icons.Filled.NotificationsFilled, workData = true),
    SidebarItem("dashboard", "deneb_dashboard", DenebDashboard, Icons.Outlined.DashboardOutlined, Icons.Filled.DashboardFilled, workData = true),
    SidebarItem("chat", "home", Home, Icons.AutoMirrored.Outlined.ChatOutlined, Icons.AutoMirrored.Filled.ChatFilled),
    SidebarItem("mail", "deneb_mail", DenebMail, Icons.Outlined.EmailOutlined, Icons.Filled.EmailFilled, workData = true),
    SidebarItem("calendar", "deneb_calendar", DenebCalendar, Icons.Outlined.CalOutlined, Icons.Filled.CalFilled, workData = true),
    SidebarItem("search", "deneb_search", DenebSearch, Icons.Outlined.SearchOutlined, Icons.Filled.SearchFilled, workData = true),
    SidebarItem("categories", "deneb_categories", DenebCategories, Icons.Outlined.GridOutlined, Icons.Filled.GridFilled, workData = true),
    SidebarItem("fleet", "deneb_fleet", DenebFleet, Icons.Outlined.DnsOutlined, Icons.Filled.DnsFilled, workData = true),
    SidebarItem("settings", "deneb_config", DenebConfig, Icons.Outlined.SettingsOutlined, Icons.Filled.SettingsFilled),
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
    // 챗봇 workspace: hide 업무 데이터 rows (mail/calendar/search/categories/fleet).
    chatMode: Boolean = false,
    // Unread work-feed count badged on the 피드 row (the bell moved here).
    feedUnread: Int = 0,
) {
    SidebarContent(currentRoute = currentRoute, chatMode = chatMode, feedUnread = feedUnread, modifier = modifier) { dest ->
        navigateToDenebSection(navController, dest)
    }
}

@Composable
private fun SidebarContent(
    currentRoute: String?,
    chatMode: Boolean = false,
    feedUnread: Int = 0,
    modifier: Modifier = Modifier,
    onNavigate: (Any) -> Unit,
) {
    val items = if (chatMode) sidebarItems.filterNot { it.workData } else sidebarItems
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
            items.forEach { item ->
                SidebarRow(
                    label = item.label,
                    outlined = item.outlined,
                    filled = item.filled,
                    selected = currentRoute == item.route,
                    badgeCount = if (item.route == "deneb_feed") feedUnread else 0,
                    onClick = { onNavigate(item.dest) },
                )
            }
        }
    }
}

@Composable
private fun SidebarRow(
    label: String,
    outlined: ImageVector,
    filled: ImageVector,
    selected: Boolean,
    badgeCount: Int = 0,
    onClick: () -> Unit,
) {
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
    Row(
        modifier = Modifier
            // Full row width: the whole 200dp band is the click target, not just the glyphs.
            .fillMaxWidth()
            .clickable(interactionSource = interaction, indication = null) {
                haptics.tap()
                onClick()
            }
            .handCursor()
            .padding(vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        // Same icon language as the phone bottom bar: outlined when idle, filled when
        // the section is active (Apple SF). Tint follows the row's mono color state.
        Icon(
            imageVector = if (selected) filled else outlined,
            contentDescription = null,
            tint = color,
            modifier = Modifier.size(20.dp),
        )
        Spacer(Modifier.width(14.dp))
        Text(text = label, style = DenebType.railItem, color = color)
        if (badgeCount > 0) {
            Spacer(Modifier.weight(1f))
            Badge { Text(if (badgeCount > 9) "9+" else badgeCount.toString()) }
        }
    }
}
