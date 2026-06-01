@file:OptIn(ExperimentalMaterial3Api::class)

package com.inspiredandroid.kai.ui.chat.composables

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.em
import androidx.compose.ui.unit.sp
import com.inspiredandroid.kai.ui.components.rememberHaptics
import com.inspiredandroid.kai.ui.handCursor

// Deneb-specific chat chrome: the left navigation drawer (a typographic menu in
// the Mini App's idiom — pure words, no icons). Kept out of ChatScreen.kt to hold
// that file under the size guideline; the chat UI stays free of any deneb-package
// import by speaking UI-neutral types and primitive callbacks.

/**
 * Platform capture actions surfaced in the drawer. Provided by the Android entry
 * point via [LocalCaptureActions]; null (the default) hides them on platforms
 * (desktop/iOS) without these system launchers.
 */
data class CaptureActions(
    val onCaptureImage: () -> Unit,
    val onCaptureAudio: () -> Unit,
    val onVoiceInput: () -> Unit,
)

/** Ambient capture actions for the drawer; null hides the capture footer. */
val LocalCaptureActions = compositionLocalOf<CaptureActions?> { null }

/**
 * The left drawer, restyled as the Mini App's typographic menu (its home idiom,
 * frontend/src/views/home.ts): pure black-and-white words, no icons, no
 * dividers — the page is the list. Big ultralight lowercase rows navigate to
 * the domain surfaces; a small capture footer (Android only) hangs below. The
 * chat itself stays the home screen — this menu is revealed by a left swipe, so
 * the beauty lives in the navigation without costing the chat-first flow.
 */
@Composable
fun DenebDrawerSheet(
    onOpenSearch: () -> Unit,
    onOpenMail: () -> Unit,
    onOpenCalendar: () -> Unit,
    onOpenPeople: () -> Unit,
    onOpenCategories: () -> Unit,
    onNavigateToSettings: () -> Unit,
    onClose: () -> Unit,
) {
    ModalDrawerSheet(drawerContainerColor = MaterialTheme.colorScheme.background) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 28.dp, vertical = 40.dp),
            verticalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            TypeMenuItem("mail") { onOpenMail(); onClose() }
            TypeMenuItem("calendar") { onOpenCalendar(); onClose() }
            TypeMenuItem("search") { onOpenSearch(); onClose() }
            TypeMenuItem("people") { onOpenPeople(); onClose() }
            TypeMenuItem("categories") { onOpenCategories(); onClose() }
            TypeMenuItem("settings") { onNavigateToSettings(); onClose() }

            val capture = LocalCaptureActions.current
            if (capture != null) {
                Spacer(Modifier.height(24.dp))
                CaptureItem("image ocr") { capture.onCaptureImage(); onClose() }
                CaptureItem("transcribe") { capture.onCaptureAudio(); onClose() }
                CaptureItem("voice") { capture.onVoiceInput(); onClose() }
            }
        }
    }
}

@Composable
private fun TypeMenuItem(label: String, onClick: () -> Unit) {
    val haptics = rememberHaptics()
    Text(
        text = label,
        fontSize = 32.sp,
        lineHeight = 42.sp,
        fontWeight = FontWeight.ExtraLight,
        letterSpacing = (-0.03).em,
        color = MaterialTheme.colorScheme.onBackground,
        modifier = Modifier
            .fillMaxWidth()
            .clickable { haptics.tap(); onClick() }
            .handCursor()
            .padding(vertical = 5.dp),
    )
}

// Capture actions are verbs, not destinations — kept small and quiet below the
// type menu so the navigation reads as pure typography.
@Composable
private fun CaptureItem(label: String, onClick: () -> Unit) {
    val haptics = rememberHaptics()
    Text(
        text = label,
        fontSize = 15.sp,
        fontWeight = FontWeight.Normal,
        letterSpacing = 0.01.em,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier
            .fillMaxWidth()
            .clickable { haptics.tap(); onClick() }
            .handCursor()
            .padding(vertical = 6.dp),
    )
}
