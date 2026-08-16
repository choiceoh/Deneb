package skytycoon.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import skytycoon.core.data.AircraftCatalog
import skytycoon.core.data.Cities
import skytycoon.core.model.Route
import skytycoon.core.sim.Command
import skytycoon.core.sim.Economics
import skytycoon.core.sim.Geo
import skytycoon.ui.Amber
import skytycoon.ui.Coral
import skytycoon.ui.GameViewModel
import skytycoon.ui.InkPanelHigh
import skytycoon.ui.Sky
import skytycoon.ui.TextHigh
import skytycoon.ui.TextLow
import skytycoon.ui.TextMid
import skytycoon.ui.components.EmptyHint
import skytycoon.ui.components.KeyValue
import skytycoon.ui.components.Panel
import skytycoon.ui.components.RatioBar
import skytycoon.ui.components.VSpace
import skytycoon.ui.decimals
import skytycoon.ui.km
import skytycoon.ui.loadFactorColor
import skytycoon.ui.moneyShort
import skytycoon.ui.people
import skytycoon.ui.percent
import skytycoon.ui.profitColor
import skytycoon.ui.signedMoney

@Composable
fun RoutesScreen(vm: GameViewModel, wide: Boolean) {
    val s = vm.game
    val routes = s.routesOf(s.playerId).sortedByDescending { it.last?.profit ?: 0.0 }
    var selectedId by remember { mutableStateOf<Int?>(null) }
    val selected = routes.firstOrNull { it.id == selectedId } ?: routes.firstOrNull()

    if (routes.isEmpty()) {
        EmptyHint("아직 노선이 없습니다. 노선망 화면에서 도시를 골라 개설하세요.")
        return
    }

    val list = @Composable { modifier: Modifier ->
        LazyColumn(modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            items(routes, key = { it.id }) { route ->
                RouteRow(route, selected?.id == route.id) { selectedId = route.id }
            }
        }
    }

    if (wide) {
        Row(Modifier.fillMaxSize()) {
            list(Modifier.weight(1f).fillMaxHeight())
            Box(Modifier.width(400.dp).fillMaxHeight().padding(12.dp)) {
                selected?.let { RouteDetail(vm, it) }
            }
        }
    } else {
        Column(Modifier.fillMaxSize()) {
            list(Modifier.fillMaxWidth().weight(1f))
            Box(Modifier.fillMaxWidth().weight(1.1f).padding(12.dp)) {
                selected?.let { RouteDetail(vm, it) }
            }
        }
    }
}

@Composable
private fun RouteRow(route: Route, selected: Boolean, onClick: () -> Unit) {
    val last = route.last
    Column(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(10.dp))
            .background(if (selected) Sky.copy(alpha = 0.14f) else InkPanelHigh)
            .clickable { onClick() }
            .padding(10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                "${Cities.name(route.from)} – ${Cities.name(route.to)}",
                color = TextHigh,
                fontSize = 14.sp,
                fontWeight = FontWeight.SemiBold,
                modifier = Modifier.weight(1f),
            )
            Text(
                last?.let { signedMoney(it.profit) } ?: "—",
                color = last?.let { profitColor(it.profit) } ?: TextMid,
                fontSize = 13.sp,
                fontWeight = FontWeight.Bold,
            )
        }
        VSpace(4)
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                "주 ${route.freq}왕복 · 운임 ${decimals(route.fareMul, 2)}배 · 기재 ${route.planeIds.size}대",
                color = TextMid,
                fontSize = 11.sp,
                modifier = Modifier.weight(1f),
            )
            if (last != null) {
                Text(percent(last.loadFactor), color = loadFactorColor(last.loadFactor), fontSize = 11.sp)
            }
        }
        if (last != null) {
            VSpace(4)
            RatioBar(last.loadFactor, loadFactorColor(last.loadFactor), Modifier.fillMaxWidth())
        }
    }
}

@Composable
private fun RouteDetail(vm: GameViewModel, route: Route) {
    val s = vm.game
    val player = s.player
    val dist = Geo.distance(route.from, route.to)
    val onRoute = s.planes.filter { it.routeId == route.id }
    val cap = Economics.capacity(onRoute, dist)
    val slotHeadroom = minOf(
        s.freeSlots(player.id, route.from) + route.freq,
        s.freeSlots(player.id, route.to) + route.freq,
    )
    val maxFreq = minOf(cap.maxFreq, slotHeadroom)

    // 배속 변경으로 route.freq 가 자동으로 깎일 수 있다. 노선 id 만 키로 쓰면
    // 슬라이더가 옛 값을 들고 있다가 범위 밖 값을 그대로 제출한다.
    var fare by remember(route.id, route.fareMul) { mutableStateOf(route.fareMul.toFloat()) }
    var freq by remember(route.id, route.freq) { mutableStateOf(route.freq.toFloat()) }
    var service by remember(route.id) { mutableStateOf(route.serviceExtra) }
    var editingFleet by remember(route.id) { mutableStateOf(false) }

    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
        Panel {
            Text(
                "${Cities.name(route.from)} – ${Cities.name(route.to)}",
                color = TextHigh,
                fontSize = 18.sp,
                fontWeight = FontWeight.Bold,
            )
            Text("${km(dist)} · 표준 운임 ${moneyShort(Economics.standardFare(dist, s.world.inflation))}", color = TextMid, fontSize = 12.sp)
            route.last?.let { r ->
                VSpace(10)
                KeyValue("수송객", people(r.pax))
                KeyValue("공급 좌석", people(r.seats))
                KeyValue("탑승률", percent(r.loadFactor), loadFactorColor(r.loadFactor))
                KeyValue("구간 점유율", percent(r.share))
                KeyValue("매출", moneyShort(r.revenue))
                KeyValue("운항 원가", moneyShort(r.cost))
                KeyValue("손익", signedMoney(r.profit), profitColor(r.profit))
            }
        }

        VSpace(12)
        Panel(title = "운항 조건") {
            Text("주간 왕복 ${freq.toInt()}회 (최대 $maxFreq)", color = TextHigh, fontSize = 12.sp)
            Slider(
                value = freq,
                onValueChange = { freq = it },
                onValueChangeFinished = {
                    vm.run(Command.TuneRoute(player.id, route.id, freq = freq.toInt()))
                },
                valueRange = 0f..maxFreq.coerceAtLeast(1).toFloat(),
            )
            Text(
                if (cap.maxFreq <= slotHeadroom) "기재 가동률이 한계입니다 — 증편하려면 기재를 더 넣으세요."
                else "슬롯이 한계입니다 — 증편하려면 슬롯을 더 사세요.",
                color = TextLow,
                fontSize = 11.sp,
            )

            VSpace(10)
            Text("운임 ${decimals(fare.toDouble(), 2)}배", color = TextHigh, fontSize = 12.sp)
            Slider(
                value = fare,
                onValueChange = { fare = it },
                onValueChangeFinished = {
                    vm.run(Command.TuneRoute(player.id, route.id, fareMul = fare.toDouble()))
                },
                valueRange = 0.55f..1.8f,
            )

            VSpace(10)
            Text("노선 서비스 투자", color = TextHigh, fontSize = 12.sp)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                for (lvl in 0..2) {
                    OutlinedButton(
                        onClick = {
                            service = lvl
                            vm.run(Command.TuneRoute(player.id, route.id, serviceExtra = lvl))
                        },
                        modifier = Modifier.weight(1f),
                    ) {
                        Text(
                            listOf("기본", "＋1", "＋2")[lvl],
                            fontSize = 12.sp,
                            color = if (service == lvl) Amber else TextMid,
                        )
                    }
                }
            }
        }

        VSpace(12)
        Panel(title = "배속 기재", trailing = {
            Text(
                if (editingFleet) "닫기" else "변경",
                color = Sky,
                fontSize = 11.sp,
                modifier = Modifier.clickable { editingFleet = !editingFleet },
            )
        }) {
            if (!editingFleet) {
                for (p in onRoute) {
                    val t = AircraftCatalog[p.typeId]
                    KeyValue(t.name, "${t.seats}석 · 기령 ${p.ageQuarters / 4}년")
                }
                if (onRoute.isEmpty()) Text("배속된 기재가 없습니다.", color = Coral, fontSize = 12.sp)
            } else {
                val candidates = s.planesOf(player.id).filter {
                    (it.routeId == null || it.routeId == route.id) &&
                        Economics.canFly(AircraftCatalog[it.typeId], dist)
                }
                var picked by remember(route.id) { mutableStateOf(onRoute.map { it.id }.toSet()) }
                for (p in candidates) {
                    val t = AircraftCatalog[p.typeId]
                    Row(
                        Modifier.fillMaxWidth().clickable {
                            picked = if (p.id in picked) picked - p.id else picked + p.id
                        },
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Checkbox(checked = p.id in picked, onCheckedChange = null)
                        Text("${t.name} · ${t.seats}석", color = TextHigh, fontSize = 12.sp)
                    }
                }
                VSpace(8)
                Button(
                    onClick = {
                        if (vm.run(Command.AssignPlanes(player.id, route.id, picked.toList()))) {
                            editingFleet = false
                        }
                    },
                    modifier = Modifier.fillMaxWidth(),
                ) { Text("배속 적용") }
            }
        }

        VSpace(12)
        OutlinedButton(
            onClick = { vm.run(Command.CloseRoute(player.id, route.id)) },
            modifier = Modifier.fillMaxWidth(),
        ) { Text("노선 폐지", color = Coral) }
        VSpace(20)
    }
}
