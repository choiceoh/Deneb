package ai.deneb.ui.chat.composables

import ai.deneb.DenebAppHub
import ai.deneb.DenebFeed
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
import androidx.compose.material.icons.automirrored.outlined.Chat as ChatBubbleOutlined
import androidx.compose.material.icons.filled.Notifications as NotificationsFilled
import androidx.compose.material.icons.filled.Widgets as WidgetsFilled
import androidx.compose.material.icons.outlined.Call as CallOutlined
import androidx.compose.material.icons.outlined.Language as LanguageOutlined
import androidx.compose.material.icons.outlined.Notifications as NotificationsOutlined
import androidx.compose.material.icons.outlined.Widgets as WidgetsOutlined

/**
 * The phone's bottom tab bar — the super-app's launcher navigation (토스식 슈퍼앱).
 *
 * Final shape (user's call): five slots 피드 · 통화 · 자체앱 · 인터넷 · 카톡, with 자체앱
 * as the emphasized center (the launcher for Deneb's own mini-apps). Three of the five
 * are *action tabs*, not screens, and never carry a selection indicator:
 *   - **통화** fires the phone dialer (`tel:`).
 *   - **인터넷** launches Samsung Internet (the external browser, Android package intent).
 *   - **카톡** launches the KakaoTalk app (Android package intent).
 * Only the two screen tabs (피드 · 자체앱) can be selected. 인터넷 is now an *external*
 * browser, not the in-app DenebBrowser — that in-app translation browser moved into the
 * 자체앱 grid as the "브라우저" tile. 채팅 · 메일 · 달력 · 더보기 all live in the 자체앱 grid too.
 *
 * Doctrine (project_superapp_vision, native-design-system.md): the *structure* is
 * Material/Apple-practical — an M3 [NavigationBar] substrate (system insets, ripple,
 * Role.Tab a11y, haptics) with the Apple SF pattern of an outlined icon when idle and
 * a filled icon when active, label always shown. The *skin* is Deneb-calm — monochrome
 * (no colored tabs), a faint ink indicator instead of the bright M3 secondaryContainer
 * pill, a hairline top edge, and small [DenebType] labels.
 *
 * Stateless: the host (App.kt) owns navigation and passes [currentRoute] for
 * highlighting, plus the [onCall]/[onInternet]/[onKakao] action callbacks. The native
 * client is mobile-only, so this bar is the app's primary navigation surface.
 */
sealed interface DenebTabItem {
    val label: String

    /** A screen tab — navigating to [dest] and highlightable when [route] is current. */
    data class Screen(
        override val label: String,
        val route: String,
        val dest: Any,
        val outlined: ImageVector,
        val filled: ImageVector,
    ) : DenebTabItem

    /** An action tab — fires a side effect (dialer / external app) and never selects. */
    data class Action(
        override val label: String,
        val icon: ImageVector,
        val onClick: () -> Unit,
    ) : DenebTabItem
}

// Screen tab routes (used by App.kt to decide when to show the bar and which is active).
const val ROUTE_FEED = "deneb_feed"
const val ROUTE_APP_HUB = "deneb_app_hub"

// The screen tabs (action tabs 통화/인터넷/카톡 are spliced in by [denebBottomTabs] since
// they need host callbacks). 피드 leads (the work home), 자체앱 is the launcher center.
// 인터넷 is no longer a screen tab — it became the external-browser action. 채팅·메일·
// 달력·검색·할일·일기·…·브라우저·설정 all live in the 자체앱 grid.
val denebScreenTabs: List<DenebTabItem.Screen> = listOf(
    DenebTabItem.Screen("피드", ROUTE_FEED, DenebFeed, Icons.Outlined.NotificationsOutlined, Icons.Filled.NotificationsFilled),
    DenebTabItem.Screen("자체앱", ROUTE_APP_HUB, DenebAppHub, Icons.Outlined.WidgetsOutlined, Icons.Filled.WidgetsFilled),
)

/**
 * Build the full five-slot tab list in display order — 피드 · 통화 · 자체앱 · 인터넷 · 카톡
 * — splicing the three action tabs around the two screen tabs. The host supplies the
 * action callbacks ([onCall] = dialer, [onInternet] = Samsung Internet, [onKakao] =
 * KakaoTalk) since they fire platform side effects.
 */
fun denebBottomTabs(onCall: () -> Unit, onInternet: () -> Unit, onKakao: () -> Unit): List<DenebTabItem> = listOf(
    denebScreenTabs[0], // 피드
    DenebTabItem.Action("통화", Icons.Outlined.CallOutlined, onCall),
    denebScreenTabs[1], // 자체앱 (center)
    // 인터넷 launches Samsung Internet (external browser) — keeps the globe glyph.
    DenebTabItem.Action("인터넷", Icons.Outlined.LanguageOutlined, onInternet),
    DenebTabItem.Action("카톡", Icons.AutoMirrored.Outlined.ChatBubbleOutlined, onKakao),
)

// Routes that surface 업무 데이터 — used by App.kt to bounce a 챗봇-mode session back to
// home if it ever lands on one (defensive — the 챗봇 workspace has no bottom bar). 피드 is
// 업무-only (the work feed home), so it bounces; the 자체앱 grid filters its own 업무 tiles.
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

// Top-level routes that show the bottom bar: the 2 screen tabs only. Pushed detail
// screens (data-class routes with args, and every section opened from the 자체앱 grid —
// including the 브라우저 tile) are absent, so they hide the bar and keep their own back nav.
val denebBottomBarRoutes: Set<String> = setOf(
    ROUTE_FEED,
    ROUTE_APP_HUB,
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
    onCall: () -> Unit,
    onInternet: () -> Unit,
    onKakao: () -> Unit,
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
        denebBottomTabs(onCall = onCall, onInternet = onInternet, onKakao = onKakao).forEach { tab ->
            when (tab) {
                is DenebTabItem.Screen -> {
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

                is DenebTabItem.Action -> {
                    // Action tabs (통화·인터넷·카톡) fire a side effect and are never selected — the
                    // outlined glyph stays at the unselected (hint) weight regardless of route.
                    NavigationBarItem(
                        selected = false,
                        onClick = {
                            haptics.tap()
                            tab.onClick()
                        },
                        icon = { Icon(tab.icon, contentDescription = tab.label) },
                        label = { Text(tab.label, style = DenebType.meta) },
                        alwaysShowLabel = true,
                        colors = colors,
                    )
                }
            }
        }
    }
}
