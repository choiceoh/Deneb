package ai.deneb.ui.chat.composables

import ai.deneb.DenebCalendar
import ai.deneb.DenebConfig
import ai.deneb.DenebMail
import ai.deneb.Home
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import androidx.compose.material.icons.Icons
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp
import androidx.compose.material.icons.automirrored.filled.Chat as ChatFilled
import androidx.compose.material.icons.automirrored.outlined.Chat as ChatOutlined
import androidx.compose.material.icons.filled.CalendarMonth as CalFilled
import androidx.compose.material.icons.filled.Email as EmailFilled
import androidx.compose.material.icons.filled.Settings as SettingsFilled
import androidx.compose.material.icons.outlined.CalendarMonth as CalOutlined
import androidx.compose.material.icons.outlined.Email as EmailOutlined
import androidx.compose.material.icons.outlined.MoreHoriz as MoreOutlined
import androidx.compose.material.icons.outlined.Settings as SettingsOutlined

/**
 * The phone's bottom tab bar — the super-app's primary navigation (토스식).
 *
 * Doctrine (project_superapp_vision, native-design-system.md): the *structure*
 * is Material/Apple-practical — an M3 [NavigationBar] substrate (system insets,
 * ripple, Role.Tab a11y, haptics) with the Apple SF pattern of an outlined icon
 * when idle and a filled icon when active, label always shown. The *skin* is
 * Deneb-calm — monochrome (no colored tabs), a faint ink indicator instead of the
 * bright M3 secondaryContainer pill, a hairline top edge, and small [DenebType]
 * labels. Chat is tab 1 (the killer hub / home); mail·calendar·categories surface
 * the all-in-one sections so they are visible and thumb-reachable instead of
 * hidden behind a drawer. 더보기 navigates to a full DenebMore screen (the host
 * wires [onMore]) for the secondary sections.
 *
 * Stateless: the host (App.kt) owns navigation and passes [currentRoute] for
 * highlighting, so this stays previewable. Desktop keeps its persistent
 * [DenebSidebar]; this bar is mounted on phones only.
 */
data class DenebTab(
    val label: String,
    val route: String,
    val dest: Any,
    val outlined: ImageVector,
    val filled: ImageVector,
)

// The four primary sections. Search·todo·diary·categories live under 더보기 (and fleet
// lives inside settings, so it is reached through the 설정 tab — not 더보기). The whole
// bar is hidden in the 챗봇 workspace (App.kt), so there is no per-tab filtering here.
val denebBottomTabs: List<DenebTab> = listOf(
    DenebTab("채팅", "home", Home, Icons.AutoMirrored.Outlined.ChatOutlined, Icons.AutoMirrored.Filled.ChatFilled),
    DenebTab("메일", "deneb_mail", DenebMail, Icons.Outlined.EmailOutlined, Icons.Filled.EmailFilled),
    DenebTab("달력", "deneb_calendar", DenebCalendar, Icons.Outlined.CalOutlined, Icons.Filled.CalFilled),
    DenebTab("설정", "deneb_config", DenebConfig, Icons.Outlined.SettingsOutlined, Icons.Filled.SettingsFilled),
)

// Routes that surface 업무 데이터 (mail/calendar/search/categories/fleet). Used by App.kt
// to bounce a 챗봇-mode session back to home if it ever lands on one (defensive — the
// 챗봇 workspace has no bottom bar and the desktop rail hides these rows) and by the
// desktop rail's row filter.
val denebWorkDataRoutes: Set<String> = setOf(
    "deneb_mail",
    "deneb_calendar",
    "deneb_search",
    "deneb_categories",
    "deneb_fleet",
)

// Top-level routes that show the bottom bar: the 4 tabs + the 더보기 screen and its
// destinations. Pushed detail screens (data-class routes with args) are absent, so
// they hide the bar and keep their own back nav. Fleet is now a settings sub-screen
// (like skill/cron) — a pushed detail with its own back nav, so it is not listed here.
val denebBottomBarRoutes: Set<String> = setOf(
    "home", "deneb_mail", "deneb_calendar", "deneb_config",
    "deneb_more", "deneb_search", "deneb_todo", "deneb_diary", "deneb_categories",
)

// Routes where 더보기 is the active tab (the More screen itself, or a section opened
// from it).
val denebMoreRoutes: Set<String> = setOf(
    "deneb_more",
    "deneb_search",
    "deneb_todo",
    "deneb_diary",
    "deneb_categories",
)

@Composable
fun DenebBottomBar(
    currentRoute: String?,
    moreActive: Boolean,
    onNavigate: (Any) -> Unit,
    onMore: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val haptics = rememberHaptics()
    val hairline = denebHairline()
    val ink = MaterialTheme.colorScheme.onBackground
    val hint = denebHint()
    // Monochrome restraint: the selected item is ink (not a brand color), and the
    // indicator is a faint ink wash rather than M3's filled secondaryContainer pill.
    val colors = NavigationBarItemDefaults.colors(
        selectedIconColor = ink,
        selectedTextColor = ink,
        unselectedIconColor = hint,
        unselectedTextColor = hint,
        indicatorColor = ink.copy(alpha = 0.10f),
    )
    NavigationBar(
        containerColor = MaterialTheme.colorScheme.background,
        tonalElevation = 0.dp,
        modifier = modifier.drawBehind {
            // Deneb hairline top edge instead of an M3 elevation shadow.
            drawLine(hairline, Offset(0f, 0f), Offset(size.width, 0f), strokeWidth = 1.dp.toPx())
        },
    ) {
        denebBottomTabs.forEach { tab ->
            val selected = currentRoute == tab.route
            NavigationBarItem(
                selected = selected,
                onClick = {
                    haptics.tap()
                    onNavigate(tab.dest)
                },
                icon = {
                    Icon(
                        imageVector = if (selected) tab.filled else tab.outlined,
                        contentDescription = tab.label,
                    )
                },
                label = { Text(tab.label, style = DenebType.meta) },
                alwaysShowLabel = true,
                colors = colors,
            )
        }
        NavigationBarItem(
            selected = moreActive,
            onClick = {
                haptics.tap()
                onMore()
            },
            icon = { Icon(Icons.Outlined.MoreOutlined, contentDescription = "더보기") },
            label = { Text("더보기", style = DenebType.meta) },
            alwaysShowLabel = true,
            colors = colors,
        )
    }
}
