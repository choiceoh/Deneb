package skytycoon.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
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
import skytycoon.core.model.GameState
import skytycoon.core.sim.Actions
import skytycoon.core.sim.Economics
import skytycoon.core.sim.Stock
import skytycoon.core.sim.TurnEngine
import skytycoon.ui.Amber
import skytycoon.ui.Coral
import skytycoon.ui.GameViewModel
import skytycoon.ui.Mint
import skytycoon.ui.Sky
import skytycoon.ui.Tab
import skytycoon.ui.TextHigh
import skytycoon.ui.TextLow
import skytycoon.ui.TextMid
import skytycoon.ui.components.Dot
import skytycoon.ui.components.KeyValue
import skytycoon.ui.components.Panel
import skytycoon.ui.components.RatioBar
import skytycoon.ui.components.StatTile
import skytycoon.ui.components.VSpace
import skytycoon.ui.loadFactorColor
import skytycoon.ui.money
import skytycoon.ui.moneyShort
import skytycoon.ui.percent
import skytycoon.ui.profitColor
import skytycoon.ui.signedMoney

/** 경영진 브리핑. "지금 당장 뭘 해야 하는가"가 맨 위에 오도록 짰다. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun DashboardScreen(vm: GameViewModel, wide: Boolean, onGoTo: (Tab) -> Unit) {
    val s = vm.game
    val player = s.player
    val last = player.lastResult

    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp)) {
        s.outcome?.let { outcome ->
            Panel {
                Text(
                    if (outcome.won) "승리" else "종료",
                    color = if (outcome.won) Amber else Coral,
                    fontSize = 22.sp,
                    fontWeight = FontWeight.Black,
                )
                VSpace(4)
                Text(outcome.reason, color = TextHigh, fontSize = 14.sp)
            }
            VSpace(14)
        }

        Panel(title = "지난 분기 실적") {
            if (last == null) {
                Text("아직 첫 분기를 진행하지 않았습니다.", color = TextMid, fontSize = 13.sp)
            } else {
                FlowRow(
                    Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(4.dp),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    StatTile("매출", moneyShort(last.totalRevenue))
                    StatTile("영업비용", moneyShort(last.operatingCost))
                    StatTile("순익", signedMoney(last.net), profitColor(last.net))
                    StatTile("수송객", "${money(last.pax)}명")
                    StatTile("탑승률", percent(last.loadFactor), loadFactorColor(last.loadFactor))
                }
            }
        }

        VSpace(14)
        val alerts = buildAlerts(s)
        Panel(title = "할 일") {
            if (alerts.isEmpty()) {
                Text("급한 현안은 없습니다. 확장을 노려볼 때입니다.", color = TextMid, fontSize = 13.sp)
            } else {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    for (a in alerts) {
                        Row(
                            Modifier
                                .fillMaxWidth()
                                .clip(RoundedCornerShape(8.dp))
                                .clickable { onGoTo(a.tab) }
                                .padding(vertical = 4.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Dot(a.color)
                            Box(Modifier.width(10.dp))
                            Column(Modifier.weight(1f)) {
                                Text(a.title, color = TextHigh, fontSize = 13.sp, fontWeight = FontWeight.Medium)
                                Text(a.body, color = TextMid, fontSize = 11.sp)
                            }
                            Text(a.tab.label + " ›", color = Sky, fontSize = 11.sp)
                        }
                    }
                }
            }
        }

        VSpace(14)
        Panel(title = "업계 순위") {
            for ((idx, entry) in TurnEngine.ranking(s).withIndex()) {
                val (airline, equity) = entry
                Row(Modifier.fillMaxWidth().padding(vertical = 5.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        "${idx + 1}",
                        color = if (idx == 0) Amber else TextLow,
                        fontSize = 13.sp,
                        fontWeight = FontWeight.Bold,
                        modifier = Modifier.width(20.dp),
                    )
                    Dot(Color(airline.colorArgb))
                    Box(Modifier.width(8.dp))
                    Text(
                        airline.name + if (airline.isPlayer) " (나)" else "",
                        color = if (airline.isPlayer) TextHigh else TextMid,
                        fontSize = 13.sp,
                        fontWeight = if (airline.isPlayer) FontWeight.Bold else FontWeight.Normal,
                        modifier = Modifier.weight(1f),
                    )
                    Text(
                        "노선 ${s.routesOf(airline.id).size} · 기재 ${s.planesOf(airline.id).size}",
                        color = TextLow,
                        fontSize = 11.sp,
                    )
                    Box(Modifier.width(12.dp))
                    Text(moneyShort(equity), color = TextHigh, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
                }
            }
        }

        VSpace(14)
        Panel(title = "세계 정세") {
            KeyValue("항공유", "$${skytycoon.ui.decimals(s.world.oil, 3)}/L")
            KeyValue("세계 경기", percent(s.world.economy, 0), if (s.world.economy >= 1) Mint else Coral)
            KeyValue("기준 금리", percent(s.world.interest, 1))
            KeyValue("물가 지수", "${skytycoon.ui.decimals(s.world.inflation, 2)}배")
            VSpace(6)
            Text("지역별 경기", color = TextLow, fontSize = 11.sp)
            VSpace(4)
            for ((region, v) in s.world.regionEconomy) {
                Row(Modifier.fillMaxWidth().padding(vertical = 2.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(region.label, color = TextMid, fontSize = 11.sp, modifier = Modifier.width(64.dp))
                    RatioBar(
                        (v / 1.6).coerceIn(0.0, 1.0),
                        if (v >= 1.0) Mint else Coral,
                        Modifier.weight(1f),
                    )
                    Box(Modifier.width(8.dp))
                    Text(percent(v, 0), color = TextMid, fontSize = 11.sp)
                }
            }
        }

        VSpace(14)
        Panel(title = "최근 소식", trailing = {
            Text("전체 보기 ›", color = Sky, fontSize = 11.sp, modifier = Modifier.clickable { onGoTo(Tab.NEWS) })
        }) {
            for (item in s.news.takeLast(4).reversed()) {
                Column(Modifier.padding(vertical = 4.dp)) {
                    Text(item.headline, color = TextHigh, fontSize = 13.sp, fontWeight = FontWeight.Medium)
                    if (item.body.isNotBlank()) {
                        Text(item.body, color = TextMid, fontSize = 11.sp)
                    }
                }
            }
        }
        VSpace(20)
    }
}

private class Alert(val title: String, val body: String, val color: Color, val tab: Tab)

/** 화면을 뒤지지 않아도 문제를 발견할 수 있게 — 이게 없으면 노선이 수십 개일 때 관리가 불가능하다. */
private fun buildAlerts(s: GameState): List<Alert> {
    val player = s.player
    val out = mutableListOf<Alert>()

    val idle = s.planesOf(player.id).count { it.routeId == null }
    if (idle > 0) {
        out += Alert("유휴 기재 ${idle}대", "노선에 배속하지 않으면 감가상각만 나갑니다.", Amber, Tab.FLEET)
    }

    val full = s.routesOf(player.id).filter { (it.last?.loadFactor ?: 0.0) >= 0.93 }
    if (full.isNotEmpty()) {
        val sample = full.first()
        out += Alert(
            "만석 노선 ${full.size}개",
            "${Cities.name(sample.from)}–${Cities.name(sample.to)} 등. 증편하거나 큰 기재로 바꿀 때입니다.",
            Mint,
            Tab.ROUTES,
        )
    }

    val bleeding = s.routesOf(player.id).filter { (it.last?.profit ?: 0.0) < 0 }
    if (bleeding.isNotEmpty()) {
        val worst = bleeding.minByOrNull { it.last?.profit ?: 0.0 }!!
        out += Alert(
            "적자 노선 ${bleeding.size}개",
            "${Cities.name(worst.from)}–${Cities.name(worst.to)} 분기 ${signedMoney(worst.last?.profit ?: 0.0)}. " +
                "운임을 내리거나 접어야 합니다.",
            Coral,
            Tab.ROUTES,
        )
    }

    val threat = s.livingAirlines
        .filter { it.id != player.id }
        .map { it to Stock.ownershipRatio(s, it.id, player.id) }
        .maxByOrNull { it.second }
    if (threat != null && threat.second >= 0.10) {
        out += Alert(
            "${threat.first.name}이 지분 ${percent(threat.second)} 보유",
            // 적자면 증자 자체가 막힌다 — 못 하는 수를 권하면 안 된다.
            if (Actions.canIssueShares(player)) {
                "50%를 넘기면 회사를 잃습니다. 유상증자로 희석하세요."
            } else {
                "50%를 넘기면 회사를 잃습니다. 적자라 증자가 막혀 있으니 실적부터 되돌리세요."
            },
            Coral,
            Tab.MARKET,
        )
    }

    if (player.cash < Economics.overhead(s, player) * 2) {
        out += Alert("현금 부족", "다음 분기 간접비를 감당하기 빠듯합니다. 차입이나 기재 매각을 검토하세요.", Coral, Tab.MANAGE)
    }

    val homeFree = s.freeSlots(player.id, player.home)
    if (homeFree <= 0 && s.unsoldSlots(Cities[player.home]) > 0) {
        out += Alert(
            "${Cities.name(player.home)} 슬롯 소진",
            "미분양 슬롯이 남아 있습니다. 경쟁사보다 먼저 확보하세요.",
            Amber,
            Tab.NETWORK,
        )
    }
    return out
}
