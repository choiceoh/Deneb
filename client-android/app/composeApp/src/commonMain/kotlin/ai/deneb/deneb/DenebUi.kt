package ai.deneb.deneb

import ai.deneb.ui.DenebType
import ai.deneb.ui.components.SkeletonList
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp

/**
 * Shared loading / error / empty states + helpers for Deneb surface screens.
 *
 * The old DenebSurface / DenebViewHeader / DenebChip(Row) chrome was removed once
 * every screen migrated onto DenebScreenScaffold (see
 * docs/agent-rules/native-design-system.md). Only the cross-screen state helpers and
 * humanBytes remain here.
 */

/**
 * Shimmering skeleton placeholder shown while a Deneb surface loads — content
 * fades in instead of replacing a "불러오는 중…" line. Shared by every Deneb
 * screen, so improving it here upgrades all of them at once.
 */
@Composable
fun DenebLoading(@Suppress("UNUSED_PARAMETER") text: String = "불러오는 중…") {
    SkeletonList(showAvatar = false)
}

/**
 * Error banner with an optional retry button. Centered with its own horizontal
 * padding: these state helpers often render in containers WITHOUT content
 * padding (feed/mail list slots), where the old left-aligned, padding-less
 * layout pressed the text against the physical screen edge (reported on the
 * feed empty state, 2026-07-05; the mail error banner had the same lean).
 */
@Composable
fun DenebError(text: String, onRetry: (() -> Unit)? = null) {
    Column(
        Modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 16.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text,
            color = MaterialTheme.colorScheme.error,
            style = DenebType.body,
            textAlign = TextAlign.Center,
        )
        if (onRetry != null) {
            Spacer(Modifier.height(8.dp))
            OutlinedButton(onClick = onRetry) { Text("다시 시도") }
        }
    }
}

/**
 * Empty-state placeholder: a quiet line with an optional call-to-action, so an
 * empty list guides the user instead of looking broken or still-loading. Shared
 * by every Deneb screen. Centered for the same edge-press reason as DenebError.
 */
@Composable
fun DenebEmpty(
    text: String,
    actionLabel: String? = null,
    onAction: (() -> Unit)? = null,
    // Designed-empty upgrades (backward compatible — bare-text call sites keep
    // their old rendering): a quiet leading icon names the surface, a hint line
    // says what will appear here, so an empty tab reads intentional, not broken.
    icon: ImageVector? = null,
    hint: String? = null,
) {
    Column(
        Modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        if (icon != null) {
            Icon(
                imageVector = icon,
                contentDescription = null,
                tint = denebHint(),
                modifier = Modifier.size(28.dp),
            )
            Spacer(Modifier.height(10.dp))
        }
        Text(
            text,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = DenebType.body,
            textAlign = TextAlign.Center,
        )
        if (hint != null) {
            Spacer(Modifier.height(4.dp))
            Text(
                hint,
                color = denebHint(),
                style = DenebType.meta,
                textAlign = TextAlign.Center,
            )
        }
        if (actionLabel != null && onAction != null) {
            Spacer(Modifier.height(8.dp))
            OutlinedButton(onClick = onAction) { Text(actionLabel) }
        }
    }
}

/**
 * Guards an in-progress form against losing edits to a stray back press. Returns a
 * "request back" lambda to use for the screen's back affordance (scaffold ← and any
 * cancel button): while [dirty] it pops a discard-confirm dialog instead of leaving;
 * when clean it leaves immediately. System back is intercepted the same way while
 * dirty. Typical use:
 *
 *     val requestBack = rememberDiscardGuard(dirty, onBack)
 *     DenebScreenScaffold(title = …, onBack = requestBack) { … }
 */
@Composable
fun rememberDiscardGuard(dirty: Boolean, onLeave: () -> Unit): () -> Unit {
    var confirming by remember { mutableStateOf(false) }
    val haptics = rememberHaptics()
    // System/gesture back: intercept only while there are unsaved edits.
    ai.deneb.PlatformBackHandler(enabled = dirty) { confirming = true }
    if (confirming) {
        AlertDialog(
            onDismissRequest = { confirming = false },
            title = { Text("편집 취소") },
            text = { Text("저장하지 않은 변경사항이 사라집니다.") },
            confirmButton = {
                TextButton(onClick = {
                    // Throwing the edits away is the destructive commit; 계속 편집 is
                    // the silent dismiss.
                    haptics.reject()
                    confirming = false
                    onLeave()
                }) { Text("나가기") }
            },
            dismissButton = {
                TextButton(onClick = { confirming = false }) { Text("계속 편집") }
            },
        )
    }
    return { if (dirty) confirming = true else onLeave() }
}

/** Bytes -> short human size (integer units; KMP-safe). Shared by the mail and
 *  category screens so the formatter lives in one place. */
internal fun humanBytes(bytes: Long): String = when {
    bytes <= 0 -> "0B"
    bytes < 1024 -> "${bytes}B"
    bytes < 1024 * 1024 -> "${bytes / 1024}KB"
    else -> "${bytes / (1024 * 1024)}MB"
}
