package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AssistChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/**
 * Shared chrome for Deneb surface screens. Material3, but with intentional
 * depth — type hierarchy, tonal surfaces, avatars — so ported views read as
 * considered rather than default-styled. Lift a helper here when ≥2 screens
 * need it.
 */

/** A tappable action chip, e.g. "+ 새 페이지". */
data class DenebChip(val label: String, val onClick: () -> Unit)

/**
 * Standard drill-down header: a small actions row (back left, optional action
 * right) above a bold title. Title is optional for detail views that lead with
 * their own heading.
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
 * Circular monogram avatar with a stable per-label color, ported in spirit
 * from the web rows — a leading visual anchor that gives lists weight instead
 * of a flat wall of text. The color is deterministic so the same sender keeps
 * the same hue across the app.
 */
@Composable
fun DenebAvatar(label: String, size: Dp = 40.dp) {
    val initial = label.trim().firstOrNull { it.isLetterOrDigit() }?.uppercaseChar()?.toString() ?: "·"
    val base = avatarPalette[(label.hashCode() and Int.MAX_VALUE) % avatarPalette.size]
    Box(
        modifier = Modifier
            .size(size)
            .clip(CircleShape)
            .background(Brush.linearGradient(listOf(base, base.copy(alpha = 0.78f)))),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            initial,
            color = Color.White,
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.SemiBold,
        )
    }
}

private val avatarPalette = listOf(
    Color(0xFF3B5BA5), Color(0xFF2E8B8B), Color(0xFFB05A8E), Color(0xFF8E6FC4),
    Color(0xFF4C8C4A), Color(0xFFC07A3E), Color(0xFF4A86C5), Color(0xFF9C6B5A),
)

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
