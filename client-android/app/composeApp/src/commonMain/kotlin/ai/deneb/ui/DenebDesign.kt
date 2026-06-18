package ai.deneb.ui

import ai.deneb.Platform
import ai.deneb.currentPlatform
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

// Deneb's component idiom in native Compose (design refresh, 2026-06): a calm
// monochrome AMOLED base structured with GROUPED INSET CARDS ([DenebGroup] +
// [DenebListRow]) — the iOS/Toss-style successor to the old flat hairline rows —
// plus functional mono icons on nav and rows, and two restrained accents (cool
// `primary` = interactive, warm apricot `denebInsight` = AI-insight). These
// primitives are what Deneb screens build from so every surface reads the same.
//
// ---------------------------------------------------------------------------
// Surface, spacing and component doctrine — extracted from how the shipped
// screens actually use these primitives (companions: type laws in DenebType.kt,
// color laws in Theme.kt, motion laws in DenebMotion.kt, touch vocabulary in
// components/Haptics.kt):
//
//  SURFACE — content is grouped into rounded inset cards ([DenebGroup]) with a
//  faint monochrome wash; rows inside are separated by inset hairlines. Elevation
//  and shadow stay absent — the wash + radius carry grouping, not Material
//  elevation. The cool `primary` accent marks the selected/interactive row; the
//  warm apricot insight accent ([denebInsight]) marks AI-analysis callouts. A bare
//  [DenebRow] (single hairline, no card) is still used for content lists (mail,
//  search) that aren't settings-like. Desktop never stretches: content is capped
//  at [DenebMaxContentWidth] and centered.
//
//  SPACING — a 4dp grid with five working stops, each owning one job
//  (usage counts across deneb screens: 4dp ×78, 8dp ×151, 12dp ×96,
//  16dp ×103, 24dp ×36):
//      4dp  micro     glyph-to-text, dot-to-label
//      8dp  gap       between lines inside one row, label-to-control
//     12dp  group     between related blocks inside a section
//     16dp  module    the unit of "one thing" — row vertical padding, card
//                     inner padding, chip horizontal padding
//     24dp  gutter    the page margin (scaffold horizontal padding)
//  Off-grid values (2/6/10/14) are optical half-step corrections only — they
//  fine-tune a specific pairing and never structure a layout.
//
//  COMPONENT KIT — a screen is assembled from: [DenebScreenScaffold] (frame),
//  [DenebGroup] + [DenebListRow] (grouped inset card + its icon/title/subtitle/
//  chevron rows — the primary settings-style list idiom), [DenebRow] (a bare
//  hairline row for content lists), [DenebSectionLabel] (grouping), the state triple
//  DenebLoading / DenebError / DenebEmpty (every remote-data surface renders
//  all three: skeleton while loading, error WITH retry, empty WITH guidance),
//  and DenebChip for compact choices. Controls (switches, buttons, fields,
//  dialogs, sheets) stay stock Material — Deneb skins presentation, not
//  interaction (see .claude/rules/native-design-system.md).
//
//  INTERACTION — every tappable row is tappable across its full width, shows
//  a hand cursor on desktop, gives the denebPressable press-scale "give" from
//  DenebMotion.kt, and fires an intent-named haptic (Haptics.kt) at the call
//  site. Back/cancel/dismiss stay silent by convention.
// ---------------------------------------------------------------------------

/**
 * Readable max content width on desktop. On a wide window this keeps lines and rows from
 * stretching across the whole screen; on a phone the content fills the width as before.
 */
val DenebMaxContentWidth: Dp = 760.dp

/**
 * A width modifier for screen content: a fixed [cap] on desktop, full width on phone.
 *
 * NOTE: we deliberately do NOT use `widthIn(max).fillMaxSize()` (fillMaxSize fills the
 * incoming max and drops the cap) nor `BoxWithConstraints { width(min(maxWidth, cap)) }`
 * — under the headless native-app desktop harness `maxWidth` comes back as a bogus value
 * so `min()` misfires. A platform-gated fixed width is the only reliable cap here.
 */
@Composable
fun denebContentWidthModifier(cap: Dp = DenebMaxContentWidth): Modifier = if (currentPlatform is Platform.Desktop) Modifier.width(cap) else Modifier.fillMaxWidth()

/** Hairline rule color — onBackground at low alpha, theme-aware (≈white 9% dark / black 6% light). */
@Composable
fun denebHairline(): Color {
    val cs = MaterialTheme.colorScheme
    val dark = cs.background.luminance() < 0.5f
    return cs.onBackground.copy(alpha = if (dark) 0.10f else 0.07f)
}

/** Muted secondary text color (the Mini App's `--tg-hint`). */
@Composable
fun denebHint(): Color = MaterialTheme.colorScheme.onBackground.copy(alpha = 0.55f)

/** Tracked-caps section header in the Mini App idiom (uppercased). */
@Composable
fun DenebSectionLabel(text: String, modifier: Modifier = Modifier) {
    Text(
        text = text.uppercase(),
        style = DenebType.sectionLabel,
        color = denebHint(),
        modifier = modifier.padding(top = 22.dp, bottom = 8.dp),
    )
}

/**
 * A list row in the Mini App idiom: no card and no fill — a single hairline rule
 * under the row, roomy vertical padding, the whole row tappable. The caller lays
 * out the row's lines (title + snippet + meta) via [content].
 *
 * Tappable rows go through [denebPressable] (ripple + a subtle spring press-scale),
 * fulfilling the DenebMotion.kt contract that wiring it here lifts every list in
 * the app at once.
 */
@Composable
fun DenebRow(
    onClick: (() -> Unit)? = null,
    onLongClick: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
    content: @Composable ColumnScope.() -> Unit,
) {
    val hairline = denebHairline()
    Column(
        modifier = modifier
            .fillMaxWidth()
            .then(if (onClick != null) Modifier.denebPressable(onClick = onClick, onLongClick = onLongClick).handCursor() else Modifier)
            .drawBehind {
                val stroke = 1.dp.toPx()
                val y = size.height - stroke / 2f
                drawLine(hairline, Offset(0f, y), Offset(size.width, y), strokeWidth = stroke)
            }
            .padding(vertical = 16.dp),
        content = content,
    )
}

/**
 * The standard Deneb screen frame: a flat AMOLED surface, a small back affordance,
 * and a big ultralight lowercase title (the Mini App's `.view-title`), then the
 * scrolling content. Replaces the Material `Scaffold` + `TopAppBar` so inner
 * screens match the home menu rather than looking like stock Material.
 *
 * On desktop the title + content are centered in a column capped at [maxContentWidth];
 * on a phone the column fills the screen. The content lambda keeps its [ColumnScope],
 * so callers that use `weight(1f)` on a scrolling child still work.
 *
 * [showBack] lets top-level sections drop the back arrow on desktop, where the
 * persistent sidebar already is the navigation — sub-screens keep it everywhere.
 *
 * [fillWidth] is for screens embedded as a pane of a wider layout (the desktop
 * mail split-view): the desktop fixed-width cap would overflow a narrow pane,
 * so the column fills the parent instead.
 */
@Composable
fun DenebScreenScaffold(
    title: String,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
    tabBar: (@Composable () -> Unit)? = null,
    actions: (@Composable RowScope.() -> Unit)? = null,
    maxContentWidth: Dp = DenebMaxContentWidth,
    showBack: Boolean = true,
    fillWidth: Boolean = false,
    content: @Composable ColumnScope.() -> Unit,
) {
    Surface(color = MaterialTheme.colorScheme.background, modifier = modifier.fillMaxSize()) {
        Column(
            Modifier
                .fillMaxSize()
                .statusBarsPadding()
                .navigationBarsPadding()
                // Lift content above the soft keyboard so a focused field (and the
                // bottom of a scrolling form) never hides behind it (edge-to-edge: the
                // app owns the IME inset). No-op when the keyboard is down.
                .imePadding(),
        ) {
            if (tabBar != null) {
                Row(
                    Modifier.fillMaxWidth().padding(top = 8.dp),
                    horizontalArrangement = Arrangement.Center,
                ) { tabBar() }
            }
            Box(Modifier.fillMaxWidth().weight(1f), contentAlignment = Alignment.TopCenter) {
                val widthMod = if (fillWidth) Modifier.fillMaxWidth() else denebContentWidthModifier(maxContentWidth)
                Column(widthMod.fillMaxHeight()) {
                    Column(Modifier.padding(start = 24.dp, end = 24.dp, top = 14.dp, bottom = 6.dp)) {
                        // Android has a system back button/gesture, so the in-app ← is
                        // redundant there — hide it and let the OS drive back (every onBack
                        // is navigateUp, which system back already does; dirty-form guards
                        // intercept system back themselves). Desktop has no system back at
                        // all (the ← is the only way back) and iOS convention shows it, so
                        // both keep it.
                        if (showBack && currentPlatform !is Platform.Mobile.Android) {
                            // A 44dp box gives the back glyph a real touch target + a
                            // TalkBack label/role (it frames every pushed screen, so a
                            // bare clickable "←" was the app-wide weak spot). Left-aligned
                            // so the arrow still sits at the content inset. Mirrors the
                            // session-row × treatment.
                            Box(
                                modifier = Modifier
                                    .size(44.dp)
                                    .clickable(onClickLabel = "뒤로", role = Role.Button, onClick = onBack)
                                    .handCursor(),
                                contentAlignment = Alignment.CenterStart,
                            ) {
                                Text(
                                    text = "←",
                                    style = DenebType.subject.copy(fontSize = 22.sp),
                                    color = denebHint(),
                                )
                            }
                        }
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(
                                text = title,
                                style = DenebType.viewTitle,
                                color = MaterialTheme.colorScheme.onBackground,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                                modifier = Modifier.weight(1f),
                            )
                            if (actions != null) {
                                Row(verticalAlignment = Alignment.CenterVertically, content = actions)
                            }
                        }
                    }
                    content()
                }
            }
        }
    }
}
