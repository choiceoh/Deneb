package skytycoon.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import skytycoon.core.model.NewsKind
import skytycoon.ui.Amber
import skytycoon.ui.Coral
import skytycoon.ui.GameViewModel
import skytycoon.ui.InkPanelHigh
import skytycoon.ui.Mint
import skytycoon.ui.Sky
import skytycoon.ui.TextHigh
import skytycoon.ui.TextLow
import skytycoon.ui.TextMid
import skytycoon.ui.components.Dot
import skytycoon.ui.components.EmptyHint

@Composable
fun NewsScreen(vm: GameViewModel) {
    val s = vm.game
    val items = s.news.reversed()
    if (items.isEmpty()) {
        EmptyHint("아직 소식이 없습니다.")
        return
    }
    LazyColumn(
        Modifier.fillMaxSize().padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        itemsIndexed(items) { _, item ->
            val year = s.startYear + item.turn / 4
            val quarter = item.turn % 4 + 1
            Row(
                Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(10.dp))
                    .background(InkPanelHigh)
                    .padding(12.dp),
            ) {
                Column(Modifier.width(64.dp)) {
                    Text("${year}", color = TextMid, fontSize = 12.sp, fontWeight = FontWeight.Bold)
                    Text("${quarter}분기", color = TextLow, fontSize = 10.sp)
                }
                Box(Modifier.width(8.dp))
                Dot(kindColor(item.kind), 8)
                Box(Modifier.width(10.dp))
                Column(Modifier.weight(1f)) {
                    Text(item.headline, color = TextHigh, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
                    if (item.body.isNotBlank()) {
                        Text(item.body, color = TextMid, fontSize = 11.sp, lineHeight = 16.sp)
                    }
                }
            }
        }
    }
}

private fun kindColor(kind: NewsKind): Color = when (kind) {
    NewsKind.WORLD -> Coral
    NewsKind.ECONOMY -> Amber
    NewsKind.RIVAL -> Sky
    NewsKind.PLAYER -> Mint
    NewsKind.ACCIDENT -> Coral
    NewsKind.MARKET -> Sky
    NewsKind.MILESTONE -> Amber
}
