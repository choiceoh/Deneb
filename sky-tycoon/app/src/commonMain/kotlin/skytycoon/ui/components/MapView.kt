package skytycoon.ui.components

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.gestures.detectTransformGestures
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.unit.dp
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.text.TextMeasurer
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.drawText
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp
import skytycoon.core.data.Cities
import skytycoon.core.model.City
import skytycoon.core.model.GameState
import skytycoon.core.sim.Geo
import skytycoon.ui.Land
import skytycoon.ui.LandEdge
import skytycoon.ui.Ocean
import skytycoon.ui.TextHigh
import skytycoon.ui.TextMid
import skytycoon.ui.WorldShapes
import kotlin.math.abs
import kotlin.math.max
import kotlin.math.min
import kotlin.math.sqrt

private const val LAT_SPAN = WorldShapes.LAT_TOP - WorldShapes.LAT_BOTTOM

/**
 * 등장방형 투영을 화면에 맞춘 변환. 가로세로 비를 지키지 않으면 대륙이 세로로 늘어나
 * 어디가 어딘지 알아볼 수 없게 된다 — 그래서 축척은 한 값만 쓰고 남는 쪽은 바다로 채운다.
 */
private class MapTransform(val size: Size, zoom: Float, pan: Offset) {
    val scale: Double = min(size.width / 360.0, size.height / LAT_SPAN) * zoom
    private val contentW = 360.0 * scale
    private val contentH = LAT_SPAN * scale
    private val tx = (size.width - contentW) / 2.0 + pan.x
    private val ty = (size.height - contentH) / 2.0 + pan.y

    fun px(lon: Double, lat: Double) = Offset(
        (tx + (lon + 180.0) * scale).toFloat(),
        (ty + (WorldShapes.LAT_TOP - lat) * scale).toFloat(),
    )

    fun px(city: City) = px(city.lon, city.lat)

    /** Geo 가 주는 0..1 정규 좌표(위도 90..-90 기준)를 지도 좌표로. */
    fun fromNorm(x: Double, y: Double) = px(x * 360.0 - 180.0, 90.0 - y * 180.0)

    companion object {
        /** 지도가 화면 밖으로 완전히 도망가지 않도록 이동량을 제한한다. */
        fun clampPan(size: Size, zoom: Float, pan: Offset): Offset {
            val scale = min(size.width / 360.0, size.height / LAT_SPAN) * zoom
            val overX = max(0.0, 360.0 * scale - size.width) / 2.0
            val overY = max(0.0, LAT_SPAN * scale - size.height) / 2.0
            return Offset(
                pan.x.coerceIn(-overX.toFloat(), overX.toFloat()),
                pan.y.coerceIn(-overY.toFloat(), overY.toFloat()),
            )
        }
    }
}

/**
 * 세계 노선도. 이 게임에서 가장 오래 보는 화면이라 정보 밀도보다 읽히는 속도를 우선했다.
 * 내 노선은 밝게, 경쟁사 노선은 흐리게 — 어디가 비어 있는지가 한눈에 보여야 한다.
 */
@Composable
fun WorldMap(
    state: GameState,
    selectedCity: String?,
    showRivals: Boolean,
    onSelectCity: (String?) -> Unit,
    modifier: Modifier = Modifier,
    initialZoom: Float = 1f,
    initialCenterLon: Double? = null,
) {
    val measurer = rememberTextMeasurer()
    var zoom by remember { mutableStateOf(initialZoom) }
    var pan by remember { mutableStateOf(Offset.Zero) }
    var centered by remember { mutableStateOf(initialCenterLon == null) }

    Canvas(
        modifier
            .onSizeChanged { px ->
                if (centered || px.width == 0) return@onSizeChanged
                val canvas = Size(px.width.toFloat(), px.height.toFloat())
                val scale = min(canvas.width / 360.0, canvas.height / LAT_SPAN) * zoom
                val wanted = 360.0 * scale / 2.0 - (initialCenterLon!! + 180.0) * scale
                pan = MapTransform.clampPan(canvas, zoom, Offset(wanted.toFloat(), 0f))
                centered = true
            }
            .pointerInput(Unit) {
                detectTransformGestures { _, panChange, zoomChange, _ ->
                    val canvas = Size(size.width.toFloat(), size.height.toFloat())
                    zoom = (zoom * zoomChange).coerceIn(1f, 6f)
                    pan = MapTransform.clampPan(canvas, zoom, pan + panChange)
                }
            }
            .pointerInput(Unit) {
                detectTapGestures { tap ->
                    val t = MapTransform(Size(size.width.toFloat(), size.height.toFloat()), zoom, pan)
                    var best: City? = null
                    var bestDist = Float.MAX_VALUE
                    for (c in Cities.all) {
                        val p = t.px(c)
                        val d = sqrt((p.x - tap.x) * (p.x - tap.x) + (p.y - tap.y) * (p.y - tap.y))
                        if (d < bestDist) {
                            bestDist = d
                            best = c
                        }
                    }
                    // 포인터 좌표는 물리 픽셀이다. 30f 로 고정하면 xxhdpi 폰에서
                    // 8~10dp 밖에 안 돼 도시를 거의 못 누른다 — 기준 타깃이 폰인데.
                    val hitRadius = 22.dp.toPx()
                    onSelectCity(if (bestDist <= hitRadius) best?.id else null)
                }
            },
    ) {
        val t = MapTransform(size, zoom, pan)
        drawRect(Ocean, size = size)
        drawGraticule(t)

        for (poly in WorldShapes.landmasses) {
            val path = Path()
            var i = 0
            while (i < poly.size) {
                val p = t.px(poly[i], poly[i + 1])
                if (i == 0) path.moveTo(p.x, p.y) else path.lineTo(p.x, p.y)
                i += 2
            }
            path.close()
            drawPath(path, Land)
            drawPath(path, LandEdge, style = Stroke(width = 1f))
        }

        val playerId = state.playerId
        val playerColor = state.airlineOrNull(playerId)?.let { Color(it.colorArgb) } ?: TextHigh

        if (showRivals) {
            for (route in state.routes) {
                if (route.airlineId == playerId || !route.active) continue
                val airline = state.airlineOrNull(route.airlineId) ?: continue
                if (!airline.alive) continue
                drawRoute(t, route.from, route.to, Color(airline.colorArgb).copy(alpha = 0.32f), 1.2f)
            }
        }
        for (route in state.routes) {
            if (route.airlineId != playerId || !route.active) continue
            drawRoute(t, route.from, route.to, playerColor.copy(alpha = 0.95f), 2.2f)
        }

        for (city in Cities.all) {
            val p = t.px(city)
            val weight = (city.econ + city.tour) / 2.0
            val radius = (2.6f + (weight / 100.0 * 4.2)).toFloat()
            val mine = state.airlineOrNull(playerId)?.slotsAt(city.id) ?: 0
            val closed = state.cityState[city.id]?.isClosed(state.turn) == true

            val fill = when {
                closed -> Color(0xFF7A3232)
                mine > 0 -> playerColor
                else -> Color(city.region.colorArgb).copy(alpha = 0.8f)
            }
            if (mine > 0) drawCircle(playerColor.copy(alpha = 0.20f), radius + 5f, p)
            drawCircle(fill, radius, p)
            drawCircle(Ocean, radius, p, style = Stroke(width = 1f))
            if (city.id == selectedCity) {
                drawCircle(TextHigh, radius + 8f, p, style = Stroke(width = 1.6f))
            }
        }

        // 라벨은 겹치면 못 읽는다. 중요한 순서로 그리되 자리가 겹치면 건너뛴다.
        val drawn = mutableListOf<Offset>()
        val ranked = Cities.all.sortedByDescending { c ->
            var score = c.econ
            if ((state.airlineOrNull(playerId)?.slotsAt(c.id) ?: 0) > 0) score += 120.0
            if (c.id == selectedCity) score += 1000.0
            score
        }
        for (city in ranked) {
            val mine = (state.airlineOrNull(playerId)?.slotsAt(city.id) ?: 0) > 0
            if (!mine && city.econ < 62 && city.id != selectedCity) continue
            val p = t.px(city)
            if (p.x < -40 || p.y < -20 || p.x > size.width + 40 || p.y > size.height + 20) continue
            if (drawn.any { abs(it.x - p.x) < 40f && abs(it.y - p.y) < 13f }) continue
            drawn += p
            drawCityLabel(measurer, city.name, p, city.id == selectedCity)
        }
    }
}

private fun DrawScope.drawGraticule(t: MapTransform) {
    val grid = Color(0xFF16202F)
    var lon = -150.0
    while (lon <= 150.0) {
        val a = t.px(lon, WorldShapes.LAT_TOP)
        val b = t.px(lon, WorldShapes.LAT_BOTTOM)
        drawLine(grid, a, b, strokeWidth = 1f)
        lon += 30.0
    }
    var lat = WorldShapes.LAT_TOP - 12.0
    while (lat > WorldShapes.LAT_BOTTOM) {
        val a = t.px(-180.0, lat)
        val b = t.px(180.0, lat)
        drawLine(grid, a, b, strokeWidth = 1f)
        lat -= 20.0
    }
    drawLine(Color(0xFF203046), t.px(-180.0, 0.0), t.px(180.0, 0.0), strokeWidth = 1.4f)
}

private fun DrawScope.drawRoute(t: MapTransform, from: String, to: String, color: Color, width: Float) {
    for (seg in Geo.greatCirclePath(Cities[from], Cities[to])) {
        if (seg.size < 2) continue
        val path = Path()
        for ((i, pt) in seg.withIndex()) {
            val p = t.fromNorm(pt.x, pt.y)
            if (i == 0) path.moveTo(p.x, p.y) else path.lineTo(p.x, p.y)
        }
        drawPath(path, color, style = Stroke(width = width))
    }
}

private fun DrawScope.drawCityLabel(measurer: TextMeasurer, name: String, at: Offset, highlighted: Boolean) {
    val style = TextStyle(
        color = if (highlighted) TextHigh else TextMid,
        fontSize = if (highlighted) 12.sp else 10.sp,
        fontWeight = if (highlighted) FontWeight.Bold else FontWeight.Normal,
    )
    val layout = measurer.measure(name, style)
    drawText(layout, topLeft = Offset(at.x - layout.size.width / 2f, at.y + 7f))
}
