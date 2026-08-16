package skytycoon.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import skytycoon.ui.InkPanel
import skytycoon.ui.InkPanelHigh
import skytycoon.ui.TextLow
import skytycoon.ui.TextMid

@Composable
fun Panel(
    modifier: Modifier = Modifier,
    title: String? = null,
    trailing: @Composable (() -> Unit)? = null,
    content: @Composable () -> Unit,
) {
    Column(
        modifier
            .clip(RoundedCornerShape(14.dp))
            .background(InkPanel)
            .border(1.dp, Color(0xFF243043), RoundedCornerShape(14.dp))
            .padding(14.dp),
    ) {
        if (title != null) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    title,
                    color = TextMid,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.SemiBold,
                    letterSpacing = 1.sp,
                    modifier = Modifier.weight(1f),
                )
                trailing?.invoke()
            }
            Spacer(Modifier.height(10.dp))
        }
        content()
    }
}

@Composable
fun StatTile(label: String, value: String, accent: Color = MaterialTheme.colorScheme.onSurface, sub: String? = null) {
    Column(Modifier.padding(end = 18.dp)) {
        Text(label, color = TextLow, fontSize = 11.sp)
        Spacer(Modifier.height(3.dp))
        Text(value, color = accent, fontSize = 19.sp, fontWeight = FontWeight.Bold)
        if (sub != null) {
            Text(sub, color = TextLow, fontSize = 11.sp)
        }
    }
}

@Composable
fun KeyValue(key: String, value: String, valueColor: Color = MaterialTheme.colorScheme.onSurface) {
    Row(Modifier.fillMaxWidth().padding(vertical = 3.dp)) {
        Text(key, color = TextMid, fontSize = 13.sp, modifier = Modifier.weight(1f))
        Text(value, color = valueColor, fontSize = 13.sp, fontWeight = FontWeight.Medium)
    }
}

/** 0..1 비율 막대. 탑승률·점유율 표시에 쓴다. */
@Composable
fun RatioBar(ratio: Double, color: Color, modifier: Modifier = Modifier, height: Int = 6) {
    Box(
        modifier
            .height(height.dp)
            .clip(RoundedCornerShape(3.dp))
            .background(InkPanelHigh),
    ) {
        Box(
            Modifier
                .fillMaxWidth(ratio.coerceIn(0.0, 1.0).toFloat())
                .height(height.dp)
                .clip(RoundedCornerShape(3.dp))
                .background(color),
        )
    }
}

@Composable
fun Dot(color: Color, size: Int = 8) {
    Box(Modifier.size(size.dp).clip(CircleShape).background(color))
}

@Composable
fun Chip(
    text: String,
    selected: Boolean = false,
    accent: Color = MaterialTheme.colorScheme.primary,
    onClick: (() -> Unit)? = null,
) {
    Box(
        Modifier
            .clip(RoundedCornerShape(8.dp))
            .background(if (selected) accent.copy(alpha = 0.22f) else InkPanelHigh)
            .then(if (onClick != null) Modifier.clickable { onClick() } else Modifier)
            .padding(horizontal = 10.dp, vertical = 6.dp),
    ) {
        Text(
            text,
            color = if (selected) accent else TextMid,
            fontSize = 12.sp,
            fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal,
        )
    }
}

@Composable
fun EmptyHint(text: String) {
    Box(Modifier.fillMaxWidth().padding(vertical = 28.dp), contentAlignment = Alignment.Center) {
        Text(text, color = TextLow, fontSize = 13.sp)
    }
}

@Composable
fun SectionRow(content: @Composable () -> Unit) {
    Row(
        Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) { content() }
}

@Composable
fun VSpace(h: Int) = Spacer(Modifier.height(h.dp))

@Composable
fun HSpace(w: Int) = Spacer(Modifier.width(w.dp))
