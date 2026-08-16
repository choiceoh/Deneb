package skytycoon.core.sim

import skytycoon.core.data.Cities
import skytycoon.core.model.City
import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.asin
import kotlin.math.atan2
import kotlin.math.cos
import kotlin.math.min
import kotlin.math.sin
import kotlin.math.sqrt

private const val EARTH_R = 6371.0

private fun rad(d: Double) = d * PI / 180.0
private fun deg(r: Double) = r * 180.0 / PI

data class MapPoint(val x: Double, val y: Double)

object Geo {
    /** 대권거리 (km) */
    fun greatCircle(a: City, b: City): Double {
        val dLat = rad(b.lat - a.lat)
        val dLon = rad(b.lon - a.lon)
        val s = sin(dLat / 2) * sin(dLat / 2) +
            cos(rad(a.lat)) * cos(rad(b.lat)) * sin(dLon / 2) * sin(dLon / 2)
        return 2 * EARTH_R * asin(min(1.0, sqrt(s)))
    }

    private val cache = HashMap<String, Double>(1024)

    /** 도시 id 두 개 사이의 거리 (캐시) */
    fun distance(a: String, b: String): Double {
        val key = if (a < b) "$a|$b" else "$b|$a"
        return cache.getOrPut(key) { greatCircle(Cities[a], Cities[b]) }
    }

    fun pairKey(a: String, b: String): String = if (a < b) "$a|$b" else "$b|$a"

    /** 등장방형 투영 — 0..1 로 정규화된 지도 좌표 */
    fun project(lat: Double, lon: Double) = MapPoint((lon + 180.0) / 360.0, (90.0 - lat) / 180.0)

    fun project(city: City) = project(city.lat, city.lon)

    /**
     * 대권 경로를 지도 위 폴리라인으로. 날짜변경선을 넘으면 경로가 두 조각으로 나뉜다.
     */
    fun greatCirclePath(a: City, b: City, steps: Int = 40): List<List<MapPoint>> {
        val f1 = rad(a.lat)
        val l1 = rad(a.lon)
        val f2 = rad(b.lat)
        val l2 = rad(b.lon)
        val d = 2 * asin(
            min(
                1.0,
                sqrt(
                    sin((f2 - f1) / 2) * sin((f2 - f1) / 2) +
                        cos(f1) * cos(f2) * sin((l2 - l1) / 2) * sin((l2 - l1) / 2),
                ),
            ),
        )
        val segments = mutableListOf<List<MapPoint>>()
        var current = mutableListOf<MapPoint>()
        var prevLon: Double? = null
        for (i in 0..steps) {
            val t = i.toDouble() / steps
            val lat: Double
            val lon: Double
            if (d < 1e-9) {
                lat = a.lat
                lon = a.lon
            } else {
                val ca = sin((1 - t) * d) / sin(d)
                val cb = sin(t * d) / sin(d)
                val x = ca * cos(f1) * cos(l1) + cb * cos(f2) * cos(l2)
                val y = ca * cos(f1) * sin(l1) + cb * cos(f2) * sin(l2)
                val z = ca * sin(f1) + cb * sin(f2)
                lat = deg(atan2(z, sqrt(x * x + y * y)))
                lon = deg(atan2(y, x))
            }
            if (prevLon != null && abs(lon - prevLon) > 180.0) {
                if (current.size > 1) segments.add(current)
                current = mutableListOf()
            }
            current.add(project(lat, lon))
            prevLon = lon
        }
        if (current.size > 1) segments.add(current)
        return segments
    }
}
