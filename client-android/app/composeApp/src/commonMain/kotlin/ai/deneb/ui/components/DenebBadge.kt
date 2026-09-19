package ai.deneb.ui.components

import ai.deneb.ui.DenebType
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp

/**
 * A tag: a small tinted word that names a state or category (생성 · 가동 중 · 완료 ·
 * 양식 · ▲ 2.1%) at meta size with 4dp corners and tight padding.
 *
 * The COLOUR is the call site's job — a category tint or a semantic one (ADR 0008
 * jobs 2 and 3) — the SHAPE is not: six hand-rolled badges had drifted to three
 * radii (4 / 6 / 999dp), and the fully round ones read as buttons or pills next to
 * the real controls. One shape, one size, so a tag is a tag everywhere.
 */
@Composable
fun DenebBadge(
    text: String,
    color: Color,
    containerColor: Color,
    modifier: Modifier = Modifier,
) {
    Text(
        text = text,
        style = DenebType.meta,
        color = color,
        maxLines = 1,
        modifier = modifier
            .background(containerColor, RoundedCornerShape(4.dp))
            .padding(horizontal = 6.dp, vertical = 1.dp),
    )
}
