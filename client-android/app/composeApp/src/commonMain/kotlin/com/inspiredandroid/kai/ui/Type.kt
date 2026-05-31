package com.inspiredandroid.kai.ui

import androidx.compose.material3.Typography
import androidx.compose.runtime.Composable
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
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
 * Material 3 default type scale with every role re-pointed at Pretendard.
 * We keep the default sizes/line-heights/weights and only swap the family, so
 * components that read MaterialTheme.typography.* pick up Pretendard everywhere.
 */
@Composable
fun pretendardTypography(): Typography {
    val family = PretendardFontFamily()
    val base = Typography()
    return Typography(
        displayLarge = base.displayLarge.copy(fontFamily = family),
        displayMedium = base.displayMedium.copy(fontFamily = family),
        displaySmall = base.displaySmall.copy(fontFamily = family),
        headlineLarge = base.headlineLarge.copy(fontFamily = family),
        headlineMedium = base.headlineMedium.copy(fontFamily = family),
        headlineSmall = base.headlineSmall.copy(fontFamily = family),
        titleLarge = base.titleLarge.copy(fontFamily = family),
        titleMedium = base.titleMedium.copy(fontFamily = family),
        titleSmall = base.titleSmall.copy(fontFamily = family),
        bodyLarge = base.bodyLarge.copy(fontFamily = family),
        bodyMedium = base.bodyMedium.copy(fontFamily = family),
        bodySmall = base.bodySmall.copy(fontFamily = family),
        labelLarge = base.labelLarge.copy(fontFamily = family),
        labelMedium = base.labelMedium.copy(fontFamily = family),
        labelSmall = base.labelSmall.copy(fontFamily = family),
    )
}
