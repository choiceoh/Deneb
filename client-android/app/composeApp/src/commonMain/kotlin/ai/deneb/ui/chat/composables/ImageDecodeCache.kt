package ai.deneb.ui.chat.composables

import androidx.compose.ui.graphics.ImageBitmap
import ai.deneb.decodeToImageBitmap
import kotlin.io.encoding.Base64
import kotlin.io.encoding.ExperimentalEncodingApi

// Decoding a base64 image attachment (base64 -> bytes -> ImageBitmap) costs several ms for a
// large form or receipt. Like the markdown parse, the per-item remember() that held the
// decoded bitmap is dropped when a message scrolls out of view, so it re-decodes on the way
// back in and lands on the scroll frame. This module-level LRU survives item disposal: each
// unique attachment decodes once and is reused. Bitmaps are large, so the cap is small.
// Single-threaded (Compose UI thread), in keeping with the project's prefer-sequential style.
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
