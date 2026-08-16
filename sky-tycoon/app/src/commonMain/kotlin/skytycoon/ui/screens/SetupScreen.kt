package skytycoon.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
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
import skytycoon.core.data.Cities
import skytycoon.core.data.Companies
import skytycoon.core.data.Difficulties
import skytycoon.core.data.Scenarios
import skytycoon.ui.Amber
import skytycoon.ui.Ink
import skytycoon.ui.InkPanel
import skytycoon.ui.TextHigh
import skytycoon.ui.TextLow
import skytycoon.ui.TextMid
import skytycoon.ui.components.VSpace
import skytycoon.ui.moneyShort

@Composable
fun SetupScreen(onStart: (String, String, String) -> Unit) {
    var scenario by remember { mutableStateOf(Scenarios.all.first().id) }
    var company by remember { mutableStateOf(Companies.all.first().id) }
    var difficulty by remember { mutableStateOf("normal") }

    Box(Modifier.fillMaxSize().background(Ink), contentAlignment = Alignment.TopCenter) {
        Column(
            Modifier
                .widthIn(max = 900.dp)
                .verticalScroll(rememberScrollState())
                .padding(28.dp),
        ) {
            Text("SKY TYCOON", color = TextHigh, fontSize = 34.sp, fontWeight = FontWeight.Black, letterSpacing = 4.sp)
            Text("항공사 경영 시뮬레이션", color = Amber, fontSize = 14.sp, fontWeight = FontWeight.Medium)
            VSpace(6)
            Text(
                "노선을 열고, 슬롯을 사들이고, 기재를 갈아 끼우며 하늘의 패권을 다툰다.\n" +
                    "20년 뒤 기업가치 1위에 오르거나, 경쟁사를 전부 삼키면 승리한다.",
                color = TextMid,
                fontSize = 13.sp,
                lineHeight = 20.sp,
            )
            VSpace(26)

            SetupSection("시대를 고르세요") {
                for (sc in Scenarios.all) {
                    SelectCard(
                        selected = scenario == sc.id,
                        title = "${sc.name} · ${sc.startYear}–${sc.startYear + sc.years}",
                        body = sc.desc,
                        onClick = { scenario = sc.id },
                    )
                }
            }

            VSpace(20)
            SetupSection("회사를 고르세요") {
                for (co in Companies.all) {
                    val home = Cities[co.home]
                    val fleet = co.startFleet.sumOf { it.second }
                    SelectCard(
                        selected = company == co.id,
                        accent = Color(co.colorArgb),
                        title = "${co.name} · ${home.name} (${home.code})",
                        body = "${co.bonusLabel} — ${co.bonusDesc}",
                        meta = "자본 ${moneyShort(co.cash)} · 기재 ${fleet}대 · " +
                            "허브 규모 ${hubGrade(home.slots, home.econ)} · 성향 ${co.trait.label}",
                        onClick = { company = co.id },
                    )
                }
            }

            VSpace(20)
            SetupSection("난이도") {
                Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    for (d in Difficulties.all) {
                        Box(
                            Modifier
                                .weight(1f)
                                .clip(RoundedCornerShape(10.dp))
                                .background(if (difficulty == d.id) Amber.copy(alpha = 0.18f) else InkPanel)
                                .border(
                                    1.dp,
                                    if (difficulty == d.id) Amber else Color(0xFF243043),
                                    RoundedCornerShape(10.dp),
                                )
                                .clickable { difficulty = d.id }
                                .padding(vertical = 12.dp),
                            contentAlignment = Alignment.Center,
                        ) {
                            Text(
                                d.name,
                                color = if (difficulty == d.id) Amber else TextMid,
                                fontWeight = FontWeight.SemiBold,
                            )
                        }
                    }
                }
            }

            VSpace(26)
            Button(
                onClick = { onStart(scenario, company, difficulty) },
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text("취항하기", fontSize = 16.sp, fontWeight = FontWeight.Bold, modifier = Modifier.padding(6.dp))
            }
            VSpace(30)
        }
    }
}

/** 모기지의 크기는 곧 시작 난이도다. 고르기 전에 알려주는 게 공정하다. */
private fun hubGrade(slots: Int, econ: Double): String {
    val score = slots + econ
    return when {
        score >= 175 -> "최상 (수월)"
        score >= 140 -> "상 (무난)"
        score >= 120 -> "중 (도전)"
        else -> "하 (가시밭길)"
    }
}

@Composable
private fun SetupSection(title: String, content: @Composable () -> Unit) {
    Text(title, color = TextLow, fontSize = 12.sp, fontWeight = FontWeight.Bold, letterSpacing = 1.sp)
    VSpace(10)
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) { content() }
}

@Composable
private fun SelectCard(
    selected: Boolean,
    title: String,
    body: String,
    meta: String? = null,
    accent: Color = Amber,
    onClick: () -> Unit,
) {
    Column(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(if (selected) accent.copy(alpha = 0.14f) else InkPanel)
            .border(1.dp, if (selected) accent else Color(0xFF243043), RoundedCornerShape(12.dp))
            .clickable { onClick() }
            .padding(14.dp),
    ) {
        Text(title, color = if (selected) accent else TextHigh, fontSize = 15.sp, fontWeight = FontWeight.Bold)
        VSpace(4)
        Text(body, color = TextMid, fontSize = 12.sp, lineHeight = 18.sp)
        if (meta != null) {
            VSpace(6)
            Text(meta, color = TextLow, fontSize = 11.sp)
        }
    }
}
