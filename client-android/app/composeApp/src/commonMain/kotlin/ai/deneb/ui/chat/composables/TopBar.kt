package ai.deneb.ui.chat.composables

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import ai.deneb.ui.chat.ChatActions
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.handCursor
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

@Composable
internal fun TopBar(
    textToSpeech: TextToSpeechInstance? = null,
    isSpeechOutputEnabled: Boolean,
    isSpeaking: Boolean,
    actions: ChatActions,
    isChatHistoryEmpty: Boolean,
    onOpenDrawer: (() -> Unit)? = null,
    navigationTabBar: (@Composable () -> Unit)? = null,
    onOpenSessionDrawer: (() -> Unit)? = null,
    onOpenWorkFeed: (() -> Unit)? = null,
    workFeedCount: Int = 0,
) {
    if (navigationTabBar != null) {
        Box(
            modifier = Modifier.fillMaxWidth().defaultMinSize(minHeight = 64.dp),
        ) {
            Row(modifier = Modifier.align(Alignment.CenterStart)) {
                DrawerButton(onOpenDrawer)
                LeadingButtons(textToSpeech, isSpeechOutputEnabled, isSpeaking, actions, isChatHistoryEmpty)
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
                WorkFeedButton(onOpenWorkFeed, workFeedCount)
                SessionButton(onOpenSessionDrawer)
            }
        }
    } else {
        Row {
            DrawerButton(onOpenDrawer)
            LeadingButtons(textToSpeech, isSpeechOutputEnabled, isSpeaking, actions, isChatHistoryEmpty)
            Spacer(Modifier.weight(1f))
            if (textToSpeech != null) {
                SpeechToggleButton(textToSpeech, isSpeechOutputEnabled, isSpeaking, actions)
            }
            WorkFeedButton(onOpenWorkFeed, workFeedCount)
            SessionButton(onOpenSessionDrawer)
        }
    }
}

// WorkFeedButton opens the work-feed (action inbox) bottom sheet. A badge shows
// the pending item count. Null callback (e.g. previews) renders nothing.
@Composable
private fun WorkFeedButton(onOpenWorkFeed: (() -> Unit)?, count: Int) {
    val haptics = rememberHaptics()
    if (onOpenWorkFeed == null) return
    IconButton(
        modifier = Modifier.handCursor(),
        onClick = { haptics.tap(); onOpenWorkFeed() },
    ) {
        BadgedBox(
            badge = {
                if (count > 0) {
                    Badge { Text(if (count > 9) "9+" else count.toString()) }
                }
            },
        ) {
            Icon(
                imageVector = Icons.Filled.Notifications,
                contentDescription = "업무 알림",
                tint = MaterialTheme.colorScheme.onBackground,
            )
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
        modifier = Modifier.handCursor(),
        onClick = { haptics.tap(); onOpenSessionDrawer() },
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
        modifier = Modifier.handCursor(),
        onClick = { haptics.tap(); onOpenDrawer() },
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
) {
    val haptics = rememberHaptics()
    if (!isChatHistoryEmpty) {
        IconButton(
            modifier = Modifier.handCursor(),
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
        modifier = Modifier.handCursor(),
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
