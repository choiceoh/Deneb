package skytycoon.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AccountBalance
import androidx.compose.material.icons.filled.Article
import androidx.compose.material.icons.filled.Flight
import androidx.compose.material.icons.filled.Public
import androidx.compose.material.icons.filled.ShowChart
import androidx.compose.material.icons.filled.Speed
import androidx.compose.material.icons.filled.Timeline
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationRail
import androidx.compose.material3.NavigationRailItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import skytycoon.core.sim.Economics
import skytycoon.core.sim.TurnEngine
import skytycoon.ui.screens.DashboardScreen
import skytycoon.ui.screens.FleetScreen
import skytycoon.ui.screens.ManageScreen
import skytycoon.ui.screens.MarketScreen
import skytycoon.ui.screens.NetworkScreen
import skytycoon.ui.screens.NewsScreen
import skytycoon.ui.screens.QuarterReportDialog
import skytycoon.ui.screens.RoutesScreen
import skytycoon.ui.screens.SetupScreen

enum class Tab(val label: String, val icon: ImageVector) {
    DASHBOARD("경영", Icons.Filled.Speed),
    NETWORK("노선망", Icons.Filled.Public),
    ROUTES("노선", Icons.Filled.Timeline),
    FLEET("기재", Icons.Filled.Flight),
    MARKET("시장", Icons.Filled.ShowChart),
    MANAGE("재무", Icons.Filled.AccountBalance),
    NEWS("뉴스", Icons.Filled.Article),
}

@Composable
fun App(vm: GameViewModel = remember { GameViewModel() }, initialTab: Tab = Tab.DASHBOARD) {
    SkyTycoonTheme {
        if (vm.state == null) {
            SetupScreen(
                canResume = vm.hasSavedGame(),
                onResume = { vm.resume() },
                onStart = { scenario, company, difficulty -> vm.start(scenario, company, difficulty) },
            )
        } else {
            GameShell(vm, initialTab)
        }
    }
}

@Composable
private fun GameShell(vm: GameViewModel, initialTab: Tab) {
    var tab by remember { mutableStateOf(initialTab) }
    val snackbar = remember { SnackbarHostState() }

    LaunchedEffect(vm.message) {
        vm.message?.let {
            snackbar.showSnackbar(it)
            vm.message = null
        }
    }

    BoxWithConstraints(Modifier.fillMaxSize().background(Ink)) {
        val wide = maxWidth >= 860.dp

        Scaffold(
            containerColor = Ink,
            snackbarHost = { SnackbarHost(snackbar) },
            topBar = { TopStatusBar(vm, wide) },
            bottomBar = {
                if (!wide) {
                    NavigationBar(containerColor = InkPanel) {
                        for (t in Tab.entries) {
                            NavigationBarItem(
                                selected = tab == t,
                                onClick = { tab = t },
                                icon = { Icon(t.icon, contentDescription = t.label) },
                                label = { Text(t.label, fontSize = 10.sp) },
                            )
                        }
                    }
                }
            },
        ) { padding ->
            Row(Modifier.fillMaxSize().padding(padding)) {
                if (wide) {
                    NavigationRail(containerColor = InkPanel) {
                        for (t in Tab.entries) {
                            NavigationRailItem(
                                selected = tab == t,
                                onClick = { tab = t },
                                icon = { Icon(t.icon, contentDescription = t.label) },
                                label = { Text(t.label, fontSize = 10.sp) },
                            )
                        }
                    }
                }
                Box(Modifier.weight(1f).fillMaxSize()) {
                    when (tab) {
                        Tab.DASHBOARD -> DashboardScreen(vm, wide, onGoTo = { tab = it })
                        Tab.NETWORK -> NetworkScreen(vm, wide)
                        Tab.ROUTES -> RoutesScreen(vm, wide)
                        Tab.FLEET -> FleetScreen(vm, wide)
                        Tab.MARKET -> MarketScreen(vm, wide)
                        Tab.MANAGE -> ManageScreen(vm, wide)
                        Tab.NEWS -> NewsScreen(vm)
                    }
                }
            }
        }

        vm.pendingReport?.let { report ->
            QuarterReportDialog(vm, report, onDismiss = vm::dismissReport)
        }
    }
}

/**
 * 상단 상태바. 폰 세로가 기준이라 좁을 때는 이름·턴 줄과 지표 줄을 나눠 쌓고,
 * 지표는 가로 스크롤로 흘려보낸다 (한 줄에 욱여넣으면 글자가 세로로 쪼개진다).
 */
@Composable
private fun TopStatusBar(vm: GameViewModel, wide: Boolean) {
    val s = vm.game
    val player = s.player
    // 회사가 사라지면 순위표에서 빠져 -1 이 나온다. 그때는 확정된 최종 순위를 쓴다.
    val rank = s.outcome?.rank
        ?: TurnEngine.ranking(s).indexOfFirst { it.first.id == player.id }
            .let { if (it < 0) s.livingAirlines.size + 1 else it + 1 }
    val last = player.lastResult

    val stats: @Composable () -> Unit = {
        StatChip("현금", moneyShort(player.cash), TextHigh)
        StatChip("기업가치", moneyShort(Economics.equity(s, player)), TextHigh)
        StatChip(
            "지난 분기",
            last?.let { signedMoney(it.net) } ?: "—",
            last?.let { profitColor(it.net) } ?: TextMid,
        )
        StatChip("순위", "${rank}위", Amber)
    }

    val turnButton: @Composable () -> Unit = {
        if (s.outcome != null) {
            Button(onClick = vm::quit) { Text("새 게임") }
        } else {
            Button(onClick = vm::endTurn, enabled = !vm.busy) {
                if (vm.busy) {
                    CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
                } else {
                    Text("분기 진행 ▶", fontWeight = FontWeight.Bold)
                }
            }
        }
    }

    // 안드로이드는 enableEdgeToEdge 로 그리므로, 직접 만든 상단 바는 상태표시줄 인셋을
    // 스스로 챙겨야 한다. 안 그러면 시계·노치가 회사명과 분기 진행 버튼을 덮는다
    // (Material TopAppBar 와 달리 이 Column 은 인셋을 모른다). 데스크톱에서는 0 이다.
    Column(
        Modifier
            .fillMaxWidth()
            .background(InkPanel)
            .statusBarsPadding()
            .padding(horizontal = 14.dp, vertical = 8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(player.name, color = TextHigh, fontSize = 16.sp, fontWeight = FontWeight.Bold)
                Text(quarterLabel(s.displayYear, s.displayQuarter), color = TextMid, fontSize = 11.sp)
            }
            if (wide) {
                Row(horizontalArrangement = Arrangement.spacedBy(22.dp)) { stats() }
                Box(Modifier.size(22.dp))
            }
            turnButton()
        }
        if (!wide) {
            Box(Modifier.size(8.dp))
            Row(
                Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
                horizontalArrangement = Arrangement.spacedBy(18.dp),
            ) { stats() }
        }
    }
}

@Composable
private fun StatChip(label: String, value: String, color: androidx.compose.ui.graphics.Color) {
    Column {
        Text(label, color = TextLow, fontSize = 10.sp, maxLines = 1)
        Text(value, color = color, fontSize = 14.sp, fontWeight = FontWeight.SemiBold, maxLines = 1)
    }
}
