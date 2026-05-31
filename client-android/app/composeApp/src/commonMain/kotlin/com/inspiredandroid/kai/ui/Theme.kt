@file:Suppress("DEPRECATION")

package com.inspiredandroid.kai.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Clear
import androidx.compose.material3.CardColors
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.input.pointer.PointerIcon
import androidx.compose.ui.input.pointer.pointerHoverIcon
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import org.jetbrains.compose.ui.tooling.preview.Preview

// Deneb brand color — a deep Prussian navy (Deneb is a bright star against the
// night sky). Used as the Material primary in the light theme. The name is
// retained from the vendored Kai theme to keep the rebrand diff small.
val darkPurple = Color(0xFF003153)

// Deneb aurora palette — an iridescent cool-spectrum loop (azure → cyan/teal →
// periwinkle → soft violet). Drives the animated brand border (a slow rotating
// sheen) on the chat input, center button, collapsed pill, and history rows.
// See AnimatedGradientBorder.kt.
val auroraAzure = Color(0xFF2C6FB5)
val auroraCyan = Color(0xFF2FB6C9)
val auroraPeriwinkle = Color(0xFF6E8FE0)
val auroraViolet = Color(0xFF9B7FE0)

// Filled brand brush (send button, circular icon buttons) — a 2-stop slice of
// the aurora spectrum so solid surfaces stay cohesive with the animated border.
val gradientBrush = androidx.compose.ui.graphics.Brush.horizontalGradient(listOf(auroraAzure, auroraViolet))

fun Modifier.handCursor() = pointerHoverIcon(PointerIcon.Hand, overrideDescendants = true)

// Full Prussian-blue M3 role set. Defining every role (not just primary +
// surfaces) keeps Material's default purple/lavender from leaking into
// secondary/tertiary/container/outline surfaces — switches, segmented buttons,
// chips, dividers and error tints now read as one blue-tinted family.
val DarkColorScheme = darkColorScheme(
    primary = Color(0xFF7FA8D0),
    onPrimary = Color(0xFF00131F),
    primaryContainer = Color(0xFF004C77),
    onPrimaryContainer = Color(0xFFD4E4F5),
    inversePrimary = Color(0xFF003153),
    secondary = Color(0xFFAFC2D6),
    onSecondary = Color(0xFF0A1A28),
    secondaryContainer = Color(0xFF2C4257),
    onSecondaryContainer = Color(0xFFD4E4F5),
    tertiary = Color(0xFF8FC9C4),
    onTertiary = Color(0xFF00322E),
    tertiaryContainer = Color(0xFF1F4A46),
    onTertiaryContainer = Color(0xFFC8EEE9),
    error = Color(0xFFF2B8B5),
    onError = Color(0xFF601410),
    errorContainer = Color(0xFF8C1D18),
    onErrorContainer = Color(0xFFF9DEDC),
    background = Color(0xFF121212),
    onBackground = Color(0xFFFFFFFF),
    surface = Color(0xFF1E1E1E),
    onSurface = Color(0xFFFFFFFF),
    surfaceVariant = Color(0xFF2A2F35),
    onSurfaceVariant = Color(0xFFC2C9D1),
    surfaceTint = Color(0xFF7FA8D0),
    surfaceContainerLowest = Color(0xFF0D0D0D),
    surfaceContainerLow = Color(0xFF1A1A1A),
    surfaceContainer = Color(0xFF1E1E1E),
    surfaceContainerHigh = Color(0xFF282828),
    surfaceContainerHighest = Color(0xFF333333),
    outline = Color(0xFF5A6470),
    outlineVariant = Color(0xFF3A4048),
)

fun ColorScheme.withBlackBackground(): ColorScheme = copy(
    background = Color.Black,
    surface = Color.Black,
    surfaceContainerLowest = Color.Black,
)

val ColorScheme.isOledFlavor: Boolean get() = background == Color.Black

@Composable
fun kaiAdaptiveCardColors(): CardColors = CardDefaults.cardColors(
    containerColor = if (MaterialTheme.colorScheme.isOledFlavor) {
        Color.Transparent
    } else {
        MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f)
    },
)

@Composable
fun kaiAdaptiveCardBorder(): BorderStroke? = if (MaterialTheme.colorScheme.isOledFlavor) {
    BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant)
} else {
    null
}

@Composable
fun Modifier.kaiAdaptiveCardSurface(shape: Shape = CardDefaults.shape): Modifier = this
    .clip(shape)
    .background(
        if (MaterialTheme.colorScheme.isOledFlavor) {
            Color.Transparent
        } else {
            MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f)
        },
    )
    .then(
        if (MaterialTheme.colorScheme.isOledFlavor) {
            Modifier.border(1.dp, MaterialTheme.colorScheme.outlineVariant, shape)
        } else {
            Modifier
        },
    )

val LightColorScheme = lightColorScheme(
    primary = darkPurple,
    onPrimary = Color(0xFFFFFFFF),
    primaryContainer = Color(0xFFD2E3F7),
    onPrimaryContainer = Color(0xFF001D33),
    inversePrimary = Color(0xFF7FA8D0),
    secondary = Color(0xFF4A6072),
    onSecondary = Color(0xFFFFFFFF),
    secondaryContainer = Color(0xFFD5E3F0),
    onSecondaryContainer = Color(0xFF0A1F2E),
    tertiary = Color(0xFF1F6F68),
    onTertiary = Color(0xFFFFFFFF),
    tertiaryContainer = Color(0xFFB8ECE6),
    onTertiaryContainer = Color(0xFF00201D),
    error = Color(0xFFB3261E),
    onError = Color(0xFFFFFFFF),
    errorContainer = Color(0xFFF9DEDC),
    onErrorContainer = Color(0xFF410E0B),
    background = Color(0xFFFFFFFF),
    onBackground = Color(0xFF000000),
    surface = Color(0xFFF7F9FB),
    onSurface = Color(0xFF000000),
    surfaceVariant = Color(0xFFE1E7EE),
    onSurfaceVariant = Color(0xFF434A52),
    surfaceTint = darkPurple,
    surfaceContainerLowest = Color(0xFFFFFFFF),
    surfaceContainerLow = Color(0xFFF2F5F8),
    surfaceContainer = Color(0xFFECF1F6),
    surfaceContainerHigh = Color(0xFFE6EDF3),
    surfaceContainerHighest = Color(0xFFE0E8F0),
    outline = Color(0xFF74808C),
    outlineVariant = Color(0xFFC4CDD6),
)

@Composable
fun outlineTextFieldColors() = OutlinedTextFieldDefaults.colors()

@Composable
fun KaiOutlinedTextField(
    value: String,
    onValueChange: (String) -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    readOnly: Boolean = false,
    label: @Composable (() -> Unit)? = null,
    placeholder: @Composable (() -> Unit)? = null,
    trailingIcon: @Composable (() -> Unit)? = null,
    visualTransformation: VisualTransformation = VisualTransformation.None,
    singleLine: Boolean = false,
    minLines: Int = 1,
    maxLines: Int = if (singleLine) 1 else Int.MAX_VALUE,
) {
    OutlinedTextField(
        value = value,
        onValueChange = onValueChange,
        modifier = modifier,
        enabled = enabled,
        readOnly = readOnly,
        label = label,
        placeholder = placeholder,
        trailingIcon = trailingIcon,
        visualTransformation = visualTransformation,
        singleLine = singleLine,
        minLines = minLines,
        maxLines = maxLines,
        shape = RoundedCornerShape(12.dp),
        colors = outlineTextFieldColors(),
    )
}

@Composable
fun KaiClearableTextField(
    value: String,
    onValueChange: (String) -> Unit,
    modifier: Modifier = Modifier,
    label: @Composable (() -> Unit)? = null,
    singleLine: Boolean = false,
) {
    var focused by remember { mutableStateOf(false) }
    KaiOutlinedTextField(
        modifier = modifier.fillMaxWidth().onFocusChanged { focused = it.isFocused },
        value = value,
        onValueChange = onValueChange,
        label = label,
        singleLine = singleLine,
        trailingIcon = {
            IconButton(
                onClick = { onValueChange("") },
                modifier = Modifier.handCursor()
                    .alpha(if (focused && value.isNotEmpty()) 1f else 0f),
                enabled = value.isNotEmpty(),
            ) {
                Icon(
                    imageVector = Icons.Default.Clear,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        },
    )
}

@Composable
@Preview
fun Theme(
    colorScheme: ColorScheme,
    content: @Composable () -> Unit,
) {
    MaterialTheme(
        colorScheme = colorScheme,
        typography = pretendardTypography(),
    ) {
        content()
    }
}
