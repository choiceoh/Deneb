package skytycoon.ui.screens

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import skytycoon.core.model.QuarterResult
import skytycoon.ui.Amber
import skytycoon.ui.GameViewModel
import skytycoon.ui.TextHigh
import skytycoon.ui.TextLow
import skytycoon.ui.TextMid
import skytycoon.ui.components.KeyValue
import skytycoon.ui.components.VSpace
import skytycoon.ui.loadFactorColor
import skytycoon.ui.money
import skytycoon.ui.moneyShort
import skytycoon.ui.percent
import skytycoon.ui.profitColor
import skytycoon.ui.signedMoney

/** 분기 결산. 숫자를 늘어놓기만 하면 안 읽히므로 매출 → 비용 → 순익 순서로 이야기를 만든다. */
@Composable
fun QuarterReportDialog(vm: GameViewModel, report: QuarterResult, onDismiss: () -> Unit) {
    val s = vm.game
    val year = s.startYear + report.turn / 4
    val quarter = report.turn % 4 + 1

    AlertDialog(
        onDismissRequest = onDismiss,
        title = {
            Column {
                Text("${year}년 ${quarter}분기 결산", fontWeight = FontWeight.Bold, fontSize = 18.sp)
                Text(
                    "순익 ${signedMoney(report.net)}",
                    color = profitColor(report.net),
                    fontSize = 15.sp,
                    fontWeight = FontWeight.Bold,
                )
            }
        },
        text = {
            Column(Modifier.heightIn(max = 440.dp).verticalScroll(rememberScrollState())) {
                Text("수입", color = Amber, fontSize = 12.sp, fontWeight = FontWeight.Bold)
                KeyValue("여객 매출", moneyShort(report.revenue))
                KeyValue("화물 매출", moneyShort(report.cargoRevenue))
                KeyValue("부대사업", moneyShort(report.businessIncome))
                KeyValue("합계", moneyShort(report.totalRevenue), TextHigh)

                VSpace(10)
                HorizontalDivider()
                VSpace(10)
                Text("비용", color = Amber, fontSize = 12.sp, fontWeight = FontWeight.Bold)
                KeyValue("연료", moneyShort(report.fuelCost))
                KeyValue("승무원", moneyShort(report.crewCost))
                KeyValue("정비", moneyShort(report.maintCost))
                KeyValue("공항·항행", moneyShort(report.landingCost))
                KeyValue("기내 서비스", moneyShort(report.paxServiceCost))
                KeyValue("판매·유통", moneyShort(report.distributionCost))
                KeyValue("본사 간접비", moneyShort(report.overhead))
                KeyValue("광고", moneyShort(report.adSpend))
                KeyValue("감가상각", moneyShort(report.depreciation))
                KeyValue("이자", moneyShort(report.interestCost))
                KeyValue("법인세", moneyShort(report.tax))
                KeyValue("합계", moneyShort(report.operatingCost + report.interestCost + report.tax), TextHigh)

                VSpace(10)
                HorizontalDivider()
                VSpace(10)
                Text("운송 실적", color = Amber, fontSize = 12.sp, fontWeight = FontWeight.Bold)
                KeyValue("수송객", "${money(report.pax)}명")
                KeyValue("탑승률", percent(report.loadFactor), loadFactorColor(report.loadFactor))
                KeyValue("기말 현금", moneyShort(report.cash))
                KeyValue("부채", moneyShort(report.debt))
                KeyValue("자기자본", moneyShort(report.equity))

                val recent = s.player.results.takeLast(8)
                if (recent.size >= 2) {
                    VSpace(12)
                    Text("최근 순익 추이", color = TextLow, fontSize = 11.sp)
                    VSpace(4)
                    Row(Modifier.fillMaxWidth()) {
                        for (r in recent) {
                            Column(Modifier.weight(1f)) {
                                Text(
                                    signedMoney(r.net),
                                    color = profitColor(r.net),
                                    fontSize = 9.sp,
                                )
                                Text(
                                    "${(s.startYear + r.turn / 4) % 100}Q${r.turn % 4 + 1}",
                                    color = TextMid,
                                    fontSize = 8.sp,
                                )
                            }
                        }
                    }
                }
            }
        },
        confirmButton = { Button(onClick = onDismiss) { Text("확인") } },
    )
}
