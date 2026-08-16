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
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import skytycoon.core.data.Cities
import skytycoon.core.sim.Actions
import skytycoon.core.sim.Balance
import skytycoon.core.sim.Command
import skytycoon.core.sim.Economics
import skytycoon.core.sim.Stock
import skytycoon.ui.Amber
import skytycoon.ui.Coral
import skytycoon.ui.GameViewModel
import skytycoon.ui.InkPanelHigh
import skytycoon.ui.Mint
import skytycoon.ui.TextHigh
import skytycoon.ui.TextLow
import skytycoon.ui.TextMid
import skytycoon.ui.components.Dot
import skytycoon.ui.components.KeyValue
import skytycoon.ui.components.Panel
import skytycoon.ui.components.RatioBar
import skytycoon.ui.components.VSpace
import skytycoon.ui.decimals
import skytycoon.ui.money
import skytycoon.ui.moneyShort
import skytycoon.ui.percent
import skytycoon.ui.profitColor
import skytycoon.ui.signedMoney

/** 경쟁사 판세와 지분 싸움. AM2 의 백미인 인수합병이 여기서 벌어진다. */
@Composable
fun MarketScreen(vm: GameViewModel, wide: Boolean) {
    val s = vm.game
    val player = s.player

    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(12.dp)) {
        Panel(title = "우리 회사 주식") {
            KeyValue("주가", moneyShort(player.sharePrice))
            KeyValue("발행 주식", "${money(player.shares)}주")
            KeyValue("시가총액", moneyShort(player.sharePrice * player.shares))
            val threat = s.livingAirlines
                .filter { it.id != player.id }
                .map { it to Stock.ownershipRatio(s, it.id, player.id) }
                .maxByOrNull { it.second }
            if (threat != null && threat.second > 0.0) {
                VSpace(6)
                Text(
                    "${threat.first.name}이 ${percent(threat.second, 1)} 보유 중",
                    color = if (threat.second >= 0.25) Coral else Amber,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.SemiBold,
                )
                RatioBar(threat.second / 0.5, if (threat.second >= 0.25) Coral else Amber, Modifier.fillMaxWidth())
                Text("50% 도달 시 경영권 상실", color = TextLow, fontSize = 11.sp)
            }
            VSpace(10)
            val issue = Actions.maxIssuable(player)
            OutlinedButton(
                onClick = { vm.run(Command.IssueShares(player.id, issue)) },
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(
                    "유상증자 ${money(issue)}주 → ${moneyShort(issue * player.sharePrice * Balance.ISSUE_DISCOUNT)}",
                    fontSize = 12.sp,
                )
            }
            Text(
                "자금을 조달하면서 적대적 지분을 희석합니다. 다만 내 주식 가치도 나뉩니다.",
                color = TextLow,
                fontSize = 11.sp,
            )
        }

        VSpace(12)
        for (rival in s.airlines.filter { it.id != player.id }) {
            val alive = rival.alive
            val equity = Economics.equity(s, rival)
            val myStake = Stock.ownershipRatio(s, player.id, rival.id)
            val annualNet = rival.results.takeLast(4).sumOf { it.net }
            val limit = if (alive) Stock.maxBuyableThisQuarter(s, player.id, rival.id) else 0.0
            // 한도 전액을 못 내더라도 낼 수 있는 만큼은 살 수 있어야 한다. 인수에 따르는
            // 정리 대금·연쇄 인수까지 계산된 값이라 여기 뜬 물량은 반드시 실행된다.
            val buyable = if (alive) Stock.affordableShares(s, player.id, rival.id) else 0.0
            val cost = if (buyable > 0) Stock.buyCost(s, player.id, rival.id, buyable) else 0.0

            Column(
                Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(12.dp))
                    .background(InkPanelHigh)
                    .padding(12.dp),
            ) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Dot(Color(rival.colorArgb), 10)
                    Box(Modifier.width(8.dp))
                    Column(Modifier.weight(1f)) {
                        Text(
                            rival.name + if (!alive) " (소멸)" else "",
                            color = if (alive) TextHigh else TextLow,
                            fontSize = 15.sp,
                            fontWeight = FontWeight.Bold,
                        )
                        Text(
                            "${Cities.name(rival.home)} 거점 · ${rival.trait.label}",
                            color = TextMid,
                            fontSize = 11.sp,
                        )
                    }
                    Text(moneyShort(equity), color = TextHigh, fontSize = 14.sp, fontWeight = FontWeight.Bold)
                }
                if (alive) {
                    VSpace(8)
                    KeyValue("노선 / 기재", "${s.routesOf(rival.id).size}개 / ${s.planesOf(rival.id).size}대")
                    KeyValue("주가", moneyShort(rival.sharePrice))
                    KeyValue("연 순익", signedMoney(annualNet), profitColor(annualNet))
                    KeyValue("서비스 등급", "${rival.serviceLevel}등급")
                    KeyValue("내 지분", percent(myStake, 1), if (myStake > 0) Mint else TextMid)

                    VSpace(8)
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Button(
                            onClick = { vm.run(Command.TradeShares(player.id, rival.id, buyable)) },
                            enabled = buyable > rival.shares * 0.001,
                            modifier = Modifier.weight(1f),
                        ) {
                            Text(
                                if (buyable <= 0) "매수 불가" else
                                    "지분 ${percent(buyable / rival.shares, 1)} 매수 (${moneyShort(cost)})",
                                fontSize = 11.sp,
                            )
                        }
                        if (myStake > 0) {
                            OutlinedButton(
                                onClick = {
                                    val held = player.holdings[rival.id] ?: 0.0
                                    vm.run(Command.TradeShares(player.id, rival.id, -held))
                                },
                                modifier = Modifier.weight(1f),
                            ) { Text("전량 매도", fontSize = 11.sp) }
                        }
                    }
                    Text(
                        "이번 분기 잔여 한도 ${percent(limit / rival.shares, 1)} · 50% 초과 시 인수 완료",
                        color = TextLow,
                        fontSize = 10.sp,
                    )
                }
            }
            VSpace(8)
        }
        VSpace(20)
    }
}
