package ai.deneb.ui.chat.composables

import ai.deneb.ui.chat.ChatActions
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import ai.deneb.ui.handCursor
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
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

@Composable
internal fun TopBar(
    textToSpeech: TextToSpeechInstance? = null,
    isSpeechOutputEnabled: Boolean,
    isSpeaking: Boolean,
    actions: ChatActions,
    isChatHistoryEmpty: Boolean,
    recallEnabled: Boolean = true,
    onOpenDrawer: (() -> Unit)? = null,
    navigationTabBar: (@Composable () -> Unit)? = null,
    onOpenSessionDrawer: (() -> Unit)? = null,
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
                RecallModePill(recallEnabled, actions)
                if (textToSpeech != null) {
                    SpeechToggleButton(textToSpeech, isSpeechOutputEnabled, isSpeaking, actions)
                }
                SessionButton(onOpenSessionDrawer)
            }
        }
    } else {
        // Phone/desktop (no nav tab bar): the 챗봇/업무 mode pill takes the true
        // center of the bar (Trae-style), with leading buttons pinned at the
        // start and trailing icons at the end. A Box with three aligned slots —
        // not a weight Row — so the pill is centered on the bar itself, not just
        // pushed to the right of the leading group (the previous off-center bug).
        Box(modifier = Modifier.fillMaxWidth().defaultMinSize(minHeight = 64.dp)) {
            Row(
                modifier = Modifier.align(Alignment.CenterStart),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                DrawerButton(onOpenDrawer)
                LeadingButtons(textToSpeech, isSpeechOutputEnabled, isSpeaking, actions, isChatHistoryEmpty)
            }
            Box(modifier = Modifier.align(Alignment.Center)) {
                RecallModePill(recallEnabled, actions)
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

// SessionButton opens the right-side session selector ([DenebSessionDrawerSheet]),
// mirroring the left hamburger. Null callback (e.g. previews) renders nothing.
@Composable
private fun SessionButton(onOpenSessionDrawer: (() -> Unit)?) {
    val haptics = rememberHaptics()
    if (onOpenSessionDrawer == null) return
    IconButton(
        modifier = Modifier.handCursor(),
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
        modifier = Modifier.handCursor(),
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

// RecallModePill is the top-bar mode switch (Trae-style): a rounded track with
// two segments, the active one a filled pill. "챗봇" = focused chat (gateway
// recall off), "업무" = full work context (recall on). This is a CONTEXT toggle,
// not a persona split — the same single assistant answers either way; only
// whether long-term work memories are recalled (and retained) changes.
@Composable
private fun RecallModePill(recallEnabled: Boolean, actions: ChatActions) {
    val haptics = rememberHaptics()
    Row(
        modifier = Modifier
            .padding(horizontal = 4.dp)
            .clip(RoundedCornerShape(percent = 50))
            .background(MaterialTheme.colorScheme.onBackground.copy(alpha = 0.06f))
            .padding(2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        RecallModeSegment(
            label = "챗봇",
            selected = !recallEnabled,
            onSelect = {
                if (recallEnabled) {
                    haptics.toggle(false)
                    actions.toggleRecall()
                }
            },
        )
        RecallModeSegment(
            label = "업무",
            selected = recallEnabled,
            onSelect = {
                if (!recallEnabled) {
                    haptics.toggle(true)
                    actions.toggleRecall()
                }
            },
        )
    }
}

// RecallModeSegment is one tab of the pill: ink + filled pill when active, hint
// text on the bare track when not. Material selectable + Role.Tab for a11y;
// Deneb presentation (ink/hint, no border).
@Composable
private fun RecallModeSegment(label: String, selected: Boolean, onSelect: () -> Unit) {
    Text(
        text = label,
        style = MaterialTheme.typography.labelMedium,
        fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal,
        color = if (selected) MaterialTheme.colorScheme.onBackground else denebHint(),
        modifier = Modifier
            .handCursor()
            .clip(RoundedCornerShape(percent = 50))
            .background(
                if (selected) MaterialTheme.colorScheme.onBackground.copy(alpha = 0.14f) else Color.Transparent,
            )
            .selectable(selected = selected, role = Role.Tab, onClick = onSelect)
            .padding(horizontal = 12.dp, vertical = 5.dp),
    )
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
