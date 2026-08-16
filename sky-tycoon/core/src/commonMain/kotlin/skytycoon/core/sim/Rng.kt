package skytycoon.core.sim

/**
 * 결정론적 난수 (mulberry32). 세이브/리플레이가 재현되어야 하므로 플랫폼 난수는 쓰지 않는다.
 * 상태는 Int 하나뿐이라 GameState 에 그대로 실려 저장된다.
 */
class Rng(seed: Int) {
    var state: Int = seed
        private set

    fun nextDouble(): Double {
        state += 0x6D2B79F5
        var t = state
        t = (t xor (t ushr 15)) * (t or 1)
        t = t xor (t + (t xor (t ushr 7)) * (t or 61))
        return ((t xor (t ushr 14)).toLong() and 0xFFFFFFFFL).toDouble() / 4294967296.0
    }

    fun range(min: Double, max: Double): Double = min + nextDouble() * (max - min)

    fun int(min: Int, max: Int): Int = min + (nextDouble() * (max - min + 1)).toInt().coerceAtMost(max - min)

    fun chance(p: Double): Boolean = nextDouble() < p

    fun <T> pick(items: List<T>): T = items[(nextDouble() * items.size).toInt().coerceAtMost(items.size - 1)]

    fun <T> pickOrNull(items: List<T>): T? = if (items.isEmpty()) null else pick(items)

    /** 평균 0, 표준편차 약 1 의 근사 정규분포 */
    fun normal(): Double {
        var s = 0.0
        repeat(6) { s += nextDouble() }
        return (s - 3.0) / 0.7071
    }

    fun <T> shuffled(items: List<T>): List<T> {
        val out = items.toMutableList()
        for (i in out.indices.reversed()) {
            val j = (nextDouble() * (i + 1)).toInt().coerceAtMost(i)
            val tmp = out[i]
            out[i] = out[j]
            out[j] = tmp
        }
        return out
    }

    companion object {
        fun fromString(s: String): Rng {
            var h = 2166136261u.toInt()
            for (c in s) {
                h = h xor c.code
                h *= 16777619
            }
            return Rng(h)
        }
    }
}
