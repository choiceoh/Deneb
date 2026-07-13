package ai.deneb.ui.markdown

import ai.deneb.ui.text.denebPhraseLineBreak
import ai.deneb.ui.text.displayUnits
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import kotlin.math.sqrt

@Composable
internal fun TableBlock(block: Table) {
    val numCols = maxOf(block.headers.size, block.rows.maxOfOrNull { it.size } ?: 0)
    if (numCols == 0) return
    // Dense tables (3+ columns) speak the data voice one rung down so narrow
    // columns fit whole phrases per line — the production-corpus lesson
    // shared with the deneb-ui table (2026-07-12).
    val dense = numCols >= 3
    // Content-derived natural width per column (CJK-aware: a full-width Hangul/Han
    // glyph is ~2 latin chars). The fit-vs-scroll choice is driven by whether that
    // natural width fits the viewport, NOT a fixed column count — so a 3-column table
    // with long Korean cells scrolls just like a 7-column one, instead of the weight
    // layout crushing each cell to a glyph per line.
    val maxUnits = remember(block, numCols) { maxColUnits(block, numCols) }
    // Numbering-style columns ("#", "1".."99") take their intrinsic width instead
    // of a weight share — the sqrt weight floor otherwise hands a single-digit
    // column ~20% of a phone-width table (2026-07-13 production screenshot: the
    // '#' column of a 3-column briefing table ate a fifth of the row).
    val tiny = remember(block, numCols) { tinyColumns(maxUnits) }
    val colWidths = remember(block, numCols) { naturalColWidths(maxUnits, tiny, dense) }
    // Auto-numeric columns: models rarely write ':---:' alignment markers, so
    // a column whose every cell starts with a digit right-aligns with tabular
    // figures — unless the author aligned it explicitly (explicit wins).
    val numericColumn = remember(block) {
        BooleanArray(numCols) { i ->
            val explicit = block.alignments.getOrNull(i)
            if (explicit != null && explicit != ColumnAlign.NONE) return@BooleanArray false
            val cells = block.rows.mapNotNull { row ->
                row.getOrNull(i)?.let(::inlinesToText)?.trim()?.takeIf(String::isNotEmpty)
            }
            cells.isNotEmpty() && cells.all { cell ->
                val bare = cell.trimStart('*', '_', '~', '`', ' ')
                bare.isNotEmpty() &&
                    (bare.first().isDigit() || ((bare.first() == '-' || bare.first() == '−') && bare.length > 1 && bare[1].isDigit()))
            }
        }
    }
    val voice = tableVoice(block, numericColumn, dense)
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
            FittedTable(block, numCols, tiny, colWidths, voice)
        } else {
            WideTable(block, numCols, colWidths, voice)
        }
    }
}

// The table's type voice + per-column style/alignment, shared by both layouts
// so the fitted and scrolling variants read identically.
private class TableVoice(
    val cell: TextStyle,
    val numericCell: TextStyle,
    val header: TextStyle,
    private val block: Table,
    private val numeric: BooleanArray,
) {
    fun style(col: Int): TextStyle = if (numeric.getOrElse(col) { false }) numericCell else cell

    fun align(col: Int): TextAlign {
        val explicit = alignTextFor(block.alignments.getOrNull(col))
        if (explicit != TextAlign.Unspecified) return explicit
        return if (numeric.getOrElse(col) { false }) TextAlign.End else TextAlign.Unspecified
    }
}

@Composable
private fun tableVoice(block: Table, numeric: BooleanArray, dense: Boolean): TableVoice {
    // Cells speak a data voice one rung under prose (two when dense); phrase
    // wrapping keeps Korean 어절 whole ("카/운트다운" never splits mid-word);
    // numeric columns take tabular figures so digits align down the column.
    val base = (if (dense) MaterialTheme.typography.bodySmall else MaterialTheme.typography.bodyMedium)
        .copy(lineBreak = denebPhraseLineBreak())
    return TableVoice(
        cell = base,
        numericCell = base.copy(fontFeatureSettings = "tnum"),
        header = base.copy(fontWeight = FontWeight.Bold),
        block = block,
        numeric = numeric,
    )
}

@Composable
private fun FittedTable(block: Table, numCols: Int, tiny: BooleanArray, colWidths: IntArray, voice: TableVoice) {
    // Column shares follow the AVERAGE cell width (CJK-aware display units) —
    // an average reads truer than the max, where one long outlier cell starves
    // every other column (2026-07-12 production-corpus lesson). The header
    // participates as a floor so a short column keeps its label legible; sqrt
    // squashes the spread so the ratio stays civil. Tiny numbering columns sit
    // at their fixed natural width (colWidths) outside the weight split.
    val weights = remember(block) {
        FloatArray(numCols) { i ->
            val cells = block.rows.mapNotNull { row -> row.getOrNull(i)?.let(::inlineDisplayUnits)?.takeIf { it > 0 } }
            val avg = if (cells.isEmpty()) 0 else cells.sum() / cells.size
            val header = inlineDisplayUnits(block.headers.getOrNull(i) ?: emptyList())
            sqrt(maxOf(avg, header).coerceIn(6, 44).toFloat())
        }
    }
    val rowDivider = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.35f)
    Column(Modifier.padding(vertical = 4.dp)) {
        if (block.headers.any { it.isNotEmpty() }) {
            Row {
                block.headers.forEachIndexed { i, cell ->
                    InlineContent(
                        inlines = cell,
                        style = voice.header,
                        textAlign = voice.align(i),
                        modifier = fittedCellWidth(i, tiny, colWidths, weights)
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
                        style = voice.style(i),
                        textAlign = voice.align(i),
                        modifier = fittedCellWidth(i, tiny, colWidths, weights)
                            .padding(horizontal = 8.dp, vertical = 6.dp),
                    )
                }
            }
        }
    }
}

private fun RowScope.fittedCellWidth(i: Int, tiny: BooleanArray, colWidths: IntArray, weights: FloatArray): Modifier = if (tiny.getOrElse(i) { false }) {
    Modifier.width(colWidths.getOrElse(i) { 100 }.dp)
} else {
    Modifier.weight(weights.getOrElse(i) { 1f })
}

// Wide table: fixed content-derived column widths under one horizontal scroll, so the
// header and every row stay aligned and each cell remains readable. Long cells wrap
// within their clamped column width instead of stretching the table indefinitely.
// [colWidths] is the CJK-aware natural width per column computed by the caller.
@Composable
private fun WideTable(block: Table, numCols: Int, colWidths: IntArray, voice: TableVoice) {
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
                        style = voice.header,
                        textAlign = voice.align(i),
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
                        style = voice.style(i),
                        textAlign = voice.align(i),
                        modifier = Modifier.width(colWidths.getOrElse(i) { 100 }.dp)
                            .padding(horizontal = 8.dp, vertical = 6.dp),
                    )
                }
            }
        }
    }
}

// Widest content (header included) per column, in CJK-aware display units.
private fun maxColUnits(block: Table, numCols: Int): IntArray = IntArray(numCols) { i ->
    var units = inlineDisplayUnits(block.headers.getOrNull(i) ?: emptyList())
    for (row in block.rows) {
        units = maxOf(units, inlineDisplayUnits(row.getOrNull(i) ?: emptyList()))
    }
    units
}

// A column is "tiny" when its widest cell AND header fit in a few display units —
// numbering ("#", "1".."99", "10."), ranks, single marks. Tiny columns are sized
// to content instead of joining the weight split or the 72dp scroll floor.
private const val TINY_COL_MAX_UNITS = 3

// Intrinsic dp per display unit for tiny columns — deliberately more generous than
// the 6-7dp prose estimate (tabular digits and '#' run wide, and a wrapped digit
// would be far worse than a few spare dp).
private const val TINY_UNIT_DP = 9

// Tiny only when some other column can absorb the freed width — an all-tiny table
// keeps the weight layout so it still spans the viewport.
private fun tinyColumns(maxUnits: IntArray): BooleanArray {
    val tiny = BooleanArray(maxUnits.size) { maxUnits[it] in 1..TINY_COL_MAX_UNITS }
    return if (tiny.all { it }) BooleanArray(maxUnits.size) else tiny
}

// Per-column natural width in dp, sized by the widest cell's CJK-aware display width
// (~7dp per display unit at the body size, ~6dp at the dense size; a full-width
// Hangul/Han glyph is 2 units, latin 1), plus cell padding. Clamped so a short key
// column stays legible and one verbose cell doesn't explode its column — except tiny
// numbering columns, which sit at intrinsic width. Shared by the fit-vs-scroll
// decision, FittedTable (tiny columns), and WideTable so all agree on natural width.
private fun naturalColWidths(maxUnits: IntArray, tiny: BooleanArray, dense: Boolean): IntArray {
    val unit = if (dense) 6 else 7
    return IntArray(maxUnits.size) { i ->
        if (tiny[i]) {
            maxUnits[i] * TINY_UNIT_DP + 16
        } else {
            (maxUnits[i] * unit + 16).coerceIn(72, 240)
        }
    }
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

private fun alignTextFor(align: ColumnAlign?): TextAlign = when (align) {
    ColumnAlign.LEFT -> TextAlign.Start
    ColumnAlign.CENTER -> TextAlign.Center
    ColumnAlign.RIGHT -> TextAlign.End
    else -> TextAlign.Unspecified
}
