@file:Suppress("DEPRECATION")

package ai.deneb.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.CardColors
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.input.pointer.PointerIcon
import androidx.compose.ui.input.pointer.pointerHoverIcon
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import org.jetbrains.compose.ui.tooling.preview.Preview

// ---------------------------------------------------------------------------
// Deneb color doctrine — the laws this file's values already follow, stated so
// new colors are derived instead of picked (companion to the type laws in
// DenebType.kt and the spacing/surface laws in DenebDesign.kt):
//
//  1. One hue, every role. A single Prussian navy seeds a COMPLETE blue-tinted
//     M3 role set in both schemes, so stock Material purple never leaks into
//     switches/chips/containers. MaterialTheme.colorScheme is the single source
//     of chrome color — screens do not hardcode hex for UI chrome.
//  2. Aurora is the only polychrome, and it is rationed. The iridescent loop
//     (azure → cyan → periwinkle → violet) marks live/agentic and brand
//     affordances only (animated input border, send). Scarcity is what makes
//     it read as "the agent is alive" — spreading it would demote it to décor.
//  3. Neutrals are derived, not picked: hairlines and hints are onBackground at
//     fixed alphas (see DenebDesign.kt), so every neutral self-adapts across
//     light / dark / OLED instead of maintaining grey ramps.
//  4. OLED is a flavor, not a theme: background snaps to true black and FILLS
//     BECOME OUTLINES (denebAdaptiveCard*) — surfaces are redefined, not merely
//     darkened, so the AMOLED panel turns off where there is nothing.
//  5. Status is semantic and two-tiered: tiny accents (dots/badges) stay
//     saturated and theme-invariant so they read at 8dp on any flavor, while
//     status CONTAINERS are theme-paired. Error uses the M3 role; success and
//     warning below fill the roles M3 lacks.
// ---------------------------------------------------------------------------

// Deneb brand color — a deep Prussian navy (Deneb is a bright star against the
// night sky). Used as the Material primary in the light theme. The name is
// retained from the vendored upstream theme to keep the rebrand diff small.
val darkPurple = Color(0xFF003153)

// Status accents (law 5, accent tier) — promoted from the values screens were
// hand-rolling (model-health dots in ConfigModelTab). Deliberately plain vals,
// not @Composable: an 8dp dot must stay saturated on every flavor, so these do
// not adapt. For text/containers use the *Container pairs below or the M3
// error role instead.
val statusSuccess = Color(0xFF4CAF50)
val statusWarning = Color(0xFFFFB300)
val statusDanger = Color(0xFFE53935)

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

/**
 * The app's only color scheme (ADR 0007). Deneb ships a single theme — OLED black —
 * so this is what every screen is drawn in and what the preview goldens capture.
 * Kept as a derivation of [DarkColorScheme] rather than a hand-written scheme so the
 * two never drift: only the three ground colors differ.
 */
val OledColorScheme: ColorScheme = DarkColorScheme.withBlackBackground()

val ColorScheme.isOledFlavor: Boolean get() = background == Color.Black

@Composable
fun denebAdaptiveCardColors(): CardColors = CardDefaults.cardColors(
    containerColor = if (MaterialTheme.colorScheme.isOledFlavor) {
        Color.Transparent
    } else {
        MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f)
    },
)

@Composable
fun denebAdaptiveCardBorder(): BorderStroke? = if (MaterialTheme.colorScheme.isOledFlavor) {
    BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant)
} else {
    null
}

@Composable
fun Modifier.denebAdaptiveCardSurface(shape: Shape = CardDefaults.shape): Modifier = this
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

// Status containers (law 5, container tier) — the success/warning surface+text
// pairs M3's scheme lacks, promoted from the values RenderAlert hand-rolled.
// Theme-paired like errorContainer/onErrorContainer; dark detection mirrors
// denebHairline()'s background-luminance probe so OLED counts as dark.
private val ColorScheme.isDarkFlavor: Boolean get() = background.luminance() < 0.5f

@Composable
fun denebSuccessContainer(): Color = if (MaterialTheme.colorScheme.isDarkFlavor) Color(0xFF1B3A1B) else Color(0xFFE8F5E9)

@Composable
fun denebOnSuccessContainer(): Color = if (MaterialTheme.colorScheme.isDarkFlavor) Color(0xFFC8E6C9) else Color(0xFF1B5E20)

@Composable
fun denebWarningContainer(): Color = if (MaterialTheme.colorScheme.isDarkFlavor) Color(0xFF3D2600) else Color(0xFFFFF3E0)

@Composable
fun denebOnWarningContainer(): Color = if (MaterialTheme.colorScheme.isDarkFlavor) Color(0xFFFF9100) else Color(0xFFE65100)

// ── Accent doctrine (design refresh, 2026-06) ───────────────────────────────
// Two restrained accents on the monochrome AMOLED base:
//   • Interactive = the existing Material `primary` (cool, Prussian-derived blue,
//     #7FA8D0 on dark / #003153 light). Used on active tabs, selected rows, links,
//     primary CTAs — it was suppressed to ink before this refresh.
//   • ★Single accent since ADR 0007. The second accent (warm apricot for AI insight)
//     is retired: its justification was the 분석↔비서 persona split, which ADR 0001
//     forbids. [denebInsight] is now a neutral emphasis, not a hue. Color stays
//     restrained: small marks on interactive things, never a whole screen.

/**
 * Emphasis foreground for analysis callouts. **Monochrome since ADR 0007.**
 *
 * This used to be a warm apricot, justified as the cool↔warm complement of the primary
 * standing for Deneb's 분석↔비서 halves. That justification contradicted ADR 0001, which
 * forbids splitting the persona — the palette was encoding a distinction the product
 * denies, which is why the color never found a place on screen. Under the single-accent
 * principle only what the user can touch gets color; analysis is set apart by weight and
 * position instead (원리 7), so this is now the brightest neutral rather than a hue.
 *
 * Kept as a token rather than deleted so the ~15 call sites stay one line from being
 * re-tuned — or re-colored, if the decision is ever revisited.
 */
@Composable
fun denebInsight(): Color = MaterialTheme.colorScheme.onBackground

/**
 * Warm accent for the IN-APP BROWSER's own chrome — the translate/bookmark/ad-block
 * toggles when they are on, and the translation status strip.
 *
 * This is a deliberate exception to the single-accent rule (ADR 0007 principle 5,
 * ADR 0008 job 1), recorded in ADR 0009. The rule holds everywhere the app draws its
 * own surfaces: one cool accent marks what you can touch. Browser chrome is the one
 * place that is NOT the app's own surface — it floats over third-party pages whose
 * links, buttons and highlights are overwhelmingly blue, so the cool accent stops
 * reading as "this control is on" and starts reading as more page.
 *
 * The hue is a true orange (hue 25 deg, not the 2026-06 apricot's washed 27 deg at
 * two thirds the saturation) chosen for punch at icon size. Its old justification is
 * NOT revived: the cool<->warm = analysis<->assistant persona mapping stays rejected
 * (it contradicts ADR 0001). The reason here is contrast against foreign content, and
 * it applies to nothing outside the browser. Do not widen this token's call sites;
 * app surfaces keep [denebInsight].
 */
@Composable
fun denebBrowserAccent(): Color = if (MaterialTheme.colorScheme.isDarkFlavor) Color(0xFFF97316) else Color(0xFFB3510A)

/** Neutral wash for analysis callouts — heavier than [denebGroupSurface]'s so a callout
 *  still reads as set apart, without spending a color on it. */
@Composable
fun denebInsightContainer(): Color = MaterialTheme.colorScheme.onBackground.copy(alpha = 0.10f)

@Composable
fun outlineTextFieldColors() = OutlinedTextFieldDefaults.colors()

@Composable
fun DenebOutlinedTextField(
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
    isError: Boolean = false,
    supportingText: @Composable (() -> Unit)? = null,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default,
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
        isError = isError,
        supportingText = supportingText,
        keyboardOptions = keyboardOptions,
        shape = RoundedCornerShape(12.dp),
        colors = outlineTextFieldColors(),
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
