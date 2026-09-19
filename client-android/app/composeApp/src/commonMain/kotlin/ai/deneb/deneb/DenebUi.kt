package ai.deneb.deneb

import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebDialog
import ai.deneb.ui.components.DenebTextButton
import ai.deneb.ui.components.SkeletonList
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
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

// A state is said, not illustrated. Both helpers below are the Zune / Metro
// statement: one Light line at content-subject size, left on the gutter like
// every other line in the app, the reason a size down in hint colour, and the
// one action as a bare accent label. No icon (decoration — the sentence already
// names the surface), no box, no centring: an empty list is not a dialog, it is
// the page saying what it found. ADR 0007 principle 3 (honest empty state) and
// principle 7 (judgement first, reason one layer below, told by position and
// type rather than colour or container).
//
// The helpers carry their own 24dp gutter because they often land in slots
// WITHOUT content padding (feed/mail list bodies) — the 2026-07-05 edge-press
// report — so the statement lines up with the page title above it.

/**
 * Error statement with an optional retry: the failure in semantic `error` (ADR
 * 0008 job 2 — a failure must be visible), the retry as a bare label under it.
 */
@Composable
fun DenebError(text: String, onRetry: (() -> Unit)? = null) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 16.dp)) {
        Text(
            text,
            color = MaterialTheme.colorScheme.error,
            style = DenebType.subject,
        )
        if (onRetry != null) {
            Spacer(Modifier.height(6.dp))
            DenebStateAction("다시 시도", onRetry)
        }
    }
}

/**
 * Empty statement: what the page found (nothing), why or what will appear here
 * as [hint], and at most one [actionLabel]. Shared by every Deneb screen, so an
 * empty tab reads intentional rather than broken or still loading.
 */
@Composable
fun DenebEmpty(
    text: String,
    actionLabel: String? = null,
    onAction: (() -> Unit)? = null,
    hint: String? = null,
) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 24.dp)) {
        Text(
            text,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = DenebType.subject,
        )
        if (hint != null) {
            Spacer(Modifier.height(4.dp))
            Text(
                hint,
                color = denebHint(),
                style = DenebType.rowSubtitle,
            )
        }
        if (actionLabel != null && onAction != null) {
            Spacer(Modifier.height(6.dp))
            DenebStateAction(actionLabel, onAction)
        }
    }
}

// The state action is a text button with no horizontal inset so its label sits
// on the same gutter as the statement — the accent says "touch me", the
// position says what it belongs to.
@Composable
private fun DenebStateAction(label: String, onClick: () -> Unit) {
    DenebTextButton(onClick = onClick, contentPadding = PaddingValues(vertical = 8.dp)) { Text(label) }
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
        DenebDialog(
            onDismissRequest = { confirming = false },
            title = { Text("편집 취소") },
            text = { Text("저장하지 않은 변경사항이 사라집니다.") },
            confirmButton = {
                DenebTextButton(onClick = {
                    // Throwing the edits away is the destructive commit; 계속 편집 is
                    // the silent dismiss.
                    haptics.reject()
                    confirming = false
                    onLeave()
                }) { Text("나가기") }
            },
            dismissButton = {
                DenebTextButton(onClick = { confirming = false }) { Text("계속 편집") }
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
