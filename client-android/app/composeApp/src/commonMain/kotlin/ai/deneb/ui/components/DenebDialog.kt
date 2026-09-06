package ai.deneb.ui.components

import ai.deneb.ui.DenebType
import ai.deneb.ui.denebHint
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.BasicAlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ProvideTextStyle
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.DialogProperties

/**
 * Deneb's dialog. The same parameters as Material's `AlertDialog`, so a call site
 * migrates by changing the name — but drawn as one of our cards, not Material's.
 *
 * Material's `BasicAlertDialog` stays underneath for what it is good at: the
 * window, the scrim, dismiss-on-outside/back, and the dialog semantics. The card
 * on top is [DenebDialogCard], which is also composable on its own so the
 * previews can render a dialog's face without a window (the headless renderer
 * cannot draw a real dialog).
 *
 * The decision-haptic rule (design-lint) anchors on this composable exactly as
 * it does on Material's: a dialog with a dismiss button decides at its confirm
 * button, which must call confirm() or reject().
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DenebDialog(
    onDismissRequest: () -> Unit,
    confirmButton: @Composable () -> Unit,
    modifier: Modifier = Modifier,
    dismissButton: (@Composable () -> Unit)? = null,
    icon: (@Composable () -> Unit)? = null,
    title: (@Composable () -> Unit)? = null,
    text: (@Composable () -> Unit)? = null,
    properties: DialogProperties = DialogProperties(),
) {
    BasicAlertDialog(
        onDismissRequest = onDismissRequest,
        modifier = modifier,
        properties = properties,
    ) {
        DenebDialogCard(
            confirmButton = confirmButton,
            dismissButton = dismissButton,
            icon = icon,
            title = title,
            text = text,
        )
    }
}

/**
 * The face of a [DenebDialog] with no window around it. Rounded like the settings
 * group cards, an opaque container (this sits over a scrim), house type for the
 * title and body, and the decision buttons right-aligned in the order the eye
 * expects: dismiss, then confirm.
 */
@Composable
fun DenebDialogCard(
    confirmButton: @Composable () -> Unit,
    modifier: Modifier = Modifier,
    dismissButton: (@Composable () -> Unit)? = null,
    icon: (@Composable () -> Unit)? = null,
    title: (@Composable () -> Unit)? = null,
    text: (@Composable () -> Unit)? = null,
) {
    val cs = MaterialTheme.colorScheme
    Column(
        modifier = modifier
            .widthIn(min = 280.dp, max = 400.dp)
            .clip(RoundedCornerShape(20.dp))
            .background(cs.surfaceContainerHigh)
            .padding(24.dp),
    ) {
        if (icon != null) {
            Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.CenterStart) {
                CompositionLocalProvider(LocalContentColor provides cs.onSurfaceVariant) { icon() }
            }
            Spacer(Modifier.height(12.dp))
        }
        if (title != null) {
            CompositionLocalProvider(LocalContentColor provides cs.onSurface) {
                ProvideTextStyle(DenebType.rowTitleStrong) { title() }
            }
            Spacer(Modifier.height(10.dp))
        }
        if (text != null) {
            CompositionLocalProvider(LocalContentColor provides denebHint()) {
                ProvideTextStyle(DenebType.body) { text() }
            }
        }
        Spacer(Modifier.height(20.dp))
        Row(
            Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (dismissButton != null) dismissButton()
            confirmButton()
        }
    }
}
