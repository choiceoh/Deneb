package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AssistChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

/**
 * Shared chrome for Deneb surface screens, ported from the Mini App's
 * `views/ui.ts` so every ported view reuses one header / chip / loading /
 * error idiom. Visuals follow the Compose theme (Prussian rebrand); the
 * structure mirrors the web surface (back link on the left, optional action
 * on the right, large lowercase title beneath).
 *
 * As more views land, lift shared pieces here when ≥2 screens need them —
 * the same rule ui.ts follows. One-shot chrome stays inline in its screen.
 */

/** A tappable action chip, e.g. "+ 새 페이지". */
data class DenebChip(val label: String, val onClick: () -> Unit)

/**
 * Standard drill-down header: an actions row (back link left, optional
 * action right) above a large title. The actions row collapses when both
 * slots are empty; the title is optional for detail views that lead with
 * their own heading.
 */
@Composable
fun DenebViewHeader(
    title: String? = null,
    onBack: (() -> Unit)? = null,
    rightLabel: String? = null,
    onRight: (() -> Unit)? = null,
) {
    val hasRight = rightLabel != null && onRight != null
    Column(Modifier.fillMaxWidth()) {
        if (onBack != null || hasRight) {
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                if (onBack != null) {
                    TextButton(onClick = onBack, contentPadding = PaddingValues(horizontal = 4.dp)) {
                        Text("← 뒤로")
                    }
                }
                Spacer(Modifier.weight(1f))
                if (hasRight) {
                    TextButton(onClick = onRight, contentPadding = PaddingValues(horizontal = 4.dp)) {
                        Text(rightLabel)
                    }
                }
            }
        }
        if (title != null) {
            Text(title, style = MaterialTheme.typography.headlineMedium)
            Spacer(Modifier.height(8.dp))
        }
    }
}

/** Horizontal action-chip strip below a header; scrolls when it overflows. */
@Composable
fun DenebChipRow(chips: List<DenebChip>) {
    if (chips.isEmpty()) return
    Row(
        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        chips.forEach { chip -> AssistChip(onClick = chip.onClick, label = { Text(chip.label) }) }
    }
}

/** Centered "loading" placeholder line. */
@Composable
fun DenebLoading(text: String = "불러오는 중…") {
    Text(
        text,
        style = MaterialTheme.typography.bodyMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(vertical = 24.dp),
    )
}

/** Error banner with an optional retry button. */
@Composable
fun DenebError(text: String, onRetry: (() -> Unit)? = null) {
    Column(Modifier.fillMaxWidth().padding(vertical = 16.dp)) {
        Text(text, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
        if (onRetry != null) {
            Spacer(Modifier.height(8.dp))
            OutlinedButton(onClick = onRetry) { Text("다시 시도") }
        }
    }
}

/**
 * Convenience scaffold for simple scrolling surfaces: status-bar padding +
 * 16dp inset + vertical scroll + the standard header. List-heavy screens
 * (mail, search) build their own LazyColumn scaffold instead.
 */
@Composable
fun DenebSurface(
    title: String? = null,
    onBack: (() -> Unit)? = null,
    rightLabel: String? = null,
    onRight: (() -> Unit)? = null,
    navigationTabBar: (@Composable () -> Unit)? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    Column(
        Modifier
            .fillMaxSize()
            .statusBarsPadding()
            .padding(16.dp)
            .verticalScroll(rememberScrollState()),
    ) {
        if (navigationTabBar != null) {
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) { navigationTabBar() }
            Spacer(Modifier.height(16.dp))
        }
        DenebViewHeader(title, onBack, rightLabel, onRight)
        content()
    }
}
