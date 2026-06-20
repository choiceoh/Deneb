package ai.deneb.ui.components

import ai.deneb.ui.DenebType
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.text.koreanTextSections
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
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
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import kotlin.math.abs
import kotlin.math.roundToInt

// A wide thumb-reachable touch zone (not a hairline strip — the index used to only
// respond at the far-right 22dp), bigger letters, and a large magnified bubble that
// tracks the thumb while scrubbing — the Niagara idiom (the bare letters alone read
// as too small, the GitHub-issue-263 complaint).
private val ScrubStripWidth = 44.dp
private val ScrubBubbleSize = 88.dp
private val ScrubLetterSize = 15.sp

// Fisheye: letters within ScrubFisheyeRange (fraction of strip height) of the thumb
// swell up to 1 + ScrubFisheyeBump×, peaking at the thumb — the Niagara "letters
// expand" feel. Applied via graphicsLayer (draw phase), so it never reflows or
// recomposes; the slots stay fixed and the near letters bulge over them.
private const val ScrubFisheyeRange = 0.16f
private const val ScrubFisheyeBump = 0.9f

/**
 * A Korean-first alphabetical list with a ㄱㄴㄷ/A–Z scrub index — the shared shape
 * behind both the app drawer and the 전체 연락처 browser. Groups [items] by initial
 * ([koreanTextSections]), renders header + [row] per section in one LazyColumn, and
 * overlays a right-edge scrub strip: drag it to jump to a section. While dragging, a
 * large magnified letter bubble tracks the thumb so the user sees which section
 * they're on without reading the small index (Niagara idiom). Pure presentation; the
 * caller supplies [label] (for sectioning), [key] (stable item identity), and [row].
 * Monochrome, text-first — no icons.
 */
@Composable
fun <T> SectionedScrubList(
    items: List<T>,
    label: (T) -> String,
    key: (T) -> Any,
    modifier: Modifier = Modifier,
    // Preview/test only: seeds the scrubbing state so a static render can show the
    // magnified bubble + active highlight (the bubble otherwise appears only mid-drag).
    // null in production → no bubble until a drag.
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
    // thumb's Y as a fraction of the strip, used to place the bubble — read only in
    // the offset lambda (layout phase) so per-frame tracking never recomposes.
    var activeKey by remember { mutableStateOf(previewActiveKey) }
    var activeFrac by remember { mutableStateOf(if (previewActiveKey != null) 0.47f else 0f) }

    BoxWithConstraints(modifier.fillMaxSize()) {
        val maxHpx = constraints.maxHeight.toFloat()
        LazyColumn(
            state = listState,
            modifier = Modifier.fillMaxSize(),
            // Leave room on the right for the scrub strip.
            contentPadding = PaddingValues(top = 4.dp, bottom = 24.dp, end = ScrubStripWidth),
        ) {
            sections.forEach { sec ->
                item(key = "h:${sec.key}") { SectionHeader(sec.key) }
                items(sec.items, key = { key(it) }) { item -> row(item) }
            }
        }
        // Scrub index: drag over the letters to jump to that section. Hidden when
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
            // The magnified current-letter bubble, to the left of the strip, tracking
            // the thumb. Composed only when the section changes; the Y follows the
            // finger purely in the layout phase (offset lambda reads activeFrac).
            activeKey?.let { k ->
                ScrubBubble(
                    letter = k,
                    modifier = Modifier
                        .align(Alignment.TopEnd)
                        .offset {
                            val bubblePx = ScrubBubbleSize.toPx()
                            val gapPx = (ScrubStripWidth + 12.dp).toPx()
                            val y = (activeFrac * maxHpx - bubblePx / 2f)
                                .coerceIn(0f, (maxHpx - bubblePx).coerceAtLeast(0f))
                            IntOffset(x = -gapPx.roundToInt(), y = y.roundToInt())
                        }
                        .size(ScrubBubbleSize),
                )
            }
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

/** The big letter shown while scrubbing — a soft surface disc (shadow + hairline for
 *  separation over the AMOLED list) with the initial in the cool interaction accent. */
@Composable
private fun ScrubBubble(letter: String, modifier: Modifier = Modifier) {
    Surface(
        modifier = modifier,
        shape = CircleShape,
        color = MaterialTheme.colorScheme.surface,
        shadowElevation = 8.dp,
        border = BorderStroke(1.dp, denebHairline()),
    ) {
        Box(contentAlignment = Alignment.Center) {
            Text(
                letter,
                style = DenebType.viewTitle.copy(fontSize = 46.sp, fontWeight = FontWeight.Medium),
                color = MaterialTheme.colorScheme.primary,
                textAlign = TextAlign.Center,
            )
        }
    }
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
        // Touch-and-slide over the column maps Y → section key (absolute positioning,
        // Niagara idiom): a single drag flies down the alphabet, firing onScrub when
        // the section changes and onActive every frame so the bubble tracks the thumb.
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
                        // A detent tick each time the drag crosses into a new initial —
                        // the scrub feels like notches under the thumb (Niagara/iOS index
                        // idiom). FrequentTick stays crisp under the rapid firing of a
                        // fast flick instead of buzzing. Shared by launcher + 전체 연락처.
                        haptics.segmentFrequentTick()
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
                onActive(null, 0f) // gesture ended → hide the bubble
            }
        },
        verticalArrangement = Arrangement.SpaceEvenly,
        horizontalAlignment = Alignment.CenterHorizontally,
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
                // Active letter brightens to the interaction accent so the strip itself
                // shows position even before the eye finds the bubble.
                color = if (active) MaterialTheme.colorScheme.primary else denebHint(),
                modifier = Modifier.graphicsLayer {
                    // Fisheye, evaluated in the draw phase (no recompose per frame): a
                    // letter swells as the thumb nears its slot, peaking at the thumb.
                    // Origin at the right edge so glyphs grow leftward into the screen
                    // (the "letters move alongside the list" feel), not off-screen right.
                    val f = fingerFrac()
                    if (f >= 0f && n > 0) {
                        val letterFrac = (index + 0.5f) / n
                        val t = (1f - abs(letterFrac - f) / ScrubFisheyeRange).coerceIn(0f, 1f)
                        val s = 1f + ScrubFisheyeBump * t
                        scaleX = s
                        scaleY = s
                        transformOrigin = TransformOrigin(1f, 0.5f)
                    }
                },
            )
        }
    }
}
