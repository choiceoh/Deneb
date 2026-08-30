package ai.deneb

import ai.deneb.ui.chat.composables.CaptureActions
import ai.deneb.ui.chat.composables.LocalCaptureActions
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.navigation.NavHostController
import coil3.ImageLoader
import coil3.PlatformContext
import coil3.compose.setSingletonImageLoaderFactory
import coil3.network.ktor3.KtorNetworkFetcherFactory
import coil3.svg.SvgDecoder
import nl.marc_apps.tts.TextToSpeechInstance
import org.koin.compose.KoinApplication
import org.koin.dsl.koinConfiguration

@Composable
fun App(
    navController: NavHostController,
    textToSpeech: TextToSpeechInstance? = null,
    isKoinStarted: Boolean = false,
    onAppOpens: ((Int) -> Unit)? = null,
    captureActions: CaptureActions? = null,
    openWorkFeedItemId: String? = null,
    onWorkFeedItemConsumed: (String) -> Unit = {},
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
            AppContent(
                navController,
                textToSpeech,
                onAppOpens,
                openWorkFeedItemId,
                onWorkFeedItemConsumed,
            )
        } else {
            KoinApplication(
                configuration = koinConfiguration {
                    modules(appModule)
                },
            ) {
                AppContent(
                    navController,
                    textToSpeech,
                    onAppOpens,
                    openWorkFeedItemId,
                    onWorkFeedItemConsumed,
                )
            }
        }
    }
}
