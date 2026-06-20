package ai.deneb.deneb

import ai.deneb.PlatformBackHandler
import ai.deneb.openUrl
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.ime
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.union
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowForward
import androidx.compose.material.icons.automirrored.outlined.OpenInNew
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Translate
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
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
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.text.AnnotatedString
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
 * editable (type/paste + Go); there is no on-screen back button — system back pops page
 * history then exits; the open-external action escapes to the system browser.
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

    // No on-screen back button — system back owns "back": page history first, then exit.
    PlatformBackHandler(enabled = true) { if (state.canGoBack) state.goBack() else onBack() }

    // Editable address bar value, re-synced to the real URL as the page navigates.
    var field by remember(state.currentUrl) { mutableStateOf(state.currentUrl) }
    var menuOpen by remember { mutableStateOf(false) }
    val clipboard = LocalClipboardManager.current

    Surface(color = MaterialTheme.colorScheme.background, modifier = modifier.fillMaxSize()) {
        Column(Modifier.fillMaxSize().statusBarsPadding()) {
            // Page fills the top — no top header.
            content()
            if (state.loading) {
                LinearProgressIndicator(modifier = Modifier.fillMaxWidth(), progress = { state.progress / 100f })
            }
            HorizontalDivider(color = denebHairline())
            // Bottom chrome (Safari-style): back · forward · editable address bar (omnibox) ·
            // reload-or-stop · overflow (⋮), above the system gesture bar. Secondary actions
            // (translate, copy, open-external) live in the ⋮ menu so the row scales as features grow.
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    // Float above whichever is present — the soft keyboard (when the address
                    // bar is focused) or the system nav bar. Without the ime inset the keyboard
                    // covered this bottom chrome (address bar + actions).
                    .windowInsetsPadding(WindowInsets.ime.union(WindowInsets.navigationBars))
                    .padding(horizontal = 4.dp, vertical = 2.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                IconButton(
                    onClick = {
                        haptics.tap()
                        state.goForward()
                    },
                    enabled = state.canGoForward,
                    modifier = Modifier.size(40.dp),
                ) {
                    Icon(
                        Icons.AutoMirrored.Outlined.ArrowForward,
                        contentDescription = "앞으로",
                        tint = if (state.canGoForward) denebHint() else denebHint().copy(alpha = 0.3f),
                    )
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
                            Text("주소 또는 검색", style = DenebType.meta, color = denebHint(), maxLines = 1)
                        }
                        innerTextField()
                    },
                )
                IconButton(
                    onClick = {
                        haptics.tap()
                        if (state.loading) state.stop() else state.reload()
                    },
                    modifier = Modifier.size(40.dp),
                ) {
                    Icon(
                        if (state.loading) Icons.Outlined.Close else Icons.Outlined.Refresh,
                        contentDescription = if (state.loading) "정지" else "새로고침",
                        tint = denebHint(),
                    )
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
                Box {
                    IconButton(
                        onClick = {
                            haptics.tap()
                            menuOpen = true
                        },
                        modifier = Modifier.size(40.dp),
                    ) {
                        Icon(Icons.Outlined.MoreVert, contentDescription = "더보기", tint = denebHint())
                    }
                    DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                        DropdownMenuItem(
                            text = { Text("URL 복사") },
                            leadingIcon = { Icon(Icons.Outlined.ContentCopy, contentDescription = null, tint = denebHint()) },
                            onClick = {
                                haptics.tap()
                                clipboard.setText(AnnotatedString(state.currentUrl))
                                menuOpen = false
                            },
                        )
                        DropdownMenuItem(
                            text = { Text("외부 브라우저로 열기") },
                            leadingIcon = { Icon(Icons.AutoMirrored.Outlined.OpenInNew, contentDescription = null, tint = denebHint()) },
                            onClick = {
                                haptics.tap()
                                openUrl(state.currentUrl)
                                menuOpen = false
                            },
                        )
                    }
                }
            }
        }
    }
}

/**
 * Omnibox: turns address-bar input into a loadable URL. Explicit http(s) → as-is; a
 * bare host (no spaces, has a dot) → assume https; anything else → a web search. Empty
 * stays empty (the blank "new tab" state, which the WebView's blank-guard leaves unloaded).
 */
private fun normalizeUrl(input: String): String {
    val s = input.trim()
    if (s.isEmpty()) return ""
    if (s.startsWith("http://", ignoreCase = true) || s.startsWith("https://", ignoreCase = true)) return s
    return if (!s.contains(' ') && s.contains('.')) {
        "https://$s"
    } else {
        "https://www.google.com/search?q=${encodeQuery(s)}"
    }
}

/** Percent-encodes a search query (UTF-8); space → '+'. KMP-safe (no java URLEncoder). */
private fun encodeQuery(s: String): String {
    val out = StringBuilder()
    for (b in s.encodeToByteArray()) {
        val v = b.toInt() and 0xFF
        val ch = v.toChar()
        when {
            ch in 'A'..'Z' || ch in 'a'..'z' || ch in '0'..'9' ||
                ch == '-' || ch == '_' || ch == '.' || ch == '~' -> out.append(ch)

            v == 0x20 -> out.append('+')

            else -> {
                out.append('%')
                out.append(((v shr 4) and 0xF).toString(16).uppercase())
                out.append((v and 0xF).toString(16).uppercase())
            }
        }
    }
    return out.toString()
}
