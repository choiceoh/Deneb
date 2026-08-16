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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import skytycoon.core.data.Cities
import skytycoon.core.model.BusinessType
import skytycoon.core.model.Region
import skytycoon.core.sim.Actions
import skytycoon.core.sim.Command
import skytycoon.core.sim.Economics
import skytycoon.ui.Amber
import skytycoon.ui.Coral
import skytycoon.ui.GameViewModel
import skytycoon.ui.InkPanelHigh
import skytycoon.ui.Mint
import skytycoon.ui.TextHigh
import skytycoon.ui.TextLow
import skytycoon.ui.TextMid
import skytycoon.ui.components.KeyValue
import skytycoon.ui.components.Panel
import skytycoon.ui.components.RatioBar
import skytycoon.ui.components.VSpace
import skytycoon.ui.decimals
import skytycoon.ui.money
import skytycoon.ui.moneyShort
import skytycoon.ui.percent

@Composable
fun ManageScreen(vm: GameViewModel, wide: Boolean) {
    val s = vm.game
    val player = s.player
    val cap = Actions.debtCapacity(s, player)
    val room = (cap - player.debt).coerceAtLeast(0.0)

    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(12.dp)) {
        Panel(title = "재무") {
            KeyValue("현금", moneyShort(player.cash))
            KeyValue("부채", moneyShort(player.debt), if (player.debt > 0) Coral else TextMid)
            KeyValue("차입 한도", moneyShort(cap))
            KeyValue("이자율", percent(Actions.interestRate(s, player), 1))
            KeyValue("기업가치", moneyShort(Economics.equity(s, player)), Amber)
            VSpace(8)
            Text("차입 여력", color = TextLow, fontSize = 11.sp)
            RatioBar(if (cap <= 0) 0.0 else player.debt / cap, Coral, Modifier.fillMaxWidth())
            VSpace(10)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                for (frac in listOf(0.25, 0.5, 1.0)) {
                    val amount = room * frac
                    OutlinedButton(
                        onClick = { vm.run(Command.Loan(player.id, amount)) },
                        enabled = amount > 1e6,
                        modifier = Modifier.weight(1f),
                    ) { Text("차입 ${moneyShort(amount)}", fontSize = 10.sp) }
                }
            }
            if (player.debt > 0) {
                VSpace(6)
                OutlinedButton(
                    onClick = {
                        vm.run(Command.Loan(player.id, -minOf(player.debt, player.cash)))
                    },
                    enabled = player.cash > 1e6,
                    modifier = Modifier.fillMaxWidth(),
                ) { Text("상환 ${moneyShort(minOf(player.debt, player.cash))}", fontSize = 11.sp) }
            }
        }

        VSpace(12)
        Panel(title = "기내 서비스") {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("현재 ${player.serviceLevel}등급", color = TextHigh, fontSize = 15.sp, fontWeight = FontWeight.Bold)
                Box(Modifier.width(12.dp))
                RatioBar(player.serviceLevel / 5.0, Mint, Modifier.weight(1f), height = 8)
            }
            VSpace(6)
            Text(
                "등급이 높으면 출장 승객이 몰리지만, 기재 수에 비례해 유지비가 붙습니다.",
                color = TextLow,
                fontSize = 11.sp,
            )
            VSpace(8)
            Button(
                onClick = { vm.run(Command.UpgradeService(player.id)) },
                enabled = player.serviceLevel < 5 &&
                    player.cash >= Actions.serviceUpgradeCost(s, player),
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(
                    if (player.serviceLevel >= 5) "최고 등급 달성" else
                        "${player.serviceLevel + 1}등급으로 (${moneyShort(Actions.serviceUpgradeCost(s, player))})",
                    fontSize = 12.sp,
                )
            }
        }

        VSpace(12)
        Panel(title = "지역별 광고") {
            Text(
                "브랜드 인지도는 매 분기 조금씩 빠집니다. 취항 지역에 꾸준히 태워야 점유율이 붙습니다.",
                color = TextLow,
                fontSize = 11.sp,
            )
            VSpace(8)
            for (region in Region.entries) {
                val current = player.adBudget[region] ?: 0.0
                var value by remember(region, current) { mutableStateOf((current / 1e6).toFloat()) }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(region.label, color = TextMid, fontSize = 12.sp, modifier = Modifier.width(70.dp))
                    Column(Modifier.weight(1f)) {
                        Slider(
                            value = value,
                            onValueChange = { value = it },
                            onValueChangeFinished = {
                                vm.run(Command.SetAdBudget(player.id, region, value.toDouble() * 1e6))
                            },
                            valueRange = 0f..30f,
                        )
                    }
                    Box(Modifier.width(8.dp))
                    Column(horizontalAlignment = Alignment.End, modifier = Modifier.width(84.dp)) {
                        Text("${decimals(value.toDouble(), 0)}백만", color = TextHigh, fontSize = 11.sp)
                        Text("인지도 ${decimals(player.brandIn(region), 0)}", color = TextLow, fontSize = 10.sp)
                    }
                }
            }
            VSpace(4)
            Text(
                "분기 광고비 합계 ${moneyShort(player.adBudget.values.sum())}",
                color = Amber,
                fontSize = 12.sp,
                fontWeight = FontWeight.SemiBold,
            )
        }

        VSpace(12)
        Panel(title = "부대사업") {
            Text(
                "취항 도시에만 낼 수 있습니다. 분기 수입이 들어오고, 그 도시를 오가는 우리 노선의 매력이 올라갑니다.",
                color = TextLow,
                fontSize = 11.sp,
            )
            VSpace(8)
            if (player.businesses.isNotEmpty()) {
                for (b in player.businesses) {
                    KeyValue("${Cities.name(b.city)} ${b.type.label}", "분기 ${moneyShort(b.type.income * s.world.inflation)}")
                }
                VSpace(8)
            }
            // 슬롯만 있는 도시가 아니라 실제로 취항 중인 도시만 후보로 (규칙과 일치시킨다).
            val myCities = s.routesOf(player.id)
                .filter { it.active }
                .flatMap { listOf(it.from, it.to) }
                .distinct()
                .sortedBy { Cities.name(it) }
            var city by remember(myCities.firstOrNull()) { mutableStateOf(myCities.firstOrNull() ?: player.home) }
            if (myCities.isEmpty()) {
                Text("취항 중인 노선이 없어 사업을 낼 수 없습니다.", color = Coral, fontSize = 12.sp)
            }
            Column {
                Text("도시 선택", color = TextLow, fontSize = 11.sp)
                VSpace(4)
                androidx.compose.foundation.layout.FlowRow(
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    for (c in myCities) {
                        skytycoon.ui.components.Chip(
                            Cities.name(c),
                            selected = city == c,
                            onClick = { city = c },
                        )
                    }
                }
                VSpace(10)
                for (type in BusinessType.entries) {
                    val exists = player.businesses.any { it.type == type && it.city == city }
                    val hangarElsewhere = type == BusinessType.HANGAR &&
                        player.businesses.any { it.type == BusinessType.HANGAR }
                    val cost = type.cost * s.world.inflation
                    val blocked = when {
                        myCities.isEmpty() -> "취항 필요"
                        exists -> "보유"
                        hangarElsewhere -> "전사 1개"
                        player.cash < cost -> moneyShort(cost)
                        else -> null
                    }
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .clip(RoundedCornerShape(8.dp))
                            .background(InkPanelHigh)
                            .padding(8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Column(Modifier.weight(1f)) {
                            Text(type.label, color = TextHigh, fontSize = 13.sp, fontWeight = FontWeight.Medium)
                            Text(businessHint(type), color = TextLow, fontSize = 10.sp)
                        }
                        OutlinedButton(
                            onClick = { vm.run(Command.BuildBusiness(player.id, type, city)) },
                            enabled = blocked == null,
                        ) {
                            Text(blocked ?: moneyShort(cost), fontSize = 11.sp)
                        }
                    }
                    VSpace(6)
                }
            }
        }
        VSpace(12)
        Panel(title = "저장") {
            Text(
                "분기를 넘길 때마다 자동 저장됩니다. 그와 별개로 지금 시점을 찍어 두었다가 " +
                    "언제든 그 자리로 되돌아올 수 있습니다.",
                color = TextLow,
                fontSize = 11.sp,
            )
            VSpace(8)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(onClick = { vm.saveNow() }, modifier = Modifier.weight(1f)) {
                    Text("이 시점 저장", fontSize = 12.sp)
                }
                OutlinedButton(
                    onClick = { vm.loadSaved() },
                    enabled = vm.hasManualSave(),
                    modifier = Modifier.weight(1f),
                ) {
                    Text("저장 지점으로", fontSize = 12.sp)
                }
            }
        }

        VSpace(20)
    }
}

private fun businessHint(type: BusinessType): String = when (type) {
    BusinessType.HOTEL -> "분기 수입이 크고 브랜드에도 도움"
    BusinessType.DUTY_FREE -> "안정적인 부수입"
    BusinessType.AGENCY -> "그 도시 노선의 송객력 상승"
    BusinessType.HANGAR -> "전 노선 정비비 12% 절감"
    BusinessType.LOUNGE -> "브랜드 인지도에 가장 효과적"
}
