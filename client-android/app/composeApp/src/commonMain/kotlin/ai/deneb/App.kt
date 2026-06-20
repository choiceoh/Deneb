@file:OptIn(ExperimentalMaterial3Api::class)

package ai.deneb

import ai.deneb.data.AppSettings
import ai.deneb.data.DataRepository
import ai.deneb.data.ThemeMode
import ai.deneb.deneb.DenebAppHubScreen
import ai.deneb.deneb.DenebBrowserScreen
import ai.deneb.deneb.DenebCalendarAddScreen
import ai.deneb.deneb.DenebCalendarEventScreen
import ai.deneb.deneb.DenebCalendarScreen
import ai.deneb.deneb.DenebCategoriesScreen
import ai.deneb.deneb.DenebCategoryPagesScreen
import ai.deneb.deneb.DenebConfigScreen
import ai.deneb.deneb.DenebCronEditScreen
import ai.deneb.deneb.DenebCronScreen
import ai.deneb.deneb.DenebDashboardScreen
import ai.deneb.deneb.DenebDiaryScreen
import ai.deneb.deneb.DenebFilesScreen
import ai.deneb.deneb.DenebFleetScreen
import ai.deneb.deneb.DenebGatewayClient
import ai.deneb.deneb.DenebMailDetailScreen
import ai.deneb.deneb.DenebMailScreen
import ai.deneb.deneb.DenebMoreScreen
import ai.deneb.deneb.DenebNotebooksScreen
import ai.deneb.deneb.DenebOrgChartScreen
import ai.deneb.deneb.DenebPeopleScreen
import ai.deneb.deneb.DenebPersonScreen
import ai.deneb.deneb.DenebSearchScreen
import ai.deneb.deneb.DenebSkillScreen
import ai.deneb.deneb.DenebTodoAddScreen
import ai.deneb.deneb.DenebTodoScreen
import ai.deneb.deneb.DenebWikiPageScreen
import ai.deneb.tools.CalendarPermissionController
import ai.deneb.tools.ContactsPermissionController
import ai.deneb.tools.NotificationPermissionController
import ai.deneb.tools.SetupCalendarPermissionHandler
import ai.deneb.tools.SetupContactsPermissionHandler
import ai.deneb.tools.SetupNotificationPermissionHandler
import ai.deneb.tools.SetupSmsPermissionHandler
import ai.deneb.tools.SetupSmsSendPermissionHandler
import ai.deneb.tools.SmsPermissionController
import ai.deneb.tools.SmsSendPermissionController
import ai.deneb.ui.DarkColorScheme
import ai.deneb.ui.LightColorScheme
import ai.deneb.ui.Theme
import ai.deneb.ui.chat.ChatScreen
import ai.deneb.ui.chat.ChatViewModel
import ai.deneb.ui.chat.composables.CaptureActions
import ai.deneb.ui.chat.composables.DenebBottomBar
import ai.deneb.ui.chat.composables.FeedScreen
import ai.deneb.ui.chat.composables.LocalCaptureActions
import ai.deneb.ui.chat.composables.denebBottomBarRoutes
import ai.deneb.ui.chat.composables.denebMoreRoutes
import ai.deneb.ui.chat.composables.denebWorkDataRoutes
import ai.deneb.ui.chat.composables.navigateToDenebSection
import ai.deneb.ui.components.FullScreenImageHost
import ai.deneb.ui.handCursor
import ai.deneb.ui.launcher.AppDrawerScreen
import ai.deneb.ui.withBlackBackground
import androidx.compose.foundation.background
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.ime
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.platform.UriHandler
import androidx.compose.ui.text.intl.Locale
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.toRoute
import coil3.ImageLoader
import coil3.PlatformContext
import coil3.compose.setSingletonImageLoaderFactory
import coil3.network.ktor3.KtorNetworkFetcherFactory
import coil3.svg.SvgDecoder
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.tab_chat
import deneb.composeapp.generated.resources.tab_settings
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import nl.marc_apps.tts.TextToSpeechInstance
import nl.marc_apps.tts.experimental.ExperimentalVoiceApi
import org.jetbrains.compose.resources.stringResource
import org.koin.compose.KoinApplication
import org.koin.compose.koinInject
import org.koin.compose.viewmodel.koinViewModel
import org.koin.dsl.koinConfiguration

@Serializable
@SerialName("home")
object Home

@Serializable
@SerialName("deneb_feed")
object DenebFeed

@Serializable
@SerialName("deneb_config")
object DenebConfig

@Serializable
@SerialName("deneb_fleet")
object DenebFleet

@Serializable
@SerialName("deneb_mail")
object DenebMail

@Serializable
@SerialName("deneb_calendar")
object DenebCalendar

@Serializable
@SerialName("deneb_mail_detail")
data class DenebMailDetail(val id: String)

@Serializable
@SerialName("deneb_calendar_event")
data class DenebCalendarEvent(val id: String)

@Serializable
@SerialName("deneb_calendar_add")
data class DenebCalendarAdd(val dateIso: String)

@Serializable
@SerialName("deneb_calendar_edit")
data class DenebCalendarEdit(val id: String)

@Serializable
@SerialName("deneb_todo")
object DenebTodo

@Serializable
@SerialName("deneb_todo_add")
object DenebTodoAdd

@Serializable
@SerialName("deneb_todo_edit")
data class DenebTodoEdit(val id: String)

@Serializable
@SerialName("deneb_search")
object DenebSearch

@Serializable
@SerialName("deneb_more")
object DenebMore

// Work-launcher app drawer (Phase 0). A local, gateway-independent screen listing
// installed apps; reached from 더보기 for now, later the home swipe-up.
@Serializable
@SerialName("deneb_apps")
object DenebApps

// 자체앱 — Deneb's own mini-apps as a home-screen grid (bottom tab 3): browser, chat,
// and calendar (moved off the bottom bar). Distinct from DenebApps (external installed apps).
@Serializable
@SerialName("deneb_app_hub")
object DenebAppHub

@Serializable
@SerialName("deneb_wiki")
data class DenebWiki(val path: String)

@Serializable
@SerialName("deneb_people")
object DenebPeople

@Serializable
@SerialName("deneb_person")
data class DenebPerson(val sender: String)

@Serializable
@SerialName("deneb_categories")
object DenebCategories

@Serializable
@SerialName("deneb_diary")
object DenebDiary

@Serializable
@SerialName("deneb_notebooks")
object DenebNotebooks

@Serializable
@SerialName("deneb_dashboard")
object DenebDashboard

@Serializable
@SerialName("deneb_org")
object DenebOrgChart

@Serializable
@SerialName("deneb_category_pages")
data class DenebCategoryPages(val category: String)

@Serializable
@SerialName("deneb_skill")
data class DenebSkill(val name: String)

@Serializable
@SerialName("deneb_browser")
data class DenebBrowser(val url: String)

@Serializable
@SerialName("deneb_cron")
data class DenebCron(val cronId: String)

@Serializable
@SerialName("deneb_cron_edit")
data class DenebCronEdit(val cronId: String)

@Serializable
@SerialName("deneb_files")
object DenebFiles

@Composable
fun App(
    navController: NavHostController,
    lightColorScheme: ColorScheme = LightColorScheme,
    darkColorScheme: ColorScheme = DarkColorScheme,
    textToSpeech: TextToSpeechInstance? = null,
    isKoinStarted: Boolean = false,
    onAppOpens: ((Int) -> Unit)? = null,
    captureActions: CaptureActions? = null,
) {
    setSingletonImageLoaderFactory { context: PlatformContext ->
        ImageLoader.Builder(context)
            .components {
                add(KtorNetworkFetcherFactory())
                add(SvgDecoder.Factory())
            }
            .build()
    }

    // Reuse global Koin if already started (Android Application class),
    // otherwise create a new instance (iOS, Desktop, Wasm).
    CompositionLocalProvider(LocalCaptureActions provides captureActions) {
        if (isKoinStarted) {
            AppContent(navController, lightColorScheme, darkColorScheme, textToSpeech, onAppOpens)
        } else {
            KoinApplication(
                configuration = koinConfiguration {
                    modules(appModule)
                },
            ) {
                AppContent(navController, lightColorScheme, darkColorScheme, textToSpeech, onAppOpens)
            }
        }
    }
}

@Composable
private fun AppContent(
    navController: NavHostController,
    lightColorScheme: ColorScheme,
    darkColorScheme: ColorScheme,
    textToSpeech: TextToSpeechInstance?,
    onAppOpens: ((Int) -> Unit)?,
) {
    val appSettings = koinInject<AppSettings>()
    val denebClient = koinInject<DataRepository>() as? DenebGatewayClient

    // Track app opens after Koin is initialized
    onAppOpens?.let { callback ->
        LaunchedEffect(Unit) {
            callback(appSettings.trackAppOpen())
        }
    }

    // Set up permission handlers
    val calendarPermissionController = koinInject<CalendarPermissionController>()
    SetupCalendarPermissionHandler(calendarPermissionController)

    val notificationPermissionController = koinInject<NotificationPermissionController>()
    SetupNotificationPermissionHandler(notificationPermissionController)

    val contactsPermissionController = koinInject<ContactsPermissionController>()
    SetupContactsPermissionHandler(contactsPermissionController)

    val smsPermissionController = koinInject<SmsPermissionController>()
    SetupSmsPermissionHandler(smsPermissionController)

    val smsSendPermissionController = koinInject<SmsSendPermissionController>()
    SetupSmsSendPermissionHandler(smsSendPermissionController)

    // Set TTS voice to match system language
    @OptIn(ExperimentalVoiceApi::class)
    LaunchedEffect(textToSpeech) {
        val tts = textToSpeech ?: return@LaunchedEffect
        val systemLanguage = Locale.current.language
        val matchingVoice = tts.voices
            .firstOrNull { it.languageTag.startsWith(systemLanguage) }
        if (matchingVoice != null) {
            tts.currentVoice = matchingVoice
        }
    }

    val uiScale by appSettings.uiScaleFlow.collectAsStateWithLifecycle()
    val defaultDensity = LocalDensity.current
    val scaledDensity = remember(defaultDensity, uiScale) {
        Density(defaultDensity.density * uiScale, defaultDensity.fontScale)
    }

    val themeMode by appSettings.themeModeFlow.collectAsStateWithLifecycle()
    val systemInDark = isSystemInDarkTheme()
    val effectiveColorScheme = when (themeMode) {
        ThemeMode.System -> if (systemInDark) darkColorScheme else lightColorScheme
        ThemeMode.Light -> lightColorScheme
        ThemeMode.Dark -> darkColorScheme
        ThemeMode.OledBlack -> darkColorScheme.withBlackBackground()
    }

    CompositionLocalProvider(LocalDensity provides scaledDensity) {
        Theme(colorScheme = effectiveColorScheme) {
            FullScreenImageHost {
                val chatViewModel: ChatViewModel = koinViewModel()
                // Web shows the chat/settings tab bar; mobile uses the bottom bar / drawer
                // instead, so it never had it.
                val showTabBar = currentPlatform is Platform.Web
                val currentBackStackEntry by navController.currentBackStackEntryAsState()
                val isHome = currentBackStackEntry?.destination?.route == "home"

                // 챗봇 ↔ 업무 workspace, reactive. 챗봇 hides 업무 데이터 sections from
                // every navigation surface (bottom bar, desktop rail, 더보기). Work is the
                // default when there is no gateway client (previews / other repos).
                val workspaceWorkFlow = remember(denebClient) {
                    denebClient?.workspaceWork ?: MutableStateFlow(true)
                }
                val isWorkMode by workspaceWorkFlow.collectAsStateWithLifecycle()
                val navChatMode = !isWorkMode
                // Switching into 챗봇 while parked on a now-hidden 업무 데이터 screen would
                // strand the user there with no active tab — bounce them back to home.
                val activeRoute = currentBackStackEntry?.destination?.route
                LaunchedEffect(navChatMode, activeRoute) {
                    if (navChatMode && activeRoute != null && activeRoute in denebWorkDataRoutes) {
                        navigateToDenebSection(navController, Home)
                    }
                }

                val navigationTabBar: @Composable () -> Unit = {
                    val isRtl = LocalLayoutDirection.current == LayoutDirection.Rtl
                    val count = 2
                    SingleChoiceSegmentedButtonRow {
                        SegmentedButton(
                            selected = isHome,
                            onClick = {
                                navController.navigate(Home) {
                                    popUpTo(Home) { inclusive = true }
                                    launchSingleTop = true
                                }
                            },
                            shape = SegmentedButtonDefaults.itemShape(index = if (isRtl) count - 1 else 0, count = count),
                            modifier = Modifier.handCursor(),
                        ) {
                            Text(stringResource(Res.string.tab_chat))
                        }
                        SegmentedButton(
                            selected = !isHome,
                            onClick = {
                                navController.navigate(DenebConfig) {
                                    popUpTo(Home)
                                    launchSingleTop = true
                                }
                            },
                            shape = SegmentedButtonDefaults.itemShape(index = if (isRtl) 0 else count - 1, count = count),
                            modifier = Modifier.handCursor(),
                        ) {
                            Text(stringResource(Res.string.tab_settings))
                        }
                    }
                }

                // Feed unread badge: the work feed is the 업무 home, so the unread count
                // (items not yet opened in the 피드 screen) badges the 피드 tab/rail rather
                // than a separate top-bar bell (removed). Hoisted here so the 피드 screen
                // and the nav badge share one reactive seen-set: marking an item read in
                // FeedScreen drops the badge live.
                val feedState by chatViewModel.state.collectAsStateWithLifecycle()
                var feedSeenIds by remember { mutableStateOf(appSettings.getFeedSeenIds()) }
                // Server status is the source of truth (an item acked on any device is no
                // longer "unread"); the local seen-set is an optimistic overlay for items
                // opened on this device (FeedScreen marks seen client-side, not a server
                // ack). Counting both keeps the badge from drifting.
                val feedUnread = feedState.workFeed.count { it.status == "unread" && it.id !in feedSeenIds }

                // 업무 launches into the 피드 home (work feed as the main screen); 챗봇
                // launches into the chat. Captured once — NavHost reads startDestination
                // only at first composition. A runtime workspace toggle then navigates
                // via the bottom bar (and the 챗봇 bounce LaunchedEffect above).
                val workAtStart = remember { isWorkMode }
                val navHost: @Composable (Modifier) -> Unit = { navHostModifier ->
                    // Route in-app link taps (markdown, text) to the in-app browser:
                    // http(s) → DenebBrowser (in-place translation), everything else
                    // (mailto, tel, file, …) keeps the OS handler. DenebBrowserScreen's
                    // "열기" uses openUrl directly, so it still escapes to the system browser.
                    val browserUriHandler = remember(navController) {
                        object : UriHandler {
                            override fun openUri(uri: String) {
                                if (uri.startsWith("http://", ignoreCase = true) ||
                                    uri.startsWith("https://", ignoreCase = true)
                                ) {
                                    navController.navigate(DenebBrowser(uri))
                                } else {
                                    openUrl(uri)
                                }
                            }
                        }
                    }
                    CompositionLocalProvider(LocalUriHandler provides browserUriHandler) {
                        NavHost(
                            navController,
                            startDestination = if (workAtStart) DenebFeed else Home,
                            modifier = navHostModifier.background(MaterialTheme.colorScheme.background),
                        ) {
                            composable<Home> {
                                ChatScreen(
                                    viewModel = chatViewModel,
                                    // Deneb chat is text-first — the TTS instance App
                                    // still configures above is not wired into chat.
                                    textToSpeech = null,
                                    navigationTabBar = if (showTabBar) navigationTabBar else null,
                                )
                            }
                            composable<DenebFeed> {
                                FeedScreen(
                                    items = feedState.workFeed,
                                    loaded = feedState.workFeedLoaded,
                                    seenIds = feedSeenIds,
                                    onMarkSeen = { id ->
                                        appSettings.markFeedSeen(id)
                                        feedSeenIds = appSettings.getFeedSeenIds()
                                    },
                                    onLoadDateRange = feedState.actions.refreshWorkFeedRange,
                                    onRunAction = feedState.actions.runWorkFeedAction,
                                    onSubmitFeedback = feedState.actions.submitWorkFeedFeedback,
                                    onRewrite = feedState.actions.rewriteWorkFeedCard,
                                    // 해당 피드 질문: open the card's dedicated chat (context injected)
                                    // and jump to the chat screen so the user can ask there.
                                    onAsk = { id ->
                                        feedState.actions.openWorkFeedItem(id)
                                        navigateToDenebSection(navController, Home)
                                    },
                                )
                            }
                            composable<DenebConfig> {
                                DenebConfigScreen(
                                    appSettings = appSettings,
                                    denebClient = denebClient,
                                    onBack = { navController.navigateUp() },
                                    onOpenSkill = { name -> navController.navigate(DenebSkill(name)) },
                                    onOpenCron = { id -> navController.navigate(DenebCron(id)) },
                                    onOpenFleet = { navController.navigate(DenebFleet) },
                                    navigationTabBar = if (showTabBar) navigationTabBar else null,
                                )
                            }
                            composable<DenebFleet> {
                                denebClient?.let { client ->
                                    DenebFleetScreen(
                                        client = client,
                                        onBack = { navController.navigateUp() },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebMail> {
                                denebClient?.let { client ->
                                    DenebMailScreen(
                                        client = client,
                                        onBack = { navController.navigateUp() },
                                        onOpenDetail = { id -> navController.navigate(DenebMailDetail(id)) },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebCalendar> {
                                denebClient?.let { client ->
                                    DenebCalendarScreen(
                                        client = client,
                                        onBack = { navController.navigateUp() },
                                        onOpenEvent = { id -> navController.navigate(DenebCalendarEvent(id)) },
                                        onAddEvent = { date -> navController.navigate(DenebCalendarAdd(date.toString())) },
                                        onOpenTodos = { navController.navigate(DenebTodo) },
                                        onOpenTodo = { id -> navController.navigate(DenebTodoEdit(id)) },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebTodo> {
                                denebClient?.let { client ->
                                    DenebTodoScreen(
                                        client = client,
                                        onBack = { navController.navigateUp() },
                                        onAddTodo = { navController.navigate(DenebTodoAdd) },
                                        onOpenTodo = { id -> navController.navigate(DenebTodoEdit(id)) },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebTodoAdd> {
                                denebClient?.let { client ->
                                    DenebTodoAddScreen(
                                        client = client,
                                        onBack = { navController.navigateUp() },
                                        onSaved = { navController.navigateUp() },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebTodoEdit> { entry ->
                                denebClient?.let { client ->
                                    DenebTodoAddScreen(
                                        client = client,
                                        editTodoId = entry.toRoute<DenebTodoEdit>().id,
                                        onBack = { navController.navigateUp() },
                                        onSaved = { navController.navigateUp() },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebCalendarEvent> { entry ->
                                denebClient?.let { client ->
                                    DenebCalendarEventScreen(
                                        client = client,
                                        eventId = entry.toRoute<DenebCalendarEvent>().id,
                                        onBack = { navController.navigateUp() },
                                        onEdit = { id -> navController.navigate(DenebCalendarEdit(id)) },
                                        onDeleted = { navController.navigateUp() },
                                        // 미팅 준비 / 회의록 정리 run as a main-chat agent turn; submit
                                        // the templated message and jump to the chat to watch it.
                                        onAskInChat = { msg ->
                                            feedState.actions.ask(msg)
                                            navController.navigate(Home)
                                        },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebCalendarAdd> { entry ->
                                denebClient?.let { client ->
                                    DenebCalendarAddScreen(
                                        client = client,
                                        initialDateIso = entry.toRoute<DenebCalendarAdd>().dateIso,
                                        onBack = { navController.navigateUp() },
                                        onSaved = { navController.navigateUp() },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebCalendarEdit> { entry ->
                                denebClient?.let { client ->
                                    DenebCalendarAddScreen(
                                        client = client,
                                        initialDateIso = "",
                                        editEventId = entry.toRoute<DenebCalendarEdit>().id,
                                        onBack = { navController.navigateUp() },
                                        onSaved = { navController.navigateUp() },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebSearch> {
                                denebClient?.let { client ->
                                    DenebSearchScreen(
                                        client = client,
                                        onBack = { navController.navigateUp() },
                                        onOpenWiki = { path -> navController.navigate(DenebWiki(path)) },
                                        onOpenPerson = { sender -> navController.navigate(DenebPerson(sender)) },
                                        onOpenCategories = { navController.navigate(DenebCategories) },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebMore> {
                                DenebMoreScreen(
                                    onBack = { navController.navigateUp() },
                                    onOpen = { dest -> navController.navigate(dest) },
                                    chatMode = navChatMode,
                                )
                            }
                            composable<DenebAppHub> {
                                DenebAppHubScreen(
                                    onBack = { navController.navigateUp() },
                                    onOpen = { dest -> navController.navigate(dest) },
                                )
                            }
                            composable<DenebApps> {
                                AppDrawerScreen(
                                    onBack = { navController.navigateUp() },
                                    navigationTabBar = if (showTabBar) navigationTabBar else null,
                                )
                            }
                            composable<DenebNotebooks> {
                                denebClient?.let { client ->
                                    DenebNotebooksScreen(
                                        client = client,
                                        onBack = { navController.navigateUp() },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebDashboard> {
                                denebClient?.let { client ->
                                    DenebDashboardScreen(
                                        client = client,
                                        onBack = { navController.navigateUp() },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebFiles> {
                                denebClient?.let { client ->
                                    DenebFilesScreen(
                                        client = client,
                                        onBack = { navController.navigateUp() },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebWiki> { entry ->
                                denebClient?.let { client ->
                                    DenebWikiPageScreen(
                                        client = client,
                                        path = entry.toRoute<DenebWiki>().path,
                                        onBack = { navController.navigateUp() },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebCategories> {
                                denebClient?.let { client ->
                                    DenebCategoriesScreen(
                                        client = client,
                                        onBack = { navController.navigateUp() },
                                        onOpenCategory = { cat -> navController.navigate(DenebCategoryPages(cat)) },
                                        onOpenDiary = { navController.navigate(DenebDiary) },
                                        onOpenPeople = { navController.navigate(DenebPeople) },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebDiary> {
                                denebClient?.let { client ->
                                    DenebDiaryScreen(
                                        client = client,
                                        onBack = { navController.navigateUp() },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebCategoryPages> { entry ->
                                denebClient?.let { client ->
                                    DenebCategoryPagesScreen(
                                        client = client,
                                        category = entry.toRoute<DenebCategoryPages>().category,
                                        onBack = { navController.navigateUp() },
                                        onOpenWiki = { path -> navController.navigate(DenebWiki(path)) },
                                        onOpenCategory = { cat -> navController.navigate(DenebCategoryPages(cat)) },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebPeople> {
                                denebClient?.let { client ->
                                    DenebPeopleScreen(
                                        client = client,
                                        onBack = { navController.navigateUp() },
                                        onOpenPerson = { sender -> navController.navigate(DenebPerson(sender)) },
                                        onOpenWiki = { path -> navController.navigate(DenebWiki(path)) },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebPerson> { entry ->
                                denebClient?.let { client ->
                                    DenebPersonScreen(
                                        client = client,
                                        sender = entry.toRoute<DenebPerson>().sender,
                                        onBack = { navController.navigateUp() },
                                        onOpenMail = { id -> navController.navigate(DenebMailDetail(id)) },
                                        onOpenWiki = { path -> navController.navigate(DenebWiki(path)) },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebSkill> { entry ->
                                denebClient?.let { client ->
                                    DenebSkillScreen(
                                        client = client,
                                        skillName = entry.toRoute<DenebSkill>().name,
                                        onBack = { navController.navigateUp() },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebBrowser> { entry ->
                                denebClient?.let { client ->
                                    DenebBrowserScreen(
                                        url = entry.toRoute<DenebBrowser>().url,
                                        client = client,
                                        onBack = { navController.navigateUp() },
                                    )
                                }
                            }
                            composable<DenebOrgChart> {
                                denebClient?.let { client ->
                                    DenebOrgChartScreen(
                                        client = client,
                                        onBack = { navController.navigateUp() },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebCron> { entry ->
                                denebClient?.let { client ->
                                    DenebCronScreen(
                                        client = client,
                                        cronId = entry.toRoute<DenebCron>().cronId,
                                        onBack = { navController.navigateUp() },
                                        onEdit = { id -> navController.navigate(DenebCronEdit(id)) },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebCronEdit> { entry ->
                                denebClient?.let { client ->
                                    DenebCronEditScreen(
                                        client = client,
                                        cronId = entry.toRoute<DenebCronEdit>().cronId,
                                        onBack = { navController.navigateUp() },
                                        onSaved = { navController.navigateUp() },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebMailDetail> { entry ->
                                denebClient?.let { client ->
                                    DenebMailDetailScreen(
                                        client = client,
                                        messageId = entry.toRoute<DenebMailDetail>().id,
                                        onBack = { navController.navigateUp() },
                                        onOpenWiki = { path -> navController.navigate(DenebWiki(path)) },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                        }
                    }
                }
                // The native client is mobile-only (the desktop workstation is a
                // separate app, Andromeda). Dock the super-app bottom bar under the
                // content on top-level sections (project_superapp_vision). Pushed detail
                // screens hide it and keep their back nav; the keyboard also hides it so
                // the chat input owns the bottom. The content area consumes the
                // navigation-bar inset (the bar applies it) so the screens' own
                // navigationBarsPadding doesn't double up.
                val route = currentBackStackEntry?.destination?.route
                val imeVisible = WindowInsets.ime.getBottom(LocalDensity.current) > 0
                // 챗봇 workspace is a clean focus-chat space: no bottom tab bar at all
                // (the top 챗봇/업무 pill is the only way in/out). 업무 keeps the super-app bar.
                val showBar = route in denebBottomBarRoutes && !imeVisible && !navChatMode
                Column(Modifier.fillMaxSize()) {
                    Box(
                        Modifier
                            .weight(1f)
                            .fillMaxWidth()
                            .then(
                                if (showBar) {
                                    Modifier.consumeWindowInsets(WindowInsets.navigationBars)
                                } else {
                                    Modifier
                                },
                            ),
                    ) {
                        navHost(Modifier.fillMaxSize())
                    }
                    if (showBar) {
                        DenebBottomBar(
                            currentRoute = route,
                            moreActive = route in denebMoreRoutes,
                            onNavigate = { dest -> navigateToDenebSection(navController, dest) },
                            // 더보기 always lands on the More list — not the last-opened
                            // section. Sections are pushed onto DenebMore via a plain
                            // navigate (onOpen below), so the shared navigateToDenebSection
                            // (restoreState = true) restored that saved sub-stack and left
                            // the user on the section. Pop to start and push a fresh DenebMore
                            // (no restoreState) so the list always shows.
                            onMore = {
                                navController.navigate(DenebMore) {
                                    popUpTo(navController.graph.startDestinationId) { saveState = true }
                                    launchSingleTop = true
                                }
                            },
                            feedUnread = feedUnread,
                        )
                    }
                }
            }
        }
    }
}
