package ai.deneb.deneb

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier

/** Non-Android stub: the in-app translation browser is an Android-only feature.
 *  Lets the desktop render harness exercise the surrounding chrome/navigation. */
@Composable
actual fun DenebWebView(state: DenebWebViewState, translate: TranslateFn, modifier: Modifier) {
    Box(modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        Text("인앱 브라우저는 안드로이드 전용입니다")
    }
}
