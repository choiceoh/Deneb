package com.inspiredandroid.kai.ui

import androidx.compose.material3.Typography
import androidx.compose.runtime.Composable
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp
import kai.composeapp.generated.resources.Res
import kai.composeapp.generated.resources.pretendard_bold
import kai.composeapp.generated.resources.pretendard_light
import kai.composeapp.generated.resources.pretendard_medium
import kai.composeapp.generated.resources.pretendard_regular
import kai.composeapp.generated.resources.pretendard_semibold
import org.jetbrains.compose.resources.Font

/**
 * Pretendard — the app's fixed UI typeface across every platform (Android, iOS,
 * desktop, web). Bundled as static OTF weights so text renders identically
 * regardless of the device's system font. Korean-first, so Pretendard's wide
 * Hangul coverage is the point. Code/terminal text stays FontFamily.Monospace
 * because those call sites override fontFamily locally.
 */
@Composable
fun PretendardFontFamily(): FontFamily = FontFamily(
    Font(Res.font.pretendard_light, FontWeight.Light),
    Font(Res.font.pretendard_regular, FontWeight.Normal),
    Font(Res.font.pretendard_medium, FontWeight.Medium),
    Font(Res.font.pretendard_semibold, FontWeight.SemiBold),
    Font(Res.font.pretendard_bold, FontWeight.Bold),
)

/**
 * Material 3 default type scale re-pointed at Pretendard and tuned for Korean.
 *
 * We keep the default sizes/line-heights/weights and swap the family, but also
 * zero out letterSpacing on every role: the M3 defaults carry small positive
 * tracking tuned for Roboto, which reads loose for Hangul. Pretendard is
 * designed for tight spacing, so 0 is the Korean-first choice.
 */
@Composable
fun pretendardTypography(): Typography {
    val family = PretendardFontFamily()
    val base = Typography()
    fun TextStyle.kr() = copy(fontFamily = family, letterSpacing = 0.sp)
    return Typography(
        displayLarge = base.displayLarge.kr(),
        displayMedium = base.displayMedium.kr(),
        displaySmall = base.displaySmall.kr(),
        headlineLarge = base.headlineLarge.kr(),
        headlineMedium = base.headlineMedium.kr(),
        headlineSmall = base.headlineSmall.kr(),
        titleLarge = base.titleLarge.kr(),
        titleMedium = base.titleMedium.kr(),
        titleSmall = base.titleSmall.kr(),
        bodyLarge = base.bodyLarge.kr(),
        bodyMedium = base.bodyMedium.kr(),
        bodySmall = base.bodySmall.kr(),
        labelLarge = base.labelLarge.kr(),
        labelMedium = base.labelMedium.kr(),
        labelSmall = base.labelSmall.kr(),
    )
}
