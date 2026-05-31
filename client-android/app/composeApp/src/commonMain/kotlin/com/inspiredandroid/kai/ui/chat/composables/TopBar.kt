package com.inspiredandroid.kai.ui.chat.composables

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Dns
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.IconButtonDefaults
import androidx.compose.material3.IconToggleButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.chat.ChatActions
import com.inspiredandroid.kai.ui.handCursor
import kai.composeapp.generated.resources.Res
import kai.composeapp.generated.resources.ic_add
import kai.composeapp.generated.resources.ic_volume_off
import kai.composeapp.generated.resources.ic_volume_up
import kai.composeapp.generated.resources.new_chat_content_description
import kai.composeapp.generated.resources.sandbox_content_description
import kai.composeapp.generated.resources.toggle_speech_output_content_description
import kotlinx.collections.immutable.ImmutableList
import kotlinx.collections.immutable.persistentListOf
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
    isSandboxAvailable: Boolean,
    isSandboxOpen: Boolean,
    isShellExecuting: Boolean,
    onToggleSandbox: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
    topics: ImmutableList<TopicTab> = persistentListOf(),
    selectedTopicKey: String? = null,
    onSelectTopic: (String) -> Unit = {},
) {
    if (navigationTabBar != null) {
        Box(
            modifier = Modifier.fillMaxWidth().defaultMinSize(minHeight = 64.dp),
        ) {
            Row(modifier = Modifier.align(Alignment.CenterStart)) {
                DrawerButton(onOpenDrawer)
                LeadingButtons(textToSpeech, isSpeechOutputEnabled, isSpeaking, actions, isChatHistoryEmpty, isSandboxAvailable, isSandboxOpen, isShellExecuting, onToggleSandbox)
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
                DenebTopicMenu(topics, selectedTopicKey, onSelectTopic)
            }
        }
    } else {
        Row {
            DrawerButton(onOpenDrawer)
            LeadingButtons(textToSpeech, isSpeechOutputEnabled, isSpeaking, actions, isChatHistoryEmpty, isSandboxAvailable, isSandboxOpen, isShellExecuting, onToggleSandbox)
            Spacer(Modifier.weight(1f))
            if (textToSpeech != null) {
                SpeechToggleButton(textToSpeech, isSpeechOutputEnabled, isSpeaking, actions)
            }
            // Search / mail / calendar / people / categories / history / settings
            // all live in the left drawer; the topic menu takes the spot where
            // the settings icon used to sit.
            DenebTopicMenu(topics, selectedTopicKey, onSelectTopic)
        }
    }
}

// DrawerButton renders the hamburger that opens the left navigation drawer.
// Null callback (e.g. previews) renders nothing so layout stays unchanged.
@Composable
private fun DrawerButton(onOpenDrawer: (() -> Unit)?) {
    if (onOpenDrawer == null) return
    IconButton(
        modifier = Modifier.handCursor(),
        onClick = onOpenDrawer,
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
    isSandboxAvailable: Boolean,
    isSandboxOpen: Boolean,
    isShellExecuting: Boolean,
    onToggleSandbox: () -> Unit,
) {
    if (!isChatHistoryEmpty) {
        IconButton(
            modifier = Modifier.handCursor(),
            onClick = {
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
    if (isSandboxAvailable) {
        val flashAlpha = remember { Animatable(0f) }
        LaunchedEffect(isShellExecuting) {
            if (isShellExecuting) {
                flashAlpha.snapTo(0.4f)
                flashAlpha.animateTo(
                    targetValue = 0f,
                    animationSpec = tween(durationMillis = 800, easing = FastOutSlowInEasing),
                )
            }
        }
        val primary = MaterialTheme.colorScheme.primary
        val checkedContainer = primary.copy(alpha = 0.2f)
        val flashContainer = primary.copy(alpha = flashAlpha.value)
        IconToggleButton(
            checked = isSandboxOpen,
            onCheckedChange = { onToggleSandbox() },
            modifier = Modifier.handCursor(),
            colors = IconButtonDefaults.iconToggleButtonColors(
                containerColor = flashContainer,
                checkedContainerColor = if (flashAlpha.value > 0f) flashContainer else checkedContainer,
                checkedContentColor = MaterialTheme.colorScheme.primary,
            ),
        ) {
            Icon(
                imageVector = Icons.Filled.Dns,
                contentDescription = stringResource(Res.string.sandbox_content_description),
                tint = if (isSandboxOpen) {
                    MaterialTheme.colorScheme.primary
                } else {
                    MaterialTheme.colorScheme.onBackground
                },
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
    IconButton(
        modifier = Modifier.handCursor(),
        onClick = {
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
