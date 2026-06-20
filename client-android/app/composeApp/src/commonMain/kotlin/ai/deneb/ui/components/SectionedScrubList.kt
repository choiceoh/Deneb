package ai.deneb.ui.components

import ai.deneb.ui.DenebType
import ai.deneb.ui.denebHint
import ai.deneb.ui.text.koreanTextSections
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.TransformOrigin
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import kotlin.math.abs

// A WIDE thumb-reachable touch zone (the index used to respond only at the far-right
// 22dp), bigger letters, and a STRONG fisheye: letters swell as the thumb nears,
// peaking under it. The bulge itself is the which-section indicator — no separate
// popup bubble — the Niagara "letters expand" idiom.
private val ScrubStripWidth = 64.dp
private val ScrubLetterSize = 15.sp
private val ScrubLetterEndPad = 12.dp

// Fisheye: letters within ScrubFisheyeRange (fraction of strip height) of the thumb
// swell up to 1 + ScrubFisheyeBump× (peak ≈ 2.8×), peaking at the thumb with a
// smoothstep falloff for a lens-like bulge. Applied via graphicsLayer (draw phase),
// so it never reflows or recomposes — the slots stay fixed, near letters bulge over.
private const val ScrubFisheyeRange = 0.20f
private const val ScrubFisheyeBump = 1.8f

/**
 * A Korean-first alphabetical list with a ㄱㄴㄷ/A–Z scrub index — the shared shape
 * behind both the app drawer and the 전체 연락처 browser. Groups [items] by initial
 * ([koreanTextSections]), renders header + [row] per section in one LazyColumn, and
 * overlays a wide right-edge scrub strip: drag it to jump to a section. While dragging,
 * the letters near the thumb swell (fisheye) so the user sees which section they're on
 * without a separate popup. Pure presentation; the caller supplies [label], [key], [row].
 * Monochrome, text-first — no icons.
 */
@Composable
fun <T> SectionedScrubList(
    items: List<T>,
    label: (T) -> String,
    key: (T) -> Any,
    modifier: Modifier = Modifier,
    // Preview/test only: seeds the scrubbing state so a static render shows the fisheye
    // + active highlight (which otherwise appear only mid-drag). null in production.
    previewActiveKey: String? = null,
    row: @Composable (T) -> Unit,
) {
    val sections = remember(items) { koreanTextSections(items, label) }
    // Flat LazyColumn index of each section header, so the scrub can scrollToItem.
    val headerIndex = remember(sections) {
        buildMap {
            var idx = 0
            sections.forEach {
                put(it.key, idx)
                idx += 1 + it.items.size
            }
        }
    }
    val listState = rememberLazyListState()
    val scope = rememberCoroutineScope()
    // activeKey: the section under the thumb (null = not scrubbing). activeFrac: the
    // thumb's Y as a fraction of the strip, read only inside the letters' graphicsLayer
    // (draw phase) so the per-frame fisheye never recomposes.
    var activeKey by remember { mutableStateOf(previewActiveKey) }
    var activeFrac by remember { mutableStateOf(if (previewActiveKey != null) 0.47f else 0f) }

    Box(modifier.fillMaxSize()) {
        LazyColumn(
            state = listState,
            modifier = Modifier.fillMaxSize(),
            // Reserve the visible letter column; the wider touch zone overlays the row
            // right-margin so a scrub can start from a generous area, not a hairline.
            contentPadding = PaddingValues(top = 4.dp, bottom = 24.dp, end = 30.dp),
        ) {
            sections.forEach { sec ->
                item(key = "h:${sec.key}") { SectionHeader(sec.key) }
                items(sec.items, key = { key(it) }) { item -> row(item) }
            }
        }
        // Scrub index: drag over the wide strip to jump to that section. Hidden when
        // there's only one section (nothing to scrub between).
        if (sections.size > 1) {
            ScrubIndex(
                keys = sections.map { it.key },
                activeKey = activeKey,
                // Provider, not a value: read inside the letters' graphicsLayer (draw
                // phase) so the per-frame fisheye never recomposes. -1 = not scrubbing.
                fingerFrac = { if (activeKey == null) -1f else activeFrac },
                onActive = { k, frac ->
                    activeKey = k
                    activeFrac = frac
                },
                onScrub = { k -> headerIndex[k]?.let { i -> scope.launch { listState.scrollToItem(i) } } },
                modifier = Modifier.align(Alignment.CenterEnd).fillMaxHeight().width(ScrubStripWidth).padding(vertical = 8.dp),
            )
        }
    }
}

@Composable
private fun SectionHeader(key: String) {
    Text(
        key,
        style = DenebType.sectionLabel,
        color = denebHint(),
        modifier = Modifier.padding(start = 20.dp, end = 20.dp, top = 10.dp, bottom = 2.dp),
    )
}

@Composable
private fun ScrubIndex(
    keys: List<String>,
    activeKey: String?,
    fingerFrac: () -> Float,
    onActive: (String?, Float) -> Unit,
    onScrub: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val haptics = rememberHaptics()
    Column(
        // Touch-and-slide over the WIDE column maps Y → section key (absolute
        // positioning, Niagara idiom): a single drag flies down the alphabet, firing
        // onScrub when the section changes and onActive every frame to drive the fisheye.
        modifier.pointerInput(keys) {
            awaitEachGesture {
                val down = awaitFirstDown(requireUnconsumed = false)
                // Dedup within a drag: fire scrollToItem only when the target section
                // changes, not on every pointer frame (each was a fresh coroutine).
                var lastKey: String? = null
                fun pick(y: Float) {
                    if (size.height <= 0) return
                    val frac = (y / size.height).coerceIn(0f, 1f)
                    val i = (frac * keys.size).toInt().coerceIn(0, keys.lastIndex)
                    val key = keys[i]
                    onActive(key, frac)
                    if (key != lastKey) {
                        lastKey = key
                        // SegmentTick (not FrequentTick): the more pronounced, distinctly
                        // segmented per-notch click — each initial crossing lands as its
                        // own definite tap, not a soft blur. Shared by launcher + 연락처.
                        haptics.segmentTick()
                        onScrub(key)
                    }
                }
                pick(down.position.y)
                down.consume()
                while (true) {
                    val event = awaitPointerEvent()
                    val change = event.changes.firstOrNull { it.id == down.id } ?: break
                    if (!change.pressed) break
                    pick(change.position.y)
                    change.consume()
                }
                onActive(null, 0f) // gesture ended → fisheye relaxes
            }
        },
        verticalArrangement = Arrangement.SpaceEvenly,
        horizontalAlignment = Alignment.End,
    ) {
        val n = keys.size
        keys.forEachIndexed { index, k ->
            val active = k == activeKey
            Text(
                k,
                style = DenebType.meta.copy(
                    fontSize = ScrubLetterSize,
                    fontWeight = if (active) FontWeight.Bold else FontWeight.Normal,
                ),
                // Active letter brightens to the interaction accent (a second position
                // cue alongside the fisheye swell).
                color = if (active) MaterialTheme.colorScheme.primary else denebHint(),
                modifier = Modifier
                    .padding(end = ScrubLetterEndPad)
                    .graphicsLayer {
                        // Fisheye in the draw phase (no recompose per frame): a letter
                        // swells as the thumb nears its slot, peaking under it, with a
                        // smoothstep falloff for a lens-like bulge. Origin at the right
                        // edge so glyphs grow leftward into the screen, not off-screen.
                        val f = fingerFrac()
                        if (f >= 0f && n > 0) {
                            val letterFrac = (index + 0.5f) / n
                            val t = (1f - abs(letterFrac - f) / ScrubFisheyeRange).coerceIn(0f, 1f)
                            val eased = t * t * (3f - 2f * t)
                            val s = 1f + ScrubFisheyeBump * eased
                            scaleX = s
                            scaleY = s
                            transformOrigin = TransformOrigin(1f, 0.5f)
                        }
                    },
            )
        }
    }
}
