package ai.deneb.ui.chat.composables

import ai.deneb.DenebCalendar
import ai.deneb.DenebFeed
import ai.deneb.DenebMail
import ai.deneb.DenebMore
import ai.deneb.Home
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.material.icons.Icons
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
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
import androidx.compose.material.icons.filled.CalendarMonth as CalendarMonthFilled
import androidx.compose.material.icons.filled.Email as EmailFilled
import androidx.compose.material.icons.filled.MoreHoriz as MoreFilled
import androidx.compose.material.icons.filled.Notifications as NotificationsFilled
import androidx.compose.material.icons.outlined.CalendarMonth as CalendarMonthOutlined
import androidx.compose.material.icons.outlined.Email as EmailOutlined
import androidx.compose.material.icons.outlined.MoreHoriz as MoreOutlined
import androidx.compose.material.icons.outlined.Notifications as NotificationsOutlined

/**
 * The phone's bottom tab bar — the assistant app's primary navigation.
 *
 * Five slots: 피드 · 메일 · 채팅 · 달력 · 더보기, with 채팅 the emphasized center (the core
 * assistant conversation). All are *screen tabs* that select + highlight when current
 * (outlined idle → filled active) so the bar always shows "you are here" — including on
 * 메일 and 달력, which jump into their section and keep the bar (the section is a bottom-bar
 * route, so you can tab-switch without backing out). 더보기 opens the section hub
 * ([DenebMore]) — a grouped text list of the remaining sections (파트별 업무 현황·조직도·
 * 검색·할일·일기·카테고리·전체 연락처·노트북·파일·브라우저·설정).
 *
 * Doctrine (native-design-system.md): the *structure* is Material/Apple-practical — an
 * M3 [NavigationBar] substrate (system insets, ripple, Role.Tab a11y, haptics) with the
 * Apple SF pattern of an outlined icon when idle and a filled icon when active, label
 * always shown. The *skin* is Deneb-calm — monochrome (no colored tabs), a faint ink
 * indicator instead of the bright M3 secondaryContainer pill, a hairline top edge, and
 * small [DenebType] labels.
 *
 * Stateless: the host (App.kt) owns navigation and passes [currentRoute] for
 * highlighting. The native client is mobile-only, so this bar is the app's primary
 * navigation surface.
 */
/** A bottom-bar tab: navigates to [dest] and highlights (outlined → filled) when [route]
 *  is current. Every slot — including 메일·달력 — is one of these, so the bar always shows
 *  the active section. */
data class DenebTabItem(
    val label: String,
    val route: String,
    val dest: Any,
    val outlined: ImageVector,
    val filled: ImageVector,
)

// Screen-tab routes (used by App.kt to decide when to show the bar and which is active).
const val ROUTE_FEED = "deneb_feed"
const val ROUTE_HOME = "home"
const val ROUTE_MORE = "deneb_more"

// The five tabs. 피드 = work briefing home; 채팅 = the assistant conversation (center);
// 더보기 = the section hub list; 메일·달력 jump into their section (which keeps the bar).
// All highlight when current — see [denebBottomTabs].
private val feedTab = DenebTabItem(
    "피드",
    ROUTE_FEED,
    DenebFeed,
    Icons.Outlined.NotificationsOutlined,
    Icons.Filled.NotificationsFilled,
)
private val mailTab = DenebTabItem(
    "메일",
    "deneb_mail",
    DenebMail,
    Icons.Outlined.EmailOutlined,
    Icons.Filled.EmailFilled,
)
private val chatTab = DenebTabItem(
    "채팅",
    ROUTE_HOME,
    Home,
    Icons.AutoMirrored.Outlined.ChatOutlined,
    Icons.AutoMirrored.Filled.ChatFilled,
)
private val calendarTab = DenebTabItem(
    "달력",
    "deneb_calendar",
    DenebCalendar,
    Icons.Outlined.CalendarMonthOutlined,
    Icons.Filled.CalendarMonthFilled,
)
private val moreTab = DenebTabItem(
    "더보기",
    ROUTE_MORE,
    DenebMore,
    Icons.Outlined.MoreOutlined,
    Icons.Filled.MoreFilled,
)

// The five-slot tab list — 피드 · 메일 · 채팅 · 달력 · 더보기 (채팅 center). All select +
// highlight when current; 메일/달력 jump into their section, which keeps the bar.
val denebBottomTabs: List<DenebTabItem> = listOf(feedTab, mailTab, chatTab, calendarTab, moreTab)

// Routes that surface 업무 데이터 — used by App.kt to bounce a 챗봇-mode session back to
// home if it ever lands on one (defensive — the 챗봇 workspace has no bottom bar). 피드 is
// 업무-only (the work feed home), so it bounces; the 더보기 list filters its own 업무 entries.
val denebWorkDataRoutes: Set<String> = setOf(
    "deneb_feed",
    "deneb_mail",
    "deneb_calendar",
    "deneb_search",
    "deneb_categories",
    "deneb_fleet",
    "deneb_dashboard",
    "deneb_org",
)

// Routes that show the bottom bar. The bar stays on 채팅(home) and on every 더보기 SECTION
// — 메일·달력·검색·할일·일기·카테고리·조직도·현황·연락처·노트북·파일·브라우저·설정 — so you can
// tab-switch without backing out first. Excluded on purpose: deep DETAILS reached *from* a
// section (a specific mail/event/wiki page/person, settings sub-screens like fleet/skill/
// cron) — those are data-class/arg routes that drill down and keep their own ← back nav.
// The 챗봇 workspace hides the bar separately (navChatMode in App.kt).
val denebBottomBarRoutes: Set<String> = setOf(
    ROUTE_FEED,
    ROUTE_HOME,
    ROUTE_MORE,
    "deneb_mail",
    "deneb_calendar",
    "deneb_search",
    "deneb_diary",
    "deneb_categories",
    "deneb_contacts",
    "deneb_org",
    "deneb_dashboard",
    "deneb_notebooks",
    "deneb_files",
    "deneb_browser",
    "deneb_config",
)

// Content height of the bar (excludes the system navigation-bar inset, which is added
// on top). Trimmed well below M3's default 80dp — the default read as a touch tall —
// while still fitting the icon + small label without clipping. This sits just above the
// icon + indicator + label intrinsic stack, so don't trim further without re-checking
// the live render for clipping.
private val DenebBottomBarHeight = 52.dp

@Composable
fun DenebBottomBar(
    currentRoute: String?,
    onNavigate: (Any) -> Unit,
    modifier: Modifier = Modifier,
    // Unread work-feed count badged on the 피드 tab (the old top-bar bell moved here).
    feedUnread: Int = 0,
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
    // Total bar height = content + system nav-bar inset. The NavigationBar still applies
    // its own windowInsets (pushing the items above the gesture bar), so the items get
    // exactly DenebBottomBarHeight regardless of device inset — no clipping.
    val bottomInset = WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding()
    NavigationBar(
        containerColor = MaterialTheme.colorScheme.background,
        tonalElevation = 0.dp,
        modifier = modifier
            .height(DenebBottomBarHeight + bottomInset)
            .drawBehind {
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
                    val glyph = @Composable {
                        Icon(
                            imageVector = if (selected) tab.filled else tab.outlined,
                            contentDescription = tab.label,
                        )
                    }
                    if (tab.route == ROUTE_FEED && feedUnread > 0) {
                        BadgedBox(
                            badge = { Badge { Text(if (feedUnread > 9) "9+" else feedUnread.toString()) } },
                        ) { glyph() }
                    } else {
                        glyph()
                    }
                },
                label = { Text(tab.label, style = DenebType.meta) },
                alwaysShowLabel = true,
                colors = colors,
            )
        }
    }
}
