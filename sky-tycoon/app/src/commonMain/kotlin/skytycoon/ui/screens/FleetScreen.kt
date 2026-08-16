package skytycoon.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import skytycoon.core.data.AircraftCatalog
import skytycoon.core.data.Cities
import skytycoon.core.sim.Actions
import skytycoon.core.sim.Command
import skytycoon.ui.Amber
import skytycoon.ui.Coral
import skytycoon.ui.GameViewModel
import skytycoon.ui.InkPanelHigh
import skytycoon.ui.Mint
import skytycoon.ui.Sky
import skytycoon.ui.TextHigh
import skytycoon.ui.TextLow
import skytycoon.ui.TextMid
import skytycoon.ui.components.Chip
import skytycoon.ui.components.EmptyHint
import skytycoon.ui.components.Panel
import skytycoon.ui.components.VSpace
import skytycoon.ui.decimals
import skytycoon.ui.grouped
import skytycoon.ui.km
import skytycoon.ui.moneyShort

@Composable
fun FleetScreen(vm: GameViewModel, wide: Boolean) {
    val owned = @Composable { m: Modifier -> OwnedFleet(vm, m) }
    val market = @Composable { m: Modifier -> AircraftMarket(vm, m) }

    if (wide) {
        Row(Modifier.fillMaxSize()) {
            owned(Modifier.weight(1f).fillMaxHeight().padding(12.dp))
            market(Modifier.width(430.dp).fillMaxHeight().padding(12.dp))
        }
    } else {
        Column(Modifier.fillMaxSize()) {
            owned(Modifier.fillMaxWidth().weight(1f).padding(12.dp))
            market(Modifier.fillMaxWidth().weight(1f).padding(12.dp))
        }
    }
}

@Composable
private fun OwnedFleet(vm: GameViewModel, modifier: Modifier) {
    val s = vm.game
    val planes = s.planesOf(s.playerId).sortedWith(compareBy({ it.routeId != null }, { it.typeId }))
    val pending = s.orders.filter { it.airlineId == s.playerId }

    Column(modifier) {
        Panel(title = "보유 기재 ${planes.size}대") {
            if (pending.isNotEmpty()) {
                for (o in pending) {
                    Text(
                        "발주 중 · ${AircraftCatalog[o.typeId].name} ${o.count}대 " +
                            "(${(o.deliverTurn - s.turn).coerceAtLeast(0)}분기 뒤 인도)",
                        color = Amber,
                        fontSize = 12.sp,
                    )
                }
                VSpace(8)
            }
            if (planes.isEmpty()) EmptyHint("보유한 기재가 없습니다.")
        }
        VSpace(10)
        LazyColumn(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            items(planes, key = { it.id }) { plane ->
                val t = AircraftCatalog[plane.typeId]
                val route = plane.routeId?.let { id -> s.routes.firstOrNull { it.id == id } }
                Row(
                    Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(10.dp))
                        .background(InkPanelHigh)
                        .padding(10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(Modifier.weight(1f)) {
                        Text(t.name, color = TextHigh, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
                        Text(
                            "${t.seats}석 · ${km(t.range.toDouble())} · 기령 ${plane.ageQuarters / 4}년 ${plane.ageQuarters % 4}분기",
                            color = TextMid,
                            fontSize = 11.sp,
                        )
                        Text(
                            route?.let { "${Cities.name(it.from)} – ${Cities.name(it.to)}" } ?: "유휴",
                            color = if (route == null) Coral else Mint,
                            fontSize = 11.sp,
                        )
                    }
                    Column(horizontalAlignment = Alignment.End) {
                        Text(
                            moneyShort(Actions.sellPrice(t, plane.ageQuarters)),
                            color = TextMid,
                            fontSize = 11.sp,
                        )
                        if (route == null) {
                            OutlinedButton(
                                onClick = { vm.run(Command.SellAircraft(s.playerId, plane.id)) },
                            ) { Text("매각", fontSize = 11.sp) }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun AircraftMarket(vm: GameViewModel, modifier: Modifier) {
    val s = vm.game
    var usedMode by remember { mutableStateOf(false) }
    val catalog = if (usedMode) AircraftCatalog.usedFor(s.year) else AircraftCatalog.newFor(s.year)

    Column(modifier) {
        Panel(title = "기재 시장 · ${s.year}년") {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Chip("신조기", selected = !usedMode, onClick = { usedMode = false })
                Chip("중고기", selected = usedMode, accent = Amber, onClick = { usedMode = true })
            }
            VSpace(6)
            Text(
                if (usedMode) "즉시 인도. 기령이 있어 정비비가 더 듭니다."
                else "발주 후 2분기 뒤 인도. 대금은 지금 나갑니다.",
                color = TextLow,
                fontSize = 11.sp,
            )
        }
        VSpace(10)
        LazyColumn(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            items(catalog, key = { it.id }) { t ->
                val price = if (usedMode) Actions.usedPrice(t, 32) else t.price
                Column(
                    Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(10.dp))
                        .background(InkPanelHigh)
                        .padding(10.dp),
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Column(Modifier.weight(1f)) {
                            Text(t.name, color = TextHigh, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
                            Text("${t.maker} · ${t.year}년 취항", color = TextLow, fontSize = 10.sp)
                        }
                        Text(moneyShort(price), color = Amber, fontSize = 13.sp, fontWeight = FontWeight.Bold)
                    }
                    VSpace(6)
                    Text(
                        "${t.seats}석 · 항속 ${km(t.range.toDouble())} · ${grouped(t.speed.toLong())}km/h · " +
                            "연료 ${decimals(t.fuel, 1)}L/km",
                        color = TextMid,
                        fontSize = 11.sp,
                    )
                    VSpace(6)
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        for (n in listOf(1, 2, 5)) {
                            OutlinedButton(
                                onClick = { vm.run(Command.BuyAircraft(s.playerId, t.id, n, used = usedMode)) },
                                enabled = s.player.cash >= price * n,
                                modifier = Modifier.weight(1f),
                            ) { Text("${n}대", fontSize = 11.sp, color = Sky) }
                        }
                    }
                }
            }
        }
    }
}
