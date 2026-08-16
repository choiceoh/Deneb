package skytycoon.ui

import kotlin.math.abs
import kotlin.math.roundToLong

/** common 소스셋에는 String.format 이 없어서 직접 만든다. */
fun decimals(value: Double, digits: Int): String {
    if (value.isNaN() || value.isInfinite()) return "-"
    var scale = 1L
    repeat(digits) { scale *= 10 }
    val scaled = (value * scale).roundToLong()
    val sign = if (scaled < 0) "-" else ""
    val a = abs(scaled)
    val whole = a / scale
    if (digits == 0) return "$sign${grouped(whole)}"
    val frac = (a % scale).toString().padStart(digits, '0')
    return "$sign${grouped(whole)}.$frac"
}

fun grouped(v: Long): String {
    val s = v.toString()
    if (s.length <= 3) return s
    val sb = StringBuilder()
    for ((i, c) in s.withIndex()) {
        if (i > 0 && (s.length - i) % 3 == 0) sb.append(',')
        sb.append(c)
    }
    return sb.toString()
}

/** 달러 금액을 한국식 억/만 단위로. */
fun money(value: Double): String {
    val sign = if (value < 0) "-" else ""
    val v = abs(value)
    return when {
        v >= 1e12 -> "$sign${decimals(v / 1e12, 2)}조"
        v >= 1e8 -> "$sign${decimals(v / 1e8, if (v >= 1e10) 0 else 1)}억"
        v >= 1e4 -> "$sign${decimals(v / 1e4, 0)}만"
        else -> "$sign${decimals(v, 0)}"
    }
}

/** 표에 쓰는 짧은 금액 ($ 접두). */
fun moneyShort(value: Double): String = "$" + money(value)

/** 손익처럼 부호가 중요한 값. */
fun signedMoney(value: Double): String = (if (value > 0) "+" else "") + moneyShort(value)

fun percent(ratio: Double, digits: Int = 0): String = "${decimals(ratio * 100, digits)}%"

fun km(value: Double): String = "${grouped(value.roundToLong())}km"

fun people(value: Double): String = "${grouped(value.roundToLong())}명"

fun quarterLabel(year: Int, quarter: Int): String = "${year}년 ${quarter}분기"
