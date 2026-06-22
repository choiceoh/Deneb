@file:OptIn(ExperimentalMaterial3Api::class)

package ai.deneb

import ai.deneb.data.AppSettings
import ai.deneb.data.DataRepository
import ai.deneb.data.ThemeMode
import ai.deneb.deneb.DenebBrowserScreen
import ai.deneb.deneb.DenebCalendarAddScreen
import ai.deneb.deneb.DenebCalendarEventScreen
import ai.deneb.deneb.DenebCalendarScreen
import ai.deneb.deneb.DenebCategoriesScreen
import ai.deneb.deneb.DenebCategoryPagesScreen
import ai.deneb.deneb.DenebConfigScreen
import ai.deneb.deneb.DenebContactsScreen
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
import ai.deneb.deneb.DenebProjectDigestScreen
import ai.deneb.deneb.DenebSearchScreen
import ai.deneb.deneb.DenebSkillScreen
import ai.deneb.deneb.DenebTodoAddScreen
import ai.deneb.deneb.DenebWikiPageScreen
import ai.deneb.sensing.applyGeofences
import ai.deneb.sensing.decodeGeofences
import ai.deneb.tools.CalendarPermissionController
import ai.deneb.tools.ContactsPermissionController
import ai.deneb.tools.LocationPermissionController
import ai.deneb.tools.NotificationPermissionController
import ai.deneb.tools.SetupCalendarPermissionHandler
import ai.deneb.tools.SetupContactsPermissionHandler
import ai.deneb.tools.SetupLocationPermissionHandler
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
import ai.deneb.ui.chat.composables.denebWorkDataRoutes
import ai.deneb.ui.chat.composables.navigateToDenebSection
import ai.deneb.ui.components.FullScreenImageHost
import ai.deneb.ui.handCursor
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
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.input.nestedscroll.NestedScrollConnection
import androidx.compose.ui.input.nestedscroll.NestedScrollSource
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.platform.UriHandler
import androidx.compose.ui.text.intl.Locale
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.Velocity
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavDestination.Companion.hasRoute
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
@SerialName("deneb_todo_add")
data class DenebTodoAdd(val dueIso: String? = null)

@Serializable
@SerialName("deneb_todo_edit")
data class DenebTodoEdit(val id: String)

@Serializable
@SerialName("deneb_search")
object DenebSearch

// 더보기 — the section hub (bottom-bar tab). A grouped text list of the sections that are
// not first-class bottom-bar tabs (파트별 업무 현황·조직도·검색·할일·일기·카테고리·전체 연락처·
// 노트북·파일·브라우저·설정). 채팅·피드·메일·달력 are their own tabs. See DenebMoreScreen.
@Serializable
@SerialName("deneb_more")
object DenebMore

@Serializable
@SerialName("deneb_wiki")
data class DenebWiki(val path: String)

@Serializable
@SerialName("deneb_people")
object DenebPeople

@Serializable
@SerialName("deneb_person")
data class DenebPerson(val sender: String)

// Full address book (전체 연락처) — the raw contacts.json mirror, sectioned ㄱㄴㄷ.
// Distinct from DenebPeople (Gmail counterparties + 인물 wiki, volume-ranked).
@Serializable
@SerialName("deneb_contacts")
object DenebContacts

@Serializable
@SerialName("deneb_categories")
object DenebCategories

@Serializable
@SerialName("deneb_diary")
object DenebDiary

@Serializable
@SerialName("deneb_notebooks")
// openId deep-links straight into one notebook's detail (e.g. from a wiki project
// page's "이 딜 노트북" link); null opens the notebook list.
data class DenebNotebooks(val openId: String? = null)

@Serializable
@SerialName("deneb_dashboard")
object DenebDashboard

@Serializable
@SerialName("deneb_project_digests")
object DenebProjectDigests

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

    val locationPermissionController = koinInject<LocationPermissionController>()
    SetupLocationPermissionHandler(locationPermissionController)

    // Re-register saved geofences (집/직장) on launch — the OS clears geofences on reboot
    // and there's no boot receiver, so app start is when they come back. No-op off Android
    // or when none are pinned.
    LaunchedEffect(Unit) {
        val saved = decodeGeofences(appSettings.getGeofencesJson())
        if (saved.isNotEmpty()) applyGeofences(saved)
    }

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
                // The platform-default URI handler (NOT the in-app-browser override, which is
                // only provided deeper inside navHost). The 통화 bottom-tab opens tel: through
                // it, same as OrgContactActions does for contact calls.
                val systemUriHandler = LocalUriHandler.current
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
                                Box(Modifier.fillMaxSize()) {
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
                                        onAnswer = feedState.actions.answerWorkFeed,
                                        onSubmitFeedback = feedState.actions.submitWorkFeedFeedback,
                                        onRewrite = feedState.actions.rewriteWorkFeedCard,
                                        // 해당 피드 질문: open the card's dedicated chat (context injected)
                                        // and jump to the chat screen so the user can ask there.
                                        onAsk = { id ->
                                            feedState.actions.openWorkFeedItem(id)
                                            navigateToDenebSection(navController, Home)
                                        },
                                    )
                                    // Feed-card 정정 피드백은 위키를 고치는 ephemeral 에이전트 턴을 돌린다.
                                    // 시트는 낙관적으로 먼저 닫히므로, 돌아온 1~3줄 보고를 여기 스낵바로 띄운다.
                                    val feedbackSnackbar = remember { SnackbarHostState() }
                                    LaunchedEffect(feedState.feedbackResultText) {
                                        val msg = feedState.feedbackResultText ?: return@LaunchedEffect
                                        feedState.actions.clearFeedbackResult()
                                        feedbackSnackbar.showSnackbar(msg, duration = SnackbarDuration.Long)
                                    }
                                    SnackbarHost(feedbackSnackbar, Modifier.align(Alignment.BottomCenter))
                                }
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
                                        onAddTodo = { date -> navController.navigate(DenebTodoAdd(date.toString())) },
                                        onOpenTodo = { id -> navController.navigate(DenebTodoEdit(id)) },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                    )
                                }
                            }
                            composable<DenebTodoAdd> { entry ->
                                denebClient?.let { client ->
                                    DenebTodoAddScreen(
                                        client = client,
                                        prefillDueIso = entry.toRoute<DenebTodoAdd>().dueIso,
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
                                    // Read fresh on entry: returning here after toggling tiles in
                                    // 설정 re-executes this composable, so the grid reflects the
                                    // latest hidden set without an observable flow.
                                    hiddenTiles = appSettings.getHiddenMoreTiles(),
                                )
                            }
                            composable<DenebNotebooks> { entry ->
                                denebClient?.let { client ->
                                    DenebNotebooksScreen(
                                        client = client,
                                        onBack = { navController.navigateUp() },
                                        initialOpenId = entry.toRoute<DenebNotebooks>().openId,
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
                            composable<DenebProjectDigests> {
                                denebClient?.let { client ->
                                    DenebProjectDigestScreen(
                                        client = client,
                                        onBack = { navController.navigateUp() },
                                        // Tap a project → open its pages (the 프로젝트/<name> wiki bucket).
                                        onOpenProject = { proj -> navController.navigate(DenebCategoryPages("프로젝트/$proj")) },
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
                                        onOpenNotebook = { id -> navController.navigate(DenebNotebooks(openId = id)) },
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
                            composable<DenebContacts> {
                                denebClient?.let { client ->
                                    DenebContactsScreen(
                                        client = client,
                                        onBack = { navController.navigateUp() },
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
                            // 더보기 always lands on the hub root — don't restore the last
                            // section drilled into it (검색·할일·…), so pressing 더보기 from a
                            // detail returns to 더보기, not back onto the detail.
                            onNavigate = { dest -> navigateToDenebSection(navController, dest, restoreState = dest != DenebMore) },
                            feedUnread = feedUnread,
                        )
                    }
                }
            }
        }
    }
}
