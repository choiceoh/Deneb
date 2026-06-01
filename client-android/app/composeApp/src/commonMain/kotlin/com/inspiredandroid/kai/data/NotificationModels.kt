package com.inspiredandroid.kai.data

import androidx.compose.runtime.Immutable
import kotlinx.serialization.Serializable

@Immutable
@Serializable
data class NotificationRecord(
    val id: String,
    val packageName: String,
    val appLabel: String,
    val title: String,
    val text: String,
    val subtext: String = "",
    val postedAtEpochMs: Long,
    val isOngoing: Boolean = false,
    val category: String = "",
    val preview: String = text.take(PREVIEW_CHARS),
    // True when the notification carried a BigPictureStyle image. The bytes
    // themselves live in NotificationStore's in-memory image cache (keyed by
    // id), never serialized here — so a manual send can forward the picture
    // through the OCR capture path. Survives process death as `true` even after
    // the cached bytes are gone; the send path falls back to text-only then.
    val hasImage: Boolean = false,
) {
    companion object {
        const val PREVIEW_CHARS = 200
    }
}

@Serializable
data class NotificationSyncState(
    val listenerBound: Boolean = false,
    val lastBoundEpochMs: Long = 0L,
    val lastError: String? = null,
)
