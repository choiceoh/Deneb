package ai.deneb.deneb

import ai.deneb.PlatformBackHandler
import ai.deneb.openUrl
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.automirrored.outlined.OpenInNew
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Translate
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
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
 * Stateless browser chrome, separated from the stateful shell so renderPreviews can
 * exercise the look with mock state. Safari-style: NO top header — the page fills the
 * screen and all chrome (editable address bar + actions) sits at the BOTTOM, above the
 * system nav bar. The address bar shows the real current URL (link safety) and is
 * editable (type/paste + Go); the back action pops page history then exits; the
 * open-external action escapes to the system browser.
 *
 * Design system: Material IconButtons with functional icons, skinned with Deneb colors
 * — the translate toggle lights the warm insight accent when ON (translation is an AI
 * surface), inactive actions use the muted hint color. [content] is the page area (the
 * real WebView, or a stub) and takes the weight, so it fills above the bottom bar.
 */
@Composable
fun DenebBrowserChrome(
    state: DenebWebViewState,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
    content: @Composable ColumnScope.() -> Unit,
) {
    val haptics = rememberHaptics()
    val focusManager = LocalFocusManager.current

    // Back pops the page's own history first; at the root it exits the browser.
    PlatformBackHandler(enabled = state.canGoBack) { state.goBack() }

    // Editable address bar value, re-synced to the real URL as the page navigates.
    var field by remember(state.currentUrl) { mutableStateOf(state.currentUrl) }

    Surface(color = MaterialTheme.colorScheme.background, modifier = modifier.fillMaxSize()) {
        Column(Modifier.fillMaxSize().statusBarsPadding()) {
            // Page fills the top — no top header.
            content()
            if (state.loading) {
                LinearProgressIndicator(modifier = Modifier.fillMaxWidth(), progress = { state.progress / 100f })
            }
            HorizontalDivider(color = denebHairline())
            // Bottom chrome: back · editable address bar · refresh · translate · open-external,
            // lifted above the system navigation gesture bar.
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .navigationBarsPadding()
                    .padding(horizontal = 4.dp, vertical = 2.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                IconButton(
                    onClick = {
                        haptics.tap()
                        if (state.canGoBack) state.goBack() else onBack()
                    },
                    modifier = Modifier.size(40.dp),
                ) {
                    Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = "뒤로", tint = denebHint())
                }
                BasicTextField(
                    value = field,
                    onValueChange = { field = it },
                    singleLine = true,
                    textStyle = DenebType.meta.copy(color = MaterialTheme.colorScheme.onSurface),
                    cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Go),
                    keyboardActions = KeyboardActions(
                        onGo = {
                            val target = normalizeUrl(field)
                            if (target.isNotEmpty()) {
                                state.load(target)
                                field = target
                            }
                            focusManager.clearFocus()
                        },
                    ),
                    modifier = Modifier.weight(1f).padding(horizontal = 8.dp),
                    decorationBox = { innerTextField ->
                        if (field.isEmpty()) {
                            Text("주소 입력", style = DenebType.meta, color = denebHint(), maxLines = 1)
                        }
                        innerTextField()
                    },
                )
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
            }
        }
    }
}

/**
 * Turns address-bar input into a loadable URL: keeps an explicit http(s) scheme,
 * otherwise assumes https. Empty input stays empty (the blank "new tab" state, which
 * the WebView's blank-guard leaves unloaded).
 */
private fun normalizeUrl(input: String): String {
    val s = input.trim()
    if (s.isEmpty()) return ""
    return if (s.startsWith("http://", ignoreCase = true) || s.startsWith("https://", ignoreCase = true)) {
        s
    } else {
        "https://$s"
    }
}
