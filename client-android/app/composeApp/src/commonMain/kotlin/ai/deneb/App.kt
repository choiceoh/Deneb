@file:OptIn(ExperimentalMaterial3Api::class, ExperimentalSharedTransitionApi::class)

package ai.deneb

import ai.deneb.data.AppSettings
import ai.deneb.data.DataRepository
import ai.deneb.deneb.DenebApprovalDetailScreen
import ai.deneb.deneb.DenebApprovalsScreen
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
import ai.deneb.deneb.DenebGroupwareERPScreen
import ai.deneb.deneb.DenebMailDetailScreen
import ai.deneb.deneb.DenebMailScreen
import ai.deneb.deneb.DenebMoreScreen
import ai.deneb.deneb.DenebNotebooksScreen
import ai.deneb.deneb.DenebOrgChartScreen
import ai.deneb.deneb.DenebPeopleScreen
import ai.deneb.deneb.DenebPersonScreen
import ai.deneb.deneb.DenebRsiScreen
import ai.deneb.deneb.DenebSearchScreen
import ai.deneb.deneb.DenebSkillScreen
import ai.deneb.deneb.DenebTodoAddScreen
import ai.deneb.deneb.DenebUsageScreen
import ai.deneb.deneb.DenebWikiPageScreen
import ai.deneb.deneb.filesDownloadUrl
import ai.deneb.deneb.hiddenMoreTilesForUi
import ai.deneb.deneb.markWorkFeedRead
import ai.deneb.deneb.openWorkFeedItem
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
import ai.deneb.ui.LiveTab
import ai.deneb.ui.LiveTabPane
import ai.deneb.ui.LocalSharedTransitionScope
import ai.deneb.ui.OledColorScheme
import ai.deneb.ui.Theme
import ai.deneb.ui.chat.ChatScreen
import ai.deneb.ui.chat.ChatViewModel
import ai.deneb.ui.chat.composables.DenebBottomBar
import ai.deneb.ui.chat.composables.DenebBottomBarHeight
import ai.deneb.ui.chat.composables.FeedLane
import ai.deneb.ui.chat.composables.FeedScreen
import ai.deneb.ui.chat.composables.ROUTE_CALENDAR
import ai.deneb.ui.chat.composables.ROUTE_FEED
import ai.deneb.ui.chat.composables.ROUTE_HOME
import ai.deneb.ui.chat.composables.ROUTE_MAIL
import ai.deneb.ui.chat.composables.ROUTE_MAIN
import ai.deneb.ui.chat.composables.ROUTE_MORE
import ai.deneb.ui.chat.composables.denebBottomChromeReservePx
import ai.deneb.ui.chat.composables.denebLiveTabRequests
import ai.deneb.ui.chat.composables.denebShowsBottomBar
import ai.deneb.ui.chat.composables.isDenebLiveTab
import ai.deneb.ui.chat.composables.navigateToDenebSection
import ai.deneb.ui.chat.isLogCard
import ai.deneb.ui.components.FullScreenImageHost
import ai.deneb.ui.denebComposable
import ai.deneb.ui.denebNavEnter
import ai.deneb.ui.denebNavExit
import ai.deneb.ui.denebNavPopEnter
import ai.deneb.ui.denebNavPopExit
import ai.deneb.ui.handCursor
import androidx.compose.animation.ExperimentalSharedTransitionApi
import androidx.compose.animation.SharedTransitionLayout
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.ime
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.union
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
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.platform.UriHandler
import androidx.compose.ui.text.intl.Locale
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.LayoutDirection
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.toRoute
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.tab_chat
import deneb.composeapp.generated.resources.tab_settings
import kotlinx.datetime.TimeZone
import kotlinx.datetime.toLocalDateTime
import kotlinx.datetime.todayIn
import nl.marc_apps.tts.TextToSpeechInstance
import nl.marc_apps.tts.experimental.ExperimentalVoiceApi
import org.jetbrains.compose.resources.stringResource
import org.koin.compose.koinInject
import org.koin.compose.viewmodel.koinViewModel
import kotlin.time.Clock
import kotlin.time.Instant

@Composable
internal fun AppContent(
    navController: NavHostController,
    textToSpeech: TextToSpeechInstance?,
    onAppOpens: ((Int) -> Unit)?,
    openWorkFeedItemId: String?,
    onWorkFeedItemConsumed: (String) -> Unit,
) {
    val appSettings = koinInject<AppSettings>()
    val denebClient = koinInject<DataRepository>() as? DenebGatewayClient

    // Live-tab pane state: which of the five bottom-bar tabs is showing. The tabs
    // render outside the NavHost (LiveTabPane) and stay alive across switches, so
    // selection is plain state — saved like a nav route would be.
    var selectedTabRoute by rememberSaveable { mutableStateOf(ROUTE_FEED) }
    // Pending 피드 deep-open (phone push / dashboard tap). seq bumps per request so
    // FeedScreen (alive across switches) re-arms even for the same item id.
    var feedOpenRequest by remember { mutableStateOf<FeedOpenRequest?>(null) }

    /** Select a live tab by its destination and return the NavHost to the resting
     *  stub, so the pane is what's on screen. Non-tab destinations are ignored. */
    fun openLiveTab(dest: Any) {
        selectedTabRoute = when (dest) {
            is DenebFeed -> ROUTE_FEED
            Home -> ROUTE_HOME
            DenebMail -> ROUTE_MAIL
            DenebCalendar -> ROUTE_CALENDAR
            DenebMore -> ROUTE_MORE
            else -> return
        }
        if (dest is DenebFeed && !dest.openItemId.isNullOrBlank()) {
            feedOpenRequest = FeedOpenRequest(
                itemId = dest.openItemId,
                createdAtMs = dest.openItemCreatedAtMs,
                seq = (feedOpenRequest?.seq ?: 0L) + 1L,
            )
        }
        navController.popBackStack(DenebMain, inclusive = false)
    }

    // Tab switches requested by callers that only hold a NavHostController (the
    // desktop harness shortcuts via navigateToDenebSection) arrive on this bus.
    LaunchedEffect(navController) {
        denebLiveTabRequests.collect { dest -> openLiveTab(dest) }
    }

    LaunchedEffect(openWorkFeedItemId) {
        val itemId = openWorkFeedItemId?.trim()?.takeIf(String::isNotEmpty)
            ?: return@LaunchedEffect
        openLiveTab(DenebFeed(openItemId = itemId))
        onWorkFeedItemConsumed(itemId)
    }

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
    // Ask once. The launcher above has been wired for a long time but nothing ever
    // pulled it: requestPermission() had no callers, so on Android 13+ (targetSdk 36
    // shows no automatic prompt) a fresh install silently posted nothing — no FCM
    // reports, no heartbeat, no work-feed alerts, no daemon notification — and every
    // post path just returned early. Gated on a flag because Android ignores a second
    // prompt after a denial anyway.
    LaunchedEffect(Unit) {
        if (!appSettings.notificationPermissionAsked() && !notificationPermissionController.hasPermission()) {
            appSettings.setNotificationPermissionAsked(true)
            notificationPermissionController.requestPermission()
        }
    }

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

    // Deneb ships one theme (ADR 0007): OLED black. There is no picker and no system
    // following — the palette is part of the product, not a preference.
    CompositionLocalProvider(LocalDensity provides scaledDensity) {
        Theme(colorScheme = OledColorScheme) {
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
                val currentRoute = currentBackStackEntry?.destination?.route?.substringBefore('?')
                // "Home" = the chat tab showing through the resting stub (the tabs
                // live outside the NavHost, so the chat is never a nav route).
                val isHome = currentRoute == ROUTE_MAIN && selectedTabRoute == ROUTE_HOME

                val navigationTabBar: @Composable () -> Unit = {
                    val isRtl = LocalLayoutDirection.current == LayoutDirection.Rtl
                    val count = 2
                    SingleChoiceSegmentedButtonRow {
                        SegmentedButton(
                            selected = isHome,
                            onClick = { openLiveTab(Home) },
                            shape = SegmentedButtonDefaults.itemShape(index = if (isRtl) count - 1 else 0, count = count),
                            modifier = Modifier.handCursor(),
                        ) {
                            Text(stringResource(Res.string.tab_chat))
                        }
                        SegmentedButton(
                            selected = !isHome,
                            onClick = {
                                navController.navigate(DenebConfig) {
                                    popUpTo(DenebMain)
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
                // ack). Counting both keeps the badge from drifting. Scoped to TODAY's items
                // (당일 피드) by createdAtMs so the badge tracks "오늘 새로 온 것", not the
                // all-time backlog — matching the day-grouped 피드 screen.
                val feedTz = TimeZone.currentSystemDefault()
                val feedToday = Clock.System.todayIn(feedTz)
                val feedUnread = feedState.workFeed.count {
                    !it.isLogCard &&
                        it.status == "unread" &&
                        it.id !in feedSeenIds &&
                        it.readAtMs == 0L &&
                        Instant.fromEpochMilliseconds(it.createdAtMs).toLocalDateTime(feedTz).date == feedToday
                }

                // Route in-app link taps (markdown, text) to the in-app browser:
                // http(s) → DenebBrowser (in-place translation), everything else
                // (mailto, tel, file, …) keeps the OS handler. DenebBrowserScreen's
                // "열기" uses openUrl directly, so it still escapes to the system browser.
                //
                // Provided around BOTH the NavHost and the LiveTabPane below. It used to
                // sit inside the navHost lambda, which silently stopped covering chat when
                // #3834 moved the five tabs out of the NavHost into the always-alive pane:
                // taps in chat markdown and deneb-ui buttons fell through to the platform
                // handler and left the app for the system browser.
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
                val navHost: @Composable (Modifier) -> Unit = { navHostModifier ->
                    SharedTransitionLayout(modifier = navHostModifier) {
                        CompositionLocalProvider(LocalSharedTransitionScope provides this) {
                            NavHost(
                                navController,
                                // Rest on the transparent stub — the five tabs render in
                                // the always-alive LiveTabPane beneath this NavHost, and
                                // pushed sections/details slide over it. No background
                                // here: the host Box paints it, and the stub must stay
                                // see-through for the pane.
                                startDestination = DenebMain,
                                modifier = Modifier.fillMaxSize(),
                                enterTransition = { denebNavEnter() },
                                exitTransition = { denebNavExit() },
                                popEnterTransition = { denebNavPopEnter() },
                                popExitTransition = { denebNavPopExit() },
                            ) {
                                denebComposable<DenebMain> { Box(Modifier.fillMaxSize()) }
                                denebComposable<DenebConfig> {
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
                                denebComposable<DenebFleet> {
                                    denebClient?.let { client ->
                                        DenebFleetScreen(
                                            client = client,
                                            onBack = { navController.navigateUp() },
                                            navigationTabBar = if (showTabBar) navigationTabBar else null,
                                        )
                                    }
                                }
                                denebComposable<DenebTodoAdd> { entry ->
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
                                denebComposable<DenebTodoEdit> { entry ->
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
                                denebComposable<DenebCalendarEvent> { entry ->
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
                                                openLiveTab(Home)
                                            },
                                            navigationTabBar = if (showTabBar) navigationTabBar else null,
                                        )
                                    }
                                }
                                denebComposable<DenebCalendarAdd> { entry ->
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
                                denebComposable<DenebCalendarEdit> { entry ->
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
                                denebComposable<DenebSearch> {
                                    denebClient?.let { client ->
                                        DenebSearchScreen(
                                            client = client,
                                            onBack = { navController.navigateUp() },
                                            onOpenWiki = { path -> navController.navigate(DenebWiki(path)) },
                                            onOpenPerson = { sender -> navController.navigate(DenebPerson(sender)) },
                                            onOpenFile = { hit -> openUrl(client.filesDownloadUrl(hit.path)) },
                                            onOpenMail = { id -> navController.navigate(DenebMailDetail(id)) },
                                            onOpenCategories = { navController.navigate(DenebCategories) },
                                            navigationTabBar = if (showTabBar) navigationTabBar else null,
                                        )
                                    }
                                }
                                denebComposable<DenebNotebooks> { entry ->
                                    denebClient?.let { client ->
                                        DenebNotebooksScreen(
                                            client = client,
                                            onBack = { navController.navigateUp() },
                                            initialOpenId = entry.toRoute<DenebNotebooks>().openId,
                                            navigationTabBar = if (showTabBar) navigationTabBar else null,
                                        )
                                    }
                                }
                                denebComposable<DenebDashboard> {
                                    denebClient?.let { client ->
                                        DenebDashboardScreen(
                                            client = client,
                                            onBack = { navController.navigateUp() },
                                            onOpenWorkFeedItem = { itemId, createdAtMs ->
                                                openLiveTab(
                                                    DenebFeed(
                                                        openItemId = itemId,
                                                        openItemCreatedAtMs = createdAtMs,
                                                    ),
                                                )
                                            },
                                            navigationTabBar = if (showTabBar) navigationTabBar else null,
                                        )
                                    }
                                }
                                denebComposable<DenebRsi> {
                                    denebClient?.let { client ->
                                        DenebRsiScreen(
                                            client = client,
                                            onBack = { navController.navigateUp() },
                                            navigationTabBar = if (showTabBar) navigationTabBar else null,
                                        )
                                    }
                                }
                                denebComposable<DenebUsage> {
                                    denebClient?.let { client ->
                                        DenebUsageScreen(
                                            client = client,
                                            onBack = { navController.navigateUp() },
                                            navigationTabBar = if (showTabBar) navigationTabBar else null,
                                        )
                                    }
                                }
                                denebComposable<DenebFiles> {
                                    denebClient?.let { client ->
                                        DenebFilesScreen(
                                            client = client,
                                            onBack = { navController.navigateUp() },
                                            navigationTabBar = if (showTabBar) navigationTabBar else null,
                                        )
                                    }
                                }
                                denebComposable<DenebWiki> { entry ->
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
                                denebComposable<DenebCategories> {
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
                                denebComposable<DenebDiary> {
                                    denebClient?.let { client ->
                                        DenebDiaryScreen(
                                            client = client,
                                            onBack = { navController.navigateUp() },
                                            navigationTabBar = if (showTabBar) navigationTabBar else null,
                                        )
                                    }
                                }
                                denebComposable<DenebCategoryPages> { entry ->
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
                                denebComposable<DenebPeople> {
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
                                denebComposable<DenebFeedLog> {
                                    FeedScreen(
                                        items = feedState.workFeed,
                                        loaded = feedState.workFeedLoaded,
                                        seenIds = feedSeenIds,
                                        lane = FeedLane.Log,
                                        onMarkSeen = { id ->
                                            appSettings.markFeedSeen(id)
                                            feedSeenIds = appSettings.getFeedSeenIds()
                                            feedState.actions.markWorkFeedRead(id)
                                        },
                                        onLoadDateRange = feedState.actions.refreshWorkFeedRange,
                                        onRunAction = feedState.actions.runWorkFeedAction,
                                        onAnswer = feedState.actions.answerWorkFeed,
                                        onSubmitFeedback = feedState.actions.submitWorkFeedFeedback,
                                        onRewrite = feedState.actions.rewriteWorkFeedCard,
                                        onAsk = { id ->
                                            feedState.actions.openWorkFeedItem(id)
                                            openLiveTab(Home)
                                        },
                                        onOpenFeed = { openLiveTab(DenebFeed()) },
                                        onOpenApprovals = { navigateToDenebSection(navController, DenebApprovals) },
                                        navigationTabBar = if (showTabBar) navigationTabBar else null,
                                        onOpenApprovalDetail = { docId, title ->
                                            navController.navigate(
                                                DenebApprovalDetail(
                                                    docId = docId,
                                                    title = title,
                                                    canAct = true,
                                                ),
                                            )
                                        },
                                    )
                                }
                                denebComposable<DenebApprovals> {
                                    denebClient?.let { client ->
                                        DenebApprovalsScreen(
                                            client = client,
                                            onBack = { navController.navigateUp() },
                                            onOpenDetail = { doc ->
                                                navController.navigate(
                                                    DenebApprovalDetail(
                                                        docId = doc.docId,
                                                        title = doc.title,
                                                        drafter = doc.drafter,
                                                        date = doc.date,
                                                        canAct = doc.canAct,
                                                        folder = doc.folder,
                                                    ),
                                                )
                                            },
                                            onOpenFeed = { openLiveTab(DenebFeed()) },
                                            onOpenLog = { navigateToDenebSection(navController, DenebFeedLog) },
                                            navigationTabBar = if (showTabBar) navigationTabBar else null,
                                        )
                                    }
                                }
                                denebComposable<DenebApprovalDetail> { entry ->
                                    denebClient?.let { client ->
                                        val route = entry.toRoute<DenebApprovalDetail>()
                                        DenebApprovalDetailScreen(
                                            client = client,
                                            docId = route.docId,
                                            title = route.title,
                                            drafter = route.drafter,
                                            date = route.date,
                                            canAct = route.canAct,
                                            folder = route.folder,
                                            onBack = { navController.navigateUp() },
                                            onActed = { navController.navigateUp() },
                                            navigationTabBar = if (showTabBar) navigationTabBar else null,
                                        )
                                    }
                                }
                                denebComposable<DenebGroupware> {
                                    denebClient?.let { client ->
                                        DenebGroupwareERPScreen(
                                            client = client,
                                            onBack = { navController.navigateUp() },
                                            navigationTabBar = if (showTabBar) navigationTabBar else null,
                                        )
                                    }
                                }
                                denebComposable<DenebContacts> {
                                    denebClient?.let { client ->
                                        DenebContactsScreen(
                                            client = client,
                                            onBack = { navController.navigateUp() },
                                            navigationTabBar = if (showTabBar) navigationTabBar else null,
                                        )
                                    }
                                }
                                denebComposable<DenebPerson> { entry ->
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
                                denebComposable<DenebSkill> { entry ->
                                    denebClient?.let { client ->
                                        DenebSkillScreen(
                                            client = client,
                                            skillName = entry.toRoute<DenebSkill>().name,
                                            onBack = { navController.navigateUp() },
                                            navigationTabBar = if (showTabBar) navigationTabBar else null,
                                        )
                                    }
                                }
                                denebComposable<DenebBrowser> { entry ->
                                    denebClient?.let { client ->
                                        DenebBrowserScreen(
                                            url = entry.toRoute<DenebBrowser>().url,
                                            client = client,
                                            appSettings = appSettings,
                                            onBack = { navController.navigateUp() },
                                        )
                                    }
                                }
                                denebComposable<DenebOrgChart> {
                                    denebClient?.let { client ->
                                        DenebOrgChartScreen(
                                            client = client,
                                            onBack = { navController.navigateUp() },
                                            navigationTabBar = if (showTabBar) navigationTabBar else null,
                                            onOpenPersonWiki = { path -> navController.navigate(DenebWiki(path)) },
                                        )
                                    }
                                }
                                denebComposable<DenebCron> { entry ->
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
                                denebComposable<DenebCronEdit> { entry ->
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
                                denebComposable<DenebMailDetail> { entry ->
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
                }
                // The five always-alive tabs (LiveTabPane). Built here — not in the
                // NavHost — so a tab switch is a crossfade between already-composed
                // screens: no rebuild, no entry refetch, every remember survives.
                val liveTabs = listOf(
                    LiveTab(ROUTE_FEED) {
                        Box(Modifier.fillMaxSize()) {
                            FeedScreen(
                                items = feedState.workFeed,
                                loaded = feedState.workFeedLoaded,
                                seenIds = feedSeenIds,
                                initialOpenItemId = feedOpenRequest?.itemId,
                                initialOpenItemCreatedAtMs = feedOpenRequest?.createdAtMs ?: 0L,
                                openRequestKey = feedOpenRequest?.seq ?: 0L,
                                onMarkSeen = { id ->
                                    appSettings.markFeedSeen(id)
                                    feedSeenIds = appSettings.getFeedSeenIds()
                                    // Also stamp it read on the gateway so the
                                    // desktop and a reinstall see it as read.
                                    feedState.actions.markWorkFeedRead(id)
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
                                    openLiveTab(Home)
                                },
                                // Zune-style title pivot: 피드 | 결재 | 로그.
                                onOpenApprovals = { navigateToDenebSection(navController, DenebApprovals) },
                                onOpenLog = { navigateToDenebSection(navController, DenebFeedLog) },
                                onOpenApprovalDetail = { docId, title ->
                                    navController.navigate(
                                        DenebApprovalDetail(
                                            docId = docId,
                                            title = title,
                                            canAct = true,
                                        ),
                                    )
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
                    },
                    LiveTab(ROUTE_MAIL) {
                        denebClient?.let { client ->
                            DenebMailScreen(
                                client = client,
                                onBack = { openLiveTab(DenebFeed()) },
                                onOpenDetail = { id -> navController.navigate(DenebMailDetail(id)) },
                                navigationTabBar = if (showTabBar) navigationTabBar else null,
                            )
                        }
                    },
                    LiveTab(ROUTE_HOME) {
                        ChatScreen(
                            viewModel = chatViewModel,
                            // Deneb chat is text-first — the TTS instance App
                            // still configures above is not wired into chat.
                            textToSpeech = null,
                            navigationTabBar = if (showTabBar) navigationTabBar else null,
                        )
                    },
                    LiveTab(ROUTE_CALENDAR) {
                        denebClient?.let { client ->
                            DenebCalendarScreen(
                                client = client,
                                onBack = { openLiveTab(DenebFeed()) },
                                onOpenEvent = { id -> navController.navigate(DenebCalendarEvent(id)) },
                                onAddEvent = { date -> navController.navigate(DenebCalendarAdd(date.toString())) },
                                onAddTodo = { date -> navController.navigate(DenebTodoAdd(date.toString())) },
                                onOpenTodo = { id -> navController.navigate(DenebTodoEdit(id)) },
                                navigationTabBar = if (showTabBar) navigationTabBar else null,
                            )
                        }
                    },
                    LiveTab(ROUTE_MORE) {
                        // The hub is alive now — it no longer re-executes on entry, so
                        // re-read the hidden-tile set on every nav change (covers
                        // returning from 설정 where tiles were just toggled).
                        val hiddenTiles = remember(currentBackStackEntry) { appSettings.hiddenMoreTilesForUi() }
                        DenebMoreScreen(
                            onBack = { openLiveTab(DenebFeed()) },
                            onOpen = { dest -> navController.navigate(dest) },
                            hiddenTiles = hiddenTiles,
                        )
                    },
                )

                // The native client is mobile-only (the desktop workstation is a
                // separate app, Andromeda). Dock the super-app bottom bar under the
                // content on top-level sections (project_superapp_vision). Pushed detail
                // screens hide it and keep their back nav.
                //
                // Keyboard: reserve max(tabBar, IME) as one bottom chrome so the
                // composer rides the IME curve. Chat hides the bar entirely
                // (KakaoTalk room) — its own imePadding owns the bottom. Other
                // tab routes keep the bar; the content box consumes ime+nav so
                // child imePadding / navigationBarsPadding don't double-apply.
                val route = currentRoute
                val density = LocalDensity.current
                val imePx = WindowInsets.ime.getBottom(density)
                val onTabRoute = denebShowsBottomBar(route, selectedTabRoute)
                val tabBarFullPx = with(density) {
                    (
                        DenebBottomBarHeight +
                            WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding()
                        ).roundToPx()
                }
                val bottomReservePx = if (onTabRoute) denebBottomChromeReservePx(tabBarFullPx, imePx) else 0
                val showBar = onTabRoute && imePx < tabBarFullPx
                Column(Modifier.fillMaxSize()) {
                    Box(
                        Modifier
                            .weight(1f)
                            .fillMaxWidth()
                            // The screen background lives here now (not on the NavHost),
                            // so the transparent resting stub shows the pane beneath.
                            .background(MaterialTheme.colorScheme.background)
                            .then(
                                if (onTabRoute) {
                                    Modifier.consumeWindowInsets(
                                        WindowInsets.ime.union(WindowInsets.navigationBars),
                                    )
                                } else {
                                    Modifier
                                },
                            ),
                    ) {
                        // System back at the resting stub steps to 피드 before exiting
                        // the app (mirrors the old popUpTo-start stack). Composed before
                        // the pane so tab-internal handlers (chat drawers) win when both
                        // are enabled.
                        PlatformBackHandler(enabled = route == ROUTE_MAIN && selectedTabRoute != ROUTE_FEED) {
                            openLiveTab(DenebFeed())
                        }
                        CompositionLocalProvider(LocalUriHandler provides browserUriHandler) {
                            LiveTabPane(
                                selectedRoute = selectedTabRoute,
                                tabs = liveTabs,
                                modifier = Modifier.fillMaxSize(),
                            )
                            navHost(Modifier.fillMaxSize())
                        }
                    }
                    if (onTabRoute) {
                        Box(
                            Modifier
                                .fillMaxWidth()
                                .height(with(density) { bottomReservePx.toDp() }),
                        ) {
                            if (showBar) {
                                DenebBottomBar(
                                    // At the resting stub the bar highlights the live tab; on a
                                    // pushed bar-keeping section it highlights by route as before.
                                    modifier = Modifier.align(Alignment.BottomCenter),
                                    currentRoute = if (route == ROUTE_MAIN) selectedTabRoute else route,
                                    onNavigate = { dest ->
                                        if (isDenebLiveTab(dest)) openLiveTab(dest) else navigateToDenebSection(navController, dest)
                                    },
                                    feedUnread = feedUnread,
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

/** A pending 피드 deep-open (phone push / dashboard tap). [seq] bumps per request so
 *  the always-alive FeedScreen re-arms even when the same item is opened twice. */
private data class FeedOpenRequest(val itemId: String, val createdAtMs: Long, val seq: Long)
