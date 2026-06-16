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

private val imageDecodeCache = LinkedHashMap<String, ImageBitmap>()

@OptIn(ExperimentalEncodingApi::class)
fun decodeBase64ImageCached(data: String): ImageBitmap? {
    imageDecodeCache.remove(data)?.let { cached ->
        imageDecodeCache[data] = cached // re-insert at the tail = most recently used
        return cached
    }
    val bitmap = runCatching { decodeToImageBitmap(Base64.decode(data)) }.getOrNull() ?: return null
    imageDecodeCache[data] = bitmap
    while (imageDecodeCache.size > IMAGE_DECODE_CACHE_MAX) {
        imageDecodeCache.remove(imageDecodeCache.keys.first())
    }
    return bitmap
}

// putImageDecoded stores an already-decoded [bitmap] (decoded off the UI thread by
// rememberDecodedImage) without decoding. UI-thread only, like the rest of this cache.
private fun putImageDecoded(data: String, bitmap: ImageBitmap) {
    if (imageDecodeCache.containsKey(data)) return
    imageDecodeCache[data] = bitmap
    while (imageDecodeCache.size > IMAGE_DECODE_CACHE_MAX) {
        imageDecodeCache.remove(imageDecodeCache.keys.first())
    }
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
