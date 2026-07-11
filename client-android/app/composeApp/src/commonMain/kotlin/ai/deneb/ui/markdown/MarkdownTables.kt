package ai.deneb.ui.markdown

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import kotlin.math.sqrt

@Composable
internal fun TableBlock(block: Table) {
    val numCols = maxOf(block.headers.size, block.rows.maxOfOrNull { it.size } ?: 0)
    if (numCols == 0) return
    // Content-derived natural width per column (CJK-aware: a full-width Hangul/Han
    // glyph is ~2 latin chars). The fit-vs-scroll choice is driven by whether that
    // natural width fits the viewport, NOT a fixed column count — so a 3-column table
    // with long Korean cells scrolls just like a 7-column one, instead of the weight
    // layout crushing each cell to a glyph per line.
    val colWidths = remember(block, numCols) { naturalColWidths(block, numCols) }
    BoxWithConstraints(Modifier.fillMaxWidth()) {
        // Fit-vs-scroll: horizontal scroll is a last resort on a phone —
        // readers lose the right columns entirely unless they think to drag
        // (the 2026-07-07 markdown-vs-card comparison showed a 4-column
        // delivery table hiding its 납기 column). Tables up to 4 columns with
        // a MODERATE overshoot (≤1.7x viewport) wrap cells via FittedTable
        // instead; sqrt weights keep narrow numeric columns readable. Truly
        // wide tables (5+ columns or extreme overshoot) still scroll.
        val overshoot = colWidths.sum() / maxWidth.value.coerceAtLeast(1f)
        if (overshoot <= 1f || (numCols <= 4 && overshoot <= 1.7f)) {
            FittedTable(block, numCols)
        } else {
            WideTable(block, numCols, colWidths)
        }
    }
}

@Composable
private fun FittedTable(block: Table, numCols: Int) {
    // Weight each column by its widest cell so a short key column stops wasting
    // half the width on a key/value table (the common analysis shape). sqrt
    // compresses the extremes, so the long value column gets the room without
    // crushing the narrow key column to nothing.
    val weights = remember(block) {
        FloatArray(numCols) { i ->
            var maxLen = inlineDisplayUnits(block.headers.getOrNull(i) ?: emptyList())
            for (row in block.rows) {
                maxLen = maxOf(maxLen, inlineDisplayUnits(row.getOrNull(i) ?: emptyList()))
            }
            sqrt(maxLen.coerceAtLeast(1).toFloat()).coerceAtLeast(1f)
        }
    }
    val rowDivider = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.35f)
    Column(Modifier.padding(vertical = 4.dp)) {
        if (block.headers.any { it.isNotEmpty() }) {
            Row {
                block.headers.forEachIndexed { i, cell ->
                    InlineContent(
                        inlines = cell,
                        style = markdownBodyStyle.copy(fontWeight = FontWeight.Bold),
                        textAlign = alignTextFor(block.alignments.getOrNull(i)),
                        modifier = Modifier.weight(weights.getOrElse(i) { 1f })
                            .padding(horizontal = 8.dp, vertical = 6.dp),
                    )
                }
            }
            HorizontalDivider()
        }
        block.rows.forEachIndexed { rowIdx, row ->
            if (rowIdx > 0) HorizontalDivider(thickness = 1.dp, color = rowDivider)
            Row {
                row.forEachIndexed { i, cell ->
                    InlineContent(
                        inlines = cell,
                        style = markdownBodyStyle,
                        textAlign = alignTextFor(block.alignments.getOrNull(i)),
                        modifier = Modifier.weight(weights.getOrElse(i) { 1f })
                            .padding(horizontal = 8.dp, vertical = 6.dp),
                    )
                }
            }
        }
    }
}

// Wide table: fixed content-derived column widths under one horizontal scroll, so the
// header and every row stay aligned and each cell remains readable. Long cells wrap
// within their clamped column width instead of stretching the table indefinitely.
// [colWidths] is the CJK-aware natural width per column computed by the caller.
@Composable
private fun WideTable(block: Table, numCols: Int, colWidths: IntArray) {
    val totalWidth = colWidths.sum().dp
    val rowDivider = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.35f)
    val scroll = rememberScrollState()
    Column(
        Modifier
            .padding(vertical = 4.dp)
            .horizontalScroll(scroll),
    ) {
        if (block.headers.any { it.isNotEmpty() }) {
            Row {
                block.headers.forEachIndexed { i, cell ->
                    InlineContent(
                        inlines = cell,
                        style = markdownBodyStyle.copy(fontWeight = FontWeight.Bold),
                        textAlign = alignTextFor(block.alignments.getOrNull(i)),
                        modifier = Modifier.width(colWidths.getOrElse(i) { 100 }.dp)
                            .padding(horizontal = 8.dp, vertical = 6.dp),
                    )
                }
            }
            // Dividers must span the content width, not the viewport, inside the scroller.
            HorizontalDivider(Modifier.width(totalWidth))
        }
        block.rows.forEachIndexed { rowIdx, row ->
            if (rowIdx > 0) {
                HorizontalDivider(Modifier.width(totalWidth), thickness = 1.dp, color = rowDivider)
            }
            Row {
                row.forEachIndexed { i, cell ->
                    InlineContent(
                        inlines = cell,
                        style = markdownBodyStyle,
                        textAlign = alignTextFor(block.alignments.getOrNull(i)),
                        modifier = Modifier.width(colWidths.getOrElse(i) { 100 }.dp)
                            .padding(horizontal = 8.dp, vertical = 6.dp),
                    )
                }
            }
        }
    }
}

// Per-column natural width in dp, sized by the widest cell's CJK-aware display width
// (~7dp per display unit at the body size; a full-width Hangul/Han glyph is 2 units ≈
// 14dp, latin ≈ 7dp), plus cell padding. Clamped so a short key column stays legible
// and one verbose cell doesn't explode its column. Shared by the fit-vs-scroll
// decision and WideTable so both agree on the table's natural width.
private fun naturalColWidths(block: Table, numCols: Int): IntArray = IntArray(numCols) { i ->
    var units = inlineDisplayUnits(block.headers.getOrNull(i) ?: emptyList())
    for (row in block.rows) {
        units = maxOf(units, inlineDisplayUnits(row.getOrNull(i) ?: emptyList()))
    }
    (units * 7 + 16).coerceIn(72, 240)
}

// inlineDisplayUnits is the cell's rendered width in "display units": every char is 1
// unit except East-Asian-wide ones (Hangul/Han/kana/full-width), which are 2 — so a
// Korean column is sized for its real on-screen width, not its character count.
private fun inlineDisplayUnits(inlines: List<InlineNode>): Int = inlines.sumOf { node ->
    when (node) {
        is Text -> displayUnits(node.value)
        is InlineCode -> displayUnits(node.code)
        is InlineMath -> displayUnits(node.latex)
        is Emphasis -> inlineDisplayUnits(node.children)
        is Strong -> inlineDisplayUnits(node.children)
        is Strike -> inlineDisplayUnits(node.children)
        is Underline -> inlineDisplayUnits(node.children)
        is Highlight -> inlineDisplayUnits(node.children)
        is Superscript -> inlineDisplayUnits(node.children)
        is Subscript -> inlineDisplayUnits(node.children)
        is Link -> inlineDisplayUnits(node.children)
        is Image -> displayUnits(node.alt)
        else -> 0
    }
}

private fun displayUnits(s: String): Int = s.sumOf { if (isEastAsianWide(it)) 2 else 1 }

// East-Asian "wide" approximation: Hangul (jamo + syllables), CJK ideographs and
// radicals through Yi (covers kana 3040–30FF and CJK symbols 3000–303F), CJK compat,
// and full-width forms. Good enough to size columns; not a full Unicode width table.
private fun isEastAsianWide(c: Char): Boolean {
    val u = c.code
    return u in 0x1100..0x115F ||
        u in 0x2E80..0xA4CF ||
        u in 0xAC00..0xD7A3 ||
        u in 0xF900..0xFAFF ||
        u in 0xFE30..0xFE4F ||
        u in 0xFF00..0xFF60 ||
        u in 0xFFE0..0xFFE6
}

private fun alignTextFor(align: ColumnAlign?): TextAlign = when (align) {
    ColumnAlign.LEFT -> TextAlign.Start
    ColumnAlign.CENTER -> TextAlign.Center
    ColumnAlign.RIGHT -> TextAlign.End
    else -> TextAlign.Unspecified
}
