package ai.deneb.ui.chat.composables

import ai.deneb.decodeToImageBitmap
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.ui.graphics.ImageBitmap
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlin.io.encoding.Base64
import kotlin.io.encoding.ExperimentalEncodingApi

// Decoding a base64 image attachment (base64 -> bytes -> ImageBitmap) costs several ms for a
// large form or receipt. Like the markdown parse, the per-item remember() that held the
// decoded bitmap is dropped when a message scrolls out of view, so it re-decodes on the way
// back in and lands on the scroll frame. This module-level LRU survives item disposal: each
// unique attachment decodes once and is reused. Bitmaps are large, so the cap is small.
// Cache access is UI-thread only, in keeping with the project's prefer-sequential style.
// rememberDecodedImage keeps that invariant while moving the decode itself onto a spare
// core (Dispatchers.Default), off the scroll frame.
private const val IMAGE_DECODE_CACHE_MAX = 24

// A count alone does not bound this cache: entries are decoded to at most 2048px
// on the long side, so 24 of them is up to ~400MB of ARGB against a ~256MB heap.
// The budget is what actually bounds it; the count cap just keeps the map short
// when the images are small.
private const val IMAGE_DECODE_CACHE_MAX_BYTES = 48L * 1024 * 1024

private val imageDecodeCache = LinkedHashMap<String, ImageBitmap>()
private var imageDecodeCacheBytes = 0L

// ARGB_8888 is 4 bytes per pixel on every target; close enough to charge the budget.
private fun ImageBitmap.approximateBytes(): Long = width.toLong() * height.toLong() * 4L

@OptIn(ExperimentalEncodingApi::class)
fun decodeBase64ImageCached(data: String): ImageBitmap? {
    imageDecodeCache.remove(data)?.let { cached ->
        imageDecodeCache[data] = cached // re-insert at the tail = most recently used
        return cached
    }
    val bitmap = runCatching { decodeToImageBitmap(Base64.decode(data)) }.getOrNull() ?: return null
    putImageDecoded(data, bitmap)
    return bitmap
}

// putImageDecoded stores an already-decoded [bitmap] (decoded off the UI thread by
// rememberDecodedImage) without decoding. UI-thread only, like the rest of this cache.
private fun putImageDecoded(data: String, bitmap: ImageBitmap) {
    if (imageDecodeCache.containsKey(data)) return
    imageDecodeCache[data] = bitmap
    imageDecodeCacheBytes += bitmap.approximateBytes()
    // Always keep the newest entry even when it alone blows the budget — a single
    // huge attachment should still display rather than evict itself on insert.
    while (imageDecodeCache.size > 1 &&
        (imageDecodeCache.size > IMAGE_DECODE_CACHE_MAX || imageDecodeCacheBytes > IMAGE_DECODE_CACHE_MAX_BYTES)
    ) {
        val oldest = imageDecodeCache.keys.first()
        imageDecodeCache.remove(oldest)?.let { imageDecodeCacheBytes -= it.approximateBytes() }
    }
}

/**
 * Drops every cached bitmap. Wired to the platform's low-memory callback: when the
 * system says it is about to start killing processes, several hundred megabytes of
 * decoded attachments is the cheapest thing this app can give back — they re-decode
 * from the transcript on demand.
 */
fun clearImageDecodeCache() {
    imageDecodeCache.clear()
    imageDecodeCacheBytes = 0L
}

// rememberDecodedImage returns the bitmap for a base64 [data] using the device's spare
// cores instead of the UI thread: a cache hit returns instantly; a miss decodes on
// Dispatchers.Default (off the scroll frame) and caches it back on the UI thread, so it's a
// hit ever after. Shows null until the first decode lands — unlike text reflow, a one-frame
// image fade-in reads fine. Replaces the old per-call `remember { decode }` that decoded on
// the composition (UI) thread (and, on the user-message side, without caching at all).
@OptIn(ExperimentalEncodingApi::class)
@Composable
fun rememberDecodedImage(data: String): ImageBitmap? {
    // Cache hit (e.g. scrolled back into view) → instant, no async hop, no decode.
    val cached = remember(data) { if (imageDecodeCache.containsKey(data)) decodeBase64ImageCached(data) else null }
    if (cached != null) return cached
    // Miss → decode on a background core, then cache on the caller (UI) context.
    val decoded by produceState<ImageBitmap?>(initialValue = null, data) {
        val bitmap = withContext(Dispatchers.Default) {
            runCatching { decodeToImageBitmap(Base64.decode(data)) }.getOrNull()
        }
        if (bitmap != null) putImageDecoded(data, bitmap)
        value = bitmap
    }
    return decoded
}

// The original encoded bytes behind a base64 attachment, for save/share (the full-
// screen viewer's export actions). Decoded on demand (at click, not per frame), so
// no caching here. Returns null on malformed input.
@OptIn(ExperimentalEncodingApi::class)
fun decodeBase64BytesOrNull(data: String): ByteArray? = runCatching { Base64.decode(data) }.getOrNull()
