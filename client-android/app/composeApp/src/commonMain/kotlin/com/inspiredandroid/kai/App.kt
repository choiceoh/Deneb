@file:OptIn(ExperimentalMaterial3Api::class)

package com.inspiredandroid.kai

import androidx.compose.foundation.background
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.text.intl.Locale
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.LayoutDirection
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
import com.inspiredandroid.kai.data.AppSettings
import com.inspiredandroid.kai.data.ThemeMode
import com.inspiredandroid.kai.data.DataRepository
import com.inspiredandroid.kai.deneb.DenebConfigScreen
import com.inspiredandroid.kai.deneb.DenebGatewayClient
import com.inspiredandroid.kai.deneb.DenebCalendarEventScreen
import com.inspiredandroid.kai.deneb.DenebCalendarScreen
import com.inspiredandroid.kai.deneb.DenebMailDetailScreen
import com.inspiredandroid.kai.deneb.DenebMailScreen
import com.inspiredandroid.kai.deneb.DenebPeopleScreen
import com.inspiredandroid.kai.deneb.DenebPersonScreen
import com.inspiredandroid.kai.deneb.DenebSearchScreen
import com.inspiredandroid.kai.deneb.DenebWikiPageScreen
import com.inspiredandroid.kai.tools.CalendarPermissionController
import com.inspiredandroid.kai.tools.NotificationPermissionController
import com.inspiredandroid.kai.tools.SetupCalendarPermissionHandler
import com.inspiredandroid.kai.tools.SetupNotificationPermissionHandler
import com.inspiredandroid.kai.tools.SetupSmsPermissionHandler
import com.inspiredandroid.kai.tools.SetupSmsSendPermissionHandler
import com.inspiredandroid.kai.tools.SmsPermissionController
import com.inspiredandroid.kai.tools.SmsSendPermissionController
import com.inspiredandroid.kai.ui.DarkColorScheme
import com.inspiredandroid.kai.ui.LightColorScheme
import com.inspiredandroid.kai.ui.Theme
import com.inspiredandroid.kai.ui.chat.ChatScreen
import com.inspiredandroid.kai.ui.chat.ChatViewModel
import com.inspiredandroid.kai.ui.components.FullScreenImageHost
import com.inspiredandroid.kai.ui.handCursor
import com.inspiredandroid.kai.ui.settings.SettingsScreen
import com.inspiredandroid.kai.ui.withBlackBackground
import kai.composeapp.generated.resources.Res
import kai.composeapp.generated.resources.tab_chat
import kai.composeapp.generated.resources.tab_settings
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
@SerialName("settings")
object Settings

@Serializable
@SerialName("deneb_config")
object DenebConfig

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
@SerialName("deneb_search")
object DenebSearch

@Serializable
@SerialName("deneb_wiki")
data class DenebWiki(val path: String)

@Serializable
@SerialName("deneb_people")
object DenebPeople

@Serializable
@SerialName("deneb_person")
data class DenebPerson(val sender: String)

@Composable
fun App(
    navController: NavHostController,
    lightColorScheme: ColorScheme = LightColorScheme,
    darkColorScheme: ColorScheme = DarkColorScheme,
    textToSpeech: TextToSpeechInstance? = null,
    isKoinStarted: Boolean = false,
    onAppOpens: ((Int) -> Unit)? = null,
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
                val showTabBar = currentPlatform !is Platform.Mobile
                val currentBackStackEntry by navController.currentBackStackEntryAsState()
                val isHome = currentBackStackEntry?.destination?.route == "home"

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

                NavHost(
                    navController,
                    startDestination = Home,
                    modifier = Modifier.background(MaterialTheme.colorScheme.background),
                ) {
                    composable<Home> {
                        ChatScreen(
                            viewModel = chatViewModel,
                            // TTS removed for Deneb — text-first client.
                            textToSpeech = null,
                            onNavigateToSettings = {
                                navController.navigate(DenebConfig)
                            },
                            onOpenMail = { navController.navigate(DenebMail) },
                            onOpenCalendar = { navController.navigate(DenebCalendar) },
                            onOpenSearch = { navController.navigate(DenebSearch) },
                            onOpenPeople = { navController.navigate(DenebPeople) },
                            // Deneb runs tools on the gateway; the local terminal/sandbox is irrelevant.
                            isSandboxAvailable = false,
                            navigationTabBar = if (showTabBar) navigationTabBar else null,
                        )
                    }
                    composable<Settings> {
                        if (showTabBar) {
                            DisposableEffect(Unit) {
                                onDispose {
                                    chatViewModel.refreshSettings()
                                }
                            }
                        }
                        SettingsScreen(
                            onNavigateBack = {
                                chatViewModel.refreshSettings()
                                navController.navigateUp()
                            },
                            navigationTabBar = if (showTabBar) navigationTabBar else null,
                        )
                    }
                    composable<DenebConfig> {
                        DenebConfigScreen(
                            appSettings = appSettings,
                            denebClient = denebClient,
                            onBack = { navController.navigateUp() },
                            onOpenKaiSettings = { navController.navigate(Settings) },
                            navigationTabBar = if (showTabBar) navigationTabBar else null,
                        )
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
                    composable<DenebPeople> {
                        denebClient?.let { client ->
                            DenebPeopleScreen(
                                client = client,
                                onBack = { navController.navigateUp() },
                                onOpenPerson = { sender -> navController.navigate(DenebPerson(sender)) },
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
                                navigationTabBar = if (showTabBar) navigationTabBar else null,
                            )
                        }
                    }
                }
            }
        }
    }
}
