package skytycoon.ui

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

// 야간 비행 계기판 같은 어두운 팔레트. 지도 위 노선 색이 살아야 하므로 배경은 최대한 눌러 둔다.
val Ink = Color(0xFF0B1220)
val InkPanel = Color(0xFF141C2B)
val InkPanelHigh = Color(0xFF1D2637)
val Sky = Color(0xFF4F9CF7)
val SkyDim = Color(0xFF2C4763)
val Amber = Color(0xFFE8B04B)
val Mint = Color(0xFF44C7A0)
val Coral = Color(0xFFE8695F)
val TextHigh = Color(0xFFE9EEF6)
val TextMid = Color(0xFF9DAABF)
val TextLow = Color(0xFF6B7A91)
val Ocean = Color(0xFF0E1826)
val Land = Color(0xFF1E2A3C)
val LandEdge = Color(0xFF2C3B52)

val Profit = Mint
val Loss = Coral

private val scheme = darkColorScheme(
    primary = Sky,
    onPrimary = Color(0xFF06101E),
    secondary = Amber,
    onSecondary = Color(0xFF1A1200),
    background = Ink,
    onBackground = TextHigh,
    surface = InkPanel,
    onSurface = TextHigh,
    surfaceVariant = InkPanelHigh,
    onSurfaceVariant = TextMid,
    error = Coral,
    outline = Color(0xFF32405A),
)

@Composable
fun SkyTycoonTheme(content: @Composable () -> Unit) {
    MaterialTheme(colorScheme = scheme, content = content)
}

/** 손익 색 — 흑자는 민트, 적자는 산호색. */
fun profitColor(value: Double): Color = if (value >= 0) Profit else Loss

/** 탑승률 색 — 만석에 가까울수록 경고색(증편 신호). */
fun loadFactorColor(lf: Double): Color = when {
    lf >= 0.92 -> Amber
    lf >= 0.65 -> Mint
    lf >= 0.45 -> Sky
    else -> Coral
}
