package ai.deneb.deneb

import ai.deneb.PlatformBackHandler
import ai.deneb.openUrl
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.OpenInNew
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Translate
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

/**
 * In-app browser for external links, with one-tap in-place translation (en/ru →
 * ko). Android renders a real WebView; other platforms show an Android-only stub
 * (the chrome and navigation still render, so the desktop harness / renderPreviews
 * can exercise them).
 */
@Composable
fun DenebBrowserScreen(
    url: String,
    client: DenebGatewayClient,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val state = remember(url) { DenebWebViewState(url) }
    DenebBrowserChrome(state = state, onBack = onBack, modifier = modifier) {
        DenebWebView(
            state = state,
            translate = { segments, lang -> client.translateSegments(segments, lang) },
            modifier = Modifier.fillMaxWidth().weight(1f),
        )
    }
}

/**
 * Stateless browser chrome (scaffold + URL bar + actions), separated from the
 * stateful shell so renderPreviews can exercise the look with mock state. The URL
 * bar always shows the real current URL (link safety — an in-app WebView otherwise
 * hides where you are); the open-external action escapes to the system browser.
 *
 * Design system: Material IconButtons with functional icons (like the mail
 * toolbar), skinned with Deneb colors — the translate toggle lights the warm
 * insight accent when ON (translation is an AI surface), inactive actions use the
 * muted hint color. [content] is the page area (the real WebView, or a stub).
 */
@Composable
fun DenebBrowserChrome(
    state: DenebWebViewState,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
    content: @Composable ColumnScope.() -> Unit,
) {
    val haptics = rememberHaptics()

    // Back pops the page's own history first; the screen is left only at the root.
    PlatformBackHandler(enabled = state.canGoBack) { state.goBack() }

    DenebScreenScaffold(
        title = "브라우저",
        onBack = onBack,
        modifier = modifier,
        fillWidth = true,
        actions = {
            IconButton(
                onClick = {
                    haptics.tap()
                    state.reload()
                },
                modifier = Modifier.size(40.dp),
            ) {
                Icon(Icons.Outlined.Refresh, contentDescription = "새로고침", tint = denebHint())
            }
            IconButton(
                onClick = {
                    haptics.tap()
                    state.translateEnabled = !state.translateEnabled
                },
                modifier = Modifier.size(40.dp),
            ) {
                Icon(
                    Icons.Outlined.Translate,
                    contentDescription = if (state.translateEnabled) "원문 보기" else "한국어로 번역",
                    tint = if (state.translateEnabled) denebInsight() else denebHint(),
                )
            }
            IconButton(
                onClick = {
                    haptics.tap()
                    openUrl(state.currentUrl)
                },
                modifier = Modifier.size(40.dp),
            ) {
                Icon(
                    Icons.AutoMirrored.Outlined.OpenInNew,
                    contentDescription = "외부 브라우저로 열기",
                    tint = denebHint(),
                )
            }
        },
    ) {
        Box(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
            Text(
                text = state.currentUrl,
                style = DenebType.meta,
                color = denebHint(),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        HorizontalDivider(color = denebHairline())
        if (state.loading) {
            LinearProgressIndicator(modifier = Modifier.fillMaxWidth(), progress = { state.progress / 100f })
        }
        content()
    }
}
