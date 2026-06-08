package com.inspiredandroid.kai.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.widthIn
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
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

// The Mini App's component idiom in native Compose: typography on a flat surface,
// separated by hairline rules — no Material cards, fills, shadows, or icons. These
// primitives are what Deneb screens build from so every surface reads the same.

/**
 * Readable max content width. On a wide desktop window this keeps lines and rows from
 * stretching across the whole screen; on a phone (narrower than this) it is a no-op,
 * so the same screens stay full-bleed on mobile. Pair with [DenebContentWidth] or the
 * built-in centering in [DenebScreenScaffold].
 */
val DenebMaxContentWidth: Dp = 920.dp

/**
 * Centers [content] in a column capped at [maxWidth] (desktop), or fills the width on a
 * phone. Drop this around the body of a self-built screen (one that doesn't use
 * [DenebScreenScaffold]) — e.g. mail, search, people — so its list/header read at a
 * comfortable width instead of spanning a 1280px window edge to edge.
 */
@Composable
fun DenebContentWidth(
    modifier: Modifier = Modifier,
    maxWidth: Dp = DenebMaxContentWidth,
    content: @Composable ColumnScope.() -> Unit,
) {
    Box(modifier.fillMaxWidth(), contentAlignment = Alignment.TopCenter) {
        Column(Modifier.widthIn(max = maxWidth).fillMaxSize(), content = content)
    }
}

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
 */
@Composable
fun DenebRow(
    onClick: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
    content: @Composable ColumnScope.() -> Unit,
) {
    val hairline = denebHairline()
    Column(
        modifier = modifier
            .fillMaxWidth()
            .then(if (onClick != null) Modifier.denebPressable(onClick = onClick).handCursor() else Modifier)
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
 * On a wide desktop window the title + content are centered in a column capped at
 * [maxContentWidth]; on a phone that cap is wider than the screen, so it stays
 * full-bleed. The content lambda keeps its [ColumnScope], so callers that use
 * `weight(1f)` on a scrolling child still work.
 */
@Composable
fun DenebScreenScaffold(
    title: String,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
    tabBar: (@Composable () -> Unit)? = null,
    maxContentWidth: Dp = DenebMaxContentWidth,
    content: @Composable ColumnScope.() -> Unit,
) {
    Surface(color = MaterialTheme.colorScheme.background, modifier = modifier.fillMaxSize()) {
        Column(
            Modifier
                .fillMaxSize()
                .statusBarsPadding()
                .navigationBarsPadding(),
        ) {
            if (tabBar != null) {
                Row(
                    Modifier.fillMaxWidth().padding(top = 8.dp),
                    horizontalArrangement = Arrangement.Center,
                ) { tabBar() }
            }
            Box(Modifier.fillMaxWidth().weight(1f), contentAlignment = Alignment.TopCenter) {
                Column(Modifier.widthIn(max = maxContentWidth).fillMaxSize()) {
                    Column(Modifier.padding(start = 24.dp, end = 24.dp, top = 14.dp, bottom = 6.dp)) {
                        Text(
                            text = "←",
                            style = DenebType.subject.copy(fontSize = 22.sp),
                            color = denebHint(),
                            modifier = Modifier.clickable(onClick = onBack).handCursor(),
                        )
                        Spacer(Modifier.height(2.dp))
                        Text(
                            text = title,
                            style = DenebType.viewTitle,
                            color = MaterialTheme.colorScheme.onBackground,
                        )
                    }
                    content()
                }
            }
        }
    }
}
