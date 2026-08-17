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
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Slider
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import skytycoon.core.data.AircraftCatalog
import skytycoon.core.data.Cities
import skytycoon.core.model.City
import skytycoon.core.model.GameState
import skytycoon.core.sim.Balance
import skytycoon.core.sim.Actions
import skytycoon.core.sim.Command
import skytycoon.core.sim.Demand
import skytycoon.core.sim.Economics
import skytycoon.core.sim.Geo
import skytycoon.ui.Amber
import skytycoon.ui.Coral
import skytycoon.ui.Ink
import skytycoon.ui.GameViewModel
import skytycoon.ui.InkPanelHigh
import skytycoon.ui.Mint
import skytycoon.ui.Sky
import skytycoon.ui.TextHigh
import skytycoon.ui.TextLow
import skytycoon.ui.TextMid
import skytycoon.ui.components.Chip
import skytycoon.ui.components.EmptyHint
import skytycoon.ui.components.KeyValue
import skytycoon.ui.components.Panel
import skytycoon.ui.components.VSpace
import skytycoon.ui.components.WorldMap
import skytycoon.ui.decimals
import skytycoon.ui.km
import skytycoon.ui.money
import skytycoon.ui.moneyShort
import skytycoon.ui.people

@Composable
fun NetworkScreen(vm: GameViewModel, wide: Boolean) {
    val s = vm.game
    var selected by remember { mutableStateOf<String?>(s.player.home) }
    var showRivals by remember { mutableStateOf(true) }
    var composing by remember { mutableStateOf<Pair<String, String>?>(null) }

    val map = @Composable { modifier: Modifier ->
        Box(modifier) {
            WorldMap(
                state = s,
                selectedCity = selected,
                showRivals = showRivals,
                onSelectCity = { selected = it ?: selected },
                modifier = Modifier.fillMaxSize(),
                initialZoom = if (wide) 1f else 2.1f,
                initialCenterLon = if (wide) null else Cities[s.player.home].lon,
            )
            // 지도 위에 얹히는 조작부라 배경을 깔아 대륙과 섞이지 않게 한다.
            Row(
                Modifier
                    .padding(8.dp)
                    .clip(RoundedCornerShape(20.dp))
                    .background(Ink.copy(alpha = 0.82f))
                    .padding(horizontal = 10.dp, vertical = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Switch(
                    checked = showRivals,
                    onCheckedChange = { showRivals = it },
                    modifier = Modifier.scale(0.75f),
                )
                Text("경쟁사 노선", color = TextMid, fontSize = 11.sp)
                Box(Modifier.width(10.dp))
                Text("꼬집어 확대", color = TextLow, fontSize = 10.sp)
            }
        }
    }

    val panel = @Composable { modifier: Modifier ->
        Box(modifier) {
            selected?.let { CityPanel(vm, Cities[it], onCompose = { to -> composing = it to to }) }
                ?: EmptyHint("지도에서 도시를 선택하세요")
        }
    }

    if (wide) {
        Row(Modifier.fillMaxSize()) {
            map(Modifier.weight(1f).fillMaxHeight())
            panel(Modifier.width(390.dp).fillMaxHeight().padding(12.dp))
        }
    } else {
        Column(Modifier.fillMaxSize()) {
            map(Modifier.fillMaxWidth().height(250.dp))
            panel(Modifier.fillMaxWidth().weight(1f).padding(12.dp))
        }
    }

    composing?.let { (from, to) ->
        RouteComposer(vm, from, to, onDismiss = { composing = null })
    }
}

@Composable
private fun CityPanel(vm: GameViewModel, city: City, onCompose: (String) -> Unit) {
    val s = vm.game
    val player = s.player
    val mine = player.slotsAt(city.id)
    val used = s.usedSlots(player.id, city.id)
    val unsold = s.unsoldSlots(city)
    val price = Economics.slotPrice(s, player.id, city.id)
    val closed = s.cityState[city.id]?.isClosed(s.turn) == true

    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
        Panel {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(city.name, color = TextHigh, fontSize = 20.sp, fontWeight = FontWeight.Bold)
                    Text(
                        "${city.code} · ${city.region.label}",
                        color = TextMid,
                        fontSize = 12.sp,
                    )
                }
                if (closed) Chip("폐쇄 중", accent = Coral, selected = true)
            }
            VSpace(10)
            KeyValue("경제 규모", decimals(city.econ * (s.cityState[city.id]?.dev ?: 1.0), 0))
            KeyValue("관광 매력", decimals(city.tour, 0))
            KeyValue("착륙료", "표준의 ${decimals(city.fee, 2)}배")
        }

        VSpace(12)
        Panel(title = "슬롯") {
            KeyValue("내 보유", "${mine}개 (사용 ${used} · 여유 ${mine - used})")
            KeyValue("공항 전체", "${city.slots + (s.cityState[city.id]?.extraSlots ?: 0)}개")
            KeyValue("미분양", "${unsold}개", if (unsold > 0) Mint else Coral)
            KeyValue("매입 단가", moneyShort(price), Amber)
            VSpace(8)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                for (n in listOf(1, 3, 5)) {
                    OutlinedButton(
                        onClick = { vm.run(Command.BuySlots(player.id, city.id, n)) },
                        enabled = unsold >= n && player.cash >= Actions.slotCost(s, player.id, city.id, n),
                        modifier = Modifier.weight(1f),
                    ) {
                        Text("+$n", fontSize = 12.sp)
                    }
                }
            }
            if (unsold == 0) {
                VSpace(6)
                Text("남은 슬롯이 없습니다. 공항을 넓히거나 항공사가 무너져야 풀립니다.", color = TextLow, fontSize = 11.sp)
            }
        }

        VSpace(12)
        Panel(title = "공항 확장 출자") {
            val building = s.expansions.firstOrNull { it.city == city.id }
            if (building != null) {
                val left = building.deliverTurn - s.turn
                val sponsor = s.airlineOrNull(building.sponsorId)
                KeyValue("공사 중", "${left}분기 남음", Amber)
                KeyValue("출자사", sponsor?.name ?: "—")
                KeyValue("완공 시 슬롯", "+${building.slots}개 (출자사 ${building.sponsorSlots}개 우선)")
            } else {
                val cost = Actions.expansionCost(s, player.id, city.id)
                val connected = mine > 0 || s.routesOf(player.id).any { it.active && it.touches(city.id) }
                KeyValue("공사비", moneyShort(cost), Amber)
                KeyValue("완공까지", "${Balance.EXPANSION_QUARTERS}분기")
                KeyValue(
                    "완공 시",
                    "슬롯 +${Balance.EXPANSION_SLOTS}개 · 그중 " +
                        "${(Balance.EXPANSION_SLOTS * Balance.EXPANSION_SPONSOR_SHARE).toInt()}개는 내 몫",
                )
                VSpace(8)
                Button(
                    onClick = { vm.run(Command.FundExpansion(player.id, city.id)) },
                    enabled = connected && player.cash >= cost,
                    modifier = Modifier.fillMaxWidth(),
                ) { Text("확장 공사에 출자", fontSize = 13.sp) }
                Text(
                    if (!connected) {
                        "이 공항에 취항하거나 슬롯을 가진 뒤에 제안할 수 있습니다."
                    } else {
                        "포화된 요지를 넓혀 선점하는 장기 투자입니다. 넓힐수록 다음 공사가 비싸집니다."
                    },
                    color = TextLow,
                    fontSize = 11.sp,
                )
            }
        }

        VSpace(12)
        Panel(title = "취항 후보") {
            // 수요 상위만 보여주면 추천은 깔끔하지만, 양쪽 도시가 서로를 14위 밖으로 미는
            // 한산한 노선은 개설 화면에 영영 닿지 못한다 (시뮬레이션은 지원하는데도).
            // 기본은 추천 순, 펼치면 전 도시.
            var showAll by remember(city.id) { mutableStateOf(false) }
            val ranked = Cities.all
                .filter { it.id != city.id }
                .map { it to Demand.annualEstimate(s, city, it) }
                .sortedByDescending { it.second }
            val candidates = if (showAll) ranked else ranked.take(14)

            for ((dest, demand) in candidates) {
                val dist = Geo.distance(city.id, dest.id)
                val existing = s.routes.any {
                    it.airlineId == player.id && Geo.pairKey(it.from, it.to) == Geo.pairKey(city.id, dest.id)
                }
                val rivals = s.routes.count {
                    it.active && it.airlineId != player.id &&
                        Geo.pairKey(it.from, it.to) == Geo.pairKey(city.id, dest.id)
                }
                Row(
                    Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(8.dp))
                        .clickable(enabled = !existing) { onCompose(dest.id) }
                        .padding(vertical = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(Modifier.weight(1f)) {
                        Text(
                            dest.name,
                            color = if (existing) TextLow else TextHigh,
                            fontSize = 13.sp,
                            fontWeight = FontWeight.Medium,
                        )
                        Text(
                            "${km(dist)} · 연 수요 ${people(demand)}" + when {
                                existing -> " · 우리 노선"
                                rivals > 0 -> " · 경쟁 ${rivals}사"
                                else -> " · 미개척"
                            },
                            color = if (!existing && rivals == 0) Mint else TextMid,
                            fontSize = 11.sp,
                        )
                    }
                    Text(
                        if (existing) "취항 중" else "개설 ›",
                        color = if (existing) TextLow else Sky,
                        fontSize = 12.sp,
                        fontWeight = FontWeight.SemiBold,
                    )
                }
            }
            if (ranked.size > 14) {
                VSpace(6)
                Text(
                    if (showAll) "추천 상위만 보기" else "전체 ${ranked.size}개 도시 보기",
                    color = Sky,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.SemiBold,
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(8.dp))
                        .clickable { showAll = !showAll }
                        .padding(vertical = 8.dp),
                )
            }
        }
        VSpace(20)
    }
}

/** 노선 개설 다이얼로그 — 기재를 고르고 편수·운임을 정한다. */
@Composable
private fun RouteComposer(vm: GameViewModel, from: String, to: String, onDismiss: () -> Unit) {
    val s = vm.game
    val player = s.player
    val dist = Geo.distance(from, to)
    val idle = remember(s) {
        s.planesOf(player.id).filter {
            it.routeId == null && Economics.canFly(AircraftCatalog[it.typeId], dist)
        }
    }
    var picked by remember { mutableStateOf(setOf<Int>()) }
    var freq by remember { mutableStateOf(3f) }
    var fare by remember { mutableStateOf(1f) }

    val chosen = idle.filter { it.id in picked }
    val cap = Economics.capacity(chosen, dist)
    val slotLimit = minOf(s.freeSlots(player.id, from), s.freeSlots(player.id, to))
    val maxFreq = minOf(cap.maxFreq, slotLimit).coerceAtLeast(0)
    val effFreq = freq.toInt().coerceIn(0, maxFreq)
    val setupCost = Actions.routeSetupCost(s, from, to)

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("${Cities.name(from)} – ${Cities.name(to)}", fontWeight = FontWeight.Bold) },
        text = {
            Column(Modifier.heightIn(max = 460.dp).verticalScroll(rememberScrollState())) {
                Text("거리 ${km(dist)} · 표준 운임 ${moneyShort(Economics.standardFare(dist, s.world.inflation))}", color = TextMid, fontSize = 12.sp)
                VSpace(10)

                Text("투입 기재", color = TextLow, fontSize = 11.sp, fontWeight = FontWeight.Bold)
                if (idle.isEmpty()) {
                    Text(
                        "이 거리를 날 수 있는 유휴 기재가 없습니다. 기재를 발주하거나 다른 노선의 배속을 푸세요.",
                        color = Coral,
                        fontSize = 12.sp,
                    )
                }
                for (p in idle) {
                    val t = AircraftCatalog[p.typeId]
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .clickable {
                                picked = if (p.id in picked) picked - p.id else picked + p.id
                            }
                            .padding(vertical = 2.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Checkbox(checked = p.id in picked, onCheckedChange = null)
                        Box(Modifier.width(6.dp))
                        Text(
                            "${t.name} · ${t.seats}석 · 기령 ${p.ageQuarters / 4}년",
                            color = TextHigh,
                            fontSize = 12.sp,
                        )
                    }
                }

                VSpace(12)
                Text(
                    "주간 왕복 ${effFreq}회 (기재 한계 ${cap.maxFreq} · 슬롯 한계 $slotLimit)",
                    color = TextHigh,
                    fontSize = 12.sp,
                )
                Slider(
                    value = freq,
                    onValueChange = { freq = it },
                    valueRange = 0f..(maxFreq.coerceAtLeast(1)).toFloat(),
                    enabled = maxFreq > 0,
                )

                VSpace(4)
                Text("운임 배율 ${decimals(fare.toDouble(), 2)}배", color = TextHigh, fontSize = 12.sp)
                Slider(value = fare, onValueChange = { fare = it }, valueRange = 0.55f..1.8f)
                Text(
                    "낮으면 관광객이, 높으면 출장객이 남습니다.",
                    color = TextLow,
                    fontSize = 11.sp,
                )

                VSpace(10)
                Box(Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).background(InkPanelHigh).padding(10.dp)) {
                    Column {
                        KeyValue("분기 공급 좌석", people(Economics.quarterlySeats(effFreq, cap.avgSeats)))
                        KeyValue(
                            "구간 연 수요",
                            people(Demand.annualEstimate(s, Cities[from], Cities[to])),
                        )
                        KeyValue("개설 비용", moneyShort(setupCost), Amber)
                    }
                }
            }
        },
        confirmButton = {
            Button(
                enabled = chosen.isNotEmpty() && effFreq >= 1 && player.cash >= setupCost,
                onClick = {
                    val ok = vm.run(
                        Command.OpenRoute(
                            airlineId = player.id,
                            from = from,
                            to = to,
                            planeIds = chosen.map { it.id },
                            freq = effFreq,
                            fareMul = fare.toDouble(),
                        ),
                    )
                    if (ok) onDismiss()
                },
            ) { Text("개설") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("취소") } },
    )
}
