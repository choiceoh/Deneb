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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.components.SkeletonList

/**
 * Shared chrome for Deneb surface screens. Material3 with intentional depth —
 * type hierarchy, tonal surfaces — so ported views read as considered rather
 * than default-styled. Lift a helper here when ≥2 screens need it.
 */

/** A tappable action chip, e.g. "+ 새 페이지". */
data class DenebChip(val label: String, val onClick: () -> Unit)

/**
 * Standard drill-down header: a small actions row (back left, optional action
 * right) above a bold title + optional subtitle. Title is optional for detail
 * views that lead with their own heading.
 */
@Composable
fun DenebViewHeader(
    title: String? = null,
    subtitle: String? = null,
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
            Text(title, style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.SemiBold)
            if (subtitle != null) {
                Text(
                    subtitle,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Spacer(Modifier.height(12.dp))
        }
    }
}

/** Horizontal action-chip strip; scrolls when it overflows. */
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

/**
 * Shimmering skeleton placeholder shown while a Deneb surface loads — content
 * fades in instead of replacing a "불러오는 중…" line. Shared by every Deneb
 * screen, so improving it here upgrades all of them at once.
 */
@Composable
fun DenebLoading(@Suppress("UNUSED_PARAMETER") text: String = "불러오는 중…") {
    SkeletonList(showAvatar = false)
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
 * Empty-state placeholder: a quiet line with an optional call-to-action, so an
 * empty list guides the user instead of looking broken or still-loading. Shared
 * by every Deneb screen.
 */
@Composable
fun DenebEmpty(text: String, actionLabel: String? = null, onAction: (() -> Unit)? = null) {
    Column(Modifier.fillMaxWidth().padding(vertical = 24.dp)) {
        Text(text, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
        if (actionLabel != null && onAction != null) {
            Spacer(Modifier.height(8.dp))
            OutlinedButton(onClick = onAction) { Text(actionLabel) }
        }
    }
}

/**
 * Convenience scaffold for simple scrolling surfaces. List-heavy screens
 * (mail, search) build their own LazyColumn scaffold instead.
 */
@Composable
fun DenebSurface(
    title: String? = null,
    subtitle: String? = null,
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
        DenebViewHeader(title, subtitle, onBack, rightLabel, onRight)
        content()
    }
}
