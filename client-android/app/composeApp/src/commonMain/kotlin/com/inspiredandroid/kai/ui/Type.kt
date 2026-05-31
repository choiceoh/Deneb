package com.inspiredandroid.kai.ui

import androidx.compose.material3.Typography
import androidx.compose.runtime.Composable
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.em
import kai.composeapp.generated.resources.Res
import kai.composeapp.generated.resources.pretendard_bold
import kai.composeapp.generated.resources.pretendard_extralight
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
    Font(Res.font.pretendard_extralight, FontWeight.ExtraLight),
    Font(Res.font.pretendard_light, FontWeight.Light),
    Font(Res.font.pretendard_regular, FontWeight.Normal),
    Font(Res.font.pretendard_medium, FontWeight.Medium),
    Font(Res.font.pretendard_semibold, FontWeight.SemiBold),
    Font(Res.font.pretendard_bold, FontWeight.Bold),
)

/**
 * Material 3 type scale re-pointed at Pretendard, with the Mini App's tight
 * letter-spacing grafted on for the same typographic feel: negative tracking
 * that tightens as text grows (display/headline), easing to a hair under zero
 * for body and labels. Korean-safe — Hangul reads well slightly tightened, and
 * we never apply the Latin all-caps positive tracking the Mini App uses for
 * small uppercase labels (it would spread 한글 syllables).
 */
@Composable
fun pretendardTypography(): Typography {
    val family = PretendardFontFamily()
    val base = Typography()
    return Typography(
        displayLarge = base.displayLarge.copy(fontFamily = family, letterSpacing = (-0.03).em),
        displayMedium = base.displayMedium.copy(fontFamily = family, letterSpacing = (-0.03).em),
        displaySmall = base.displaySmall.copy(fontFamily = family, letterSpacing = (-0.025).em),
        headlineLarge = base.headlineLarge.copy(fontFamily = family, letterSpacing = (-0.025).em),
        headlineMedium = base.headlineMedium.copy(fontFamily = family, letterSpacing = (-0.02).em),
        headlineSmall = base.headlineSmall.copy(fontFamily = family, letterSpacing = (-0.02).em),
        titleLarge = base.titleLarge.copy(fontFamily = family, letterSpacing = (-0.015).em),
        titleMedium = base.titleMedium.copy(fontFamily = family, letterSpacing = (-0.01).em),
        titleSmall = base.titleSmall.copy(fontFamily = family, letterSpacing = (-0.01).em),
        bodyLarge = base.bodyLarge.copy(fontFamily = family, letterSpacing = (-0.005).em),
        bodyMedium = base.bodyMedium.copy(fontFamily = family, letterSpacing = (-0.005).em),
        bodySmall = base.bodySmall.copy(fontFamily = family, letterSpacing = (-0.005).em),
        labelLarge = base.labelLarge.copy(fontFamily = family, letterSpacing = (-0.005).em),
        labelMedium = base.labelMedium.copy(fontFamily = family, letterSpacing = (-0.005).em),
        labelSmall = base.labelSmall.copy(fontFamily = family, letterSpacing = (-0.005).em),
    )
}
