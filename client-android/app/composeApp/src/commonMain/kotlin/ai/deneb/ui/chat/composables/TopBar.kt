package ai.deneb.ui.chat.composables

import ai.deneb.ui.chat.ChatActions
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.handCursor
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LocalMinimumInteractiveComponentSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.ic_add
import deneb.composeapp.generated.resources.ic_history
import deneb.composeapp.generated.resources.ic_volume_off
import deneb.composeapp.generated.resources.ic_volume_up
import deneb.composeapp.generated.resources.new_chat_content_description
import deneb.composeapp.generated.resources.toggle_speech_output_content_description
import nl.marc_apps.tts.TextToSpeechInstance
import org.jetbrains.compose.resources.stringResource
import org.jetbrains.compose.resources.vectorResource

// TopBarHeight: user-preserved 48dp chat chrome height. Icon buttons still use
// the compact IconTouchTarget below, but the bar keeps the operator-tuned height.
private val TopBarHeight = 48.dp

// IconTouchTarget: top-bar icon buttons drop from Material's 48dp enforced minimum
// to a snug target just larger than the 24dp glyph. The bar is a dense mouse+touch
// surface the operator asked to compact, so we disable the minimum-interactive
// enforcement (LocalMinimumInteractiveComponentSize = Unspecified) around the bar
// and let this explicit size apply.
private val IconTouchTarget = 36.dp

@Composable
internal fun TopBar(
    textToSpeech: TextToSpeechInstance? = null,
    isSpeechOutputEnabled: Boolean,
    isSpeaking: Boolean,
    actions: ChatActions,
    isChatHistoryEmpty: Boolean,
    currentConversationId: String? = null,
    onOpenDrawer: (() -> Unit)? = null,
    navigationTabBar: (@Composable () -> Unit)? = null,
    onOpenSessionDrawer: (() -> Unit)? = null,
) {
    // Disable the 48dp minimum-interactive enforcement for the whole bar so the
    // icon buttons can shrink to IconTouchTarget and the bar to TopBarHeight.
    CompositionLocalProvider(LocalMinimumInteractiveComponentSize provides Dp.Unspecified) {
        if (navigationTabBar != null) {
            Box(
                modifier = Modifier.fillMaxWidth().defaultMinSize(minHeight = TopBarHeight),
            ) {
                Row(
                    modifier = Modifier.align(Alignment.CenterStart),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    DrawerButton(onOpenDrawer)
                    LeadingButtons(
                        textToSpeech,
                        isSpeechOutputEnabled,
                        isSpeaking,
                        actions,
                        isChatHistoryEmpty,
                        currentConversationId,
                    )
                }
                Box(modifier = Modifier.align(Alignment.Center)) {
                    navigationTabBar()
                }
                Row(
                    modifier = Modifier.align(Alignment.CenterEnd),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    if (textToSpeech != null) {
                        SpeechToggleButton(textToSpeech, isSpeechOutputEnabled, isSpeaking, actions)
                    }
                    SessionButton(onOpenSessionDrawer)
                }
            }
        } else {
            // Phone/desktop (no nav tab bar): leading buttons pinned at the start
            // and trailing icons at the end. A Box with aligned slots — not a
            // weight Row — so the slots stay pinned on the bar itself.
            Box(modifier = Modifier.fillMaxWidth().defaultMinSize(minHeight = TopBarHeight)) {
                Row(
                    modifier = Modifier.align(Alignment.CenterStart),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    DrawerButton(onOpenDrawer)
                    LeadingButtons(
                        textToSpeech,
                        isSpeechOutputEnabled,
                        isSpeaking,
                        actions,
                        isChatHistoryEmpty,
                        currentConversationId,
                    )
                }
                Row(
                    modifier = Modifier.align(Alignment.CenterEnd),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    if (textToSpeech != null) {
                        SpeechToggleButton(textToSpeech, isSpeechOutputEnabled, isSpeaking, actions)
                    }
                    SessionButton(onOpenSessionDrawer)
                }
            }
        }
    }
}

// SessionButton opens the right-side session selector ([DenebSessionDrawerSheet]),
// mirroring the left hamburger. Null callback (e.g. previews) renders nothing.
@Composable
private fun SessionButton(onOpenSessionDrawer: (() -> Unit)?) {
    val haptics = rememberHaptics()
    if (onOpenSessionDrawer == null) return
    IconButton(
        modifier = Modifier.handCursor().size(IconTouchTarget),
        onClick = {
            haptics.tap()
            onOpenSessionDrawer()
        },
    ) {
        Icon(
            imageVector = vectorResource(Res.drawable.ic_history),
            contentDescription = "세션",
            tint = MaterialTheme.colorScheme.onBackground,
        )
    }
}

// DrawerButton renders the hamburger that opens the left navigation drawer.
// Null callback (e.g. previews) renders nothing so layout stays unchanged.
@Composable
private fun DrawerButton(onOpenDrawer: (() -> Unit)?) {
    val haptics = rememberHaptics()
    if (onOpenDrawer == null) return
    IconButton(
        modifier = Modifier.handCursor().size(IconTouchTarget),
        onClick = {
            haptics.tap()
            onOpenDrawer()
        },
    ) {
        Icon(
            imageVector = Icons.Filled.Menu,
            contentDescription = "메뉴",
            tint = MaterialTheme.colorScheme.onBackground,
        )
    }
}

@Composable
private fun LeadingButtons(
    textToSpeech: TextToSpeechInstance?,
    isSpeechOutputEnabled: Boolean,
    isSpeaking: Boolean,
    actions: ChatActions,
    isChatHistoryEmpty: Boolean,
    currentConversationId: String?,
) {
    val haptics = rememberHaptics()
    val onBranch = !currentConversationId.isNullOrBlank() && currentConversationId != HOME_SESSION_ID
    if (!isChatHistoryEmpty || onBranch) {
        IconButton(
            modifier = Modifier.handCursor().size(IconTouchTarget),
            onClick = {
                haptics.tap()
                if (isSpeechOutputEnabled && isSpeaking) {
                    actions.setIsSpeaking(false, "")
                    textToSpeech?.stop()
                }
                actions.startNewChat()
            },
        ) {
            Icon(
                imageVector = vectorResource(Res.drawable.ic_add),
                contentDescription = stringResource(Res.string.new_chat_content_description),
                tint = MaterialTheme.colorScheme.onBackground,
            )
        }
    }
}

@Composable
private fun SpeechToggleButton(
    textToSpeech: TextToSpeechInstance,
    isSpeechOutputEnabled: Boolean,
    isSpeaking: Boolean,
    actions: ChatActions,
) {
    val haptics = rememberHaptics()
    IconButton(
        modifier = Modifier.handCursor().size(IconTouchTarget),
        onClick = {
            haptics.toggle(!isSpeechOutputEnabled)
            if (isSpeechOutputEnabled && isSpeaking) {
                actions.setIsSpeaking(false, "")
                textToSpeech.stop()
            }
            actions.toggleSpeechOutput()
        },
    ) {
        Icon(
            imageVector = if (isSpeechOutputEnabled) {
                vectorResource(Res.drawable.ic_volume_up)
            } else {
                vectorResource(Res.drawable.ic_volume_off)
            },
            contentDescription = stringResource(Res.string.toggle_speech_output_content_description),
            tint = MaterialTheme.colorScheme.onBackground,
        )
    }
}
