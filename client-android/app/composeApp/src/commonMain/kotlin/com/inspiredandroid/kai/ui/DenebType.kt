package com.inspiredandroid.kai.ui

import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.em
import androidx.compose.ui.unit.sp

/**
 * The Mini App's type scale, ported one role at a time from frontend/src/styles.css
 * so the native screens speak the same typographic language: ultralight giants for
 * menus/titles, tight tracking on big text, tracked caps for section labels, and a
 * light, airy body. This is the single source for Deneb screen typography — screens
 * should reach for these named styles instead of Material's `typography.*` roles,
 * which are tuned for a different (icon + card) idiom.
 *
 * Every style is Pretendard (the bundled UI face); only the size/weight/tracking
 * differ. Access is @Composable because the bundled font is a compose resource.
 */
object DenebType {
    private val family: FontFamily
        @Composable get() = PretendardFontFamily()

    /** Home/landing menu rows — the `.type-item` giants (52px / 200 / -0.035em). */
    val menuItem: TextStyle
        @Composable get() = TextStyle(fontFamily = family).copy(fontSize = 46.sp, lineHeight = 50.sp, fontWeight = FontWeight.ExtraLight, letterSpacing = (-0.035).em)

    /** Hero page title — `.view-title` (40px / 200 / -0.035em). */
    val viewTitle: TextStyle
        @Composable get() = TextStyle(fontFamily = family).copy(fontSize = 32.sp, lineHeight = 36.sp, fontWeight = FontWeight.ExtraLight, letterSpacing = (-0.035).em)

    /** Big content subject — `.email-subject` / `.wiki-title` (28px / 300 / -0.025em). */
    val subject: TextStyle
        @Composable get() = TextStyle(fontFamily = family).copy(fontSize = 26.sp, lineHeight = 32.sp, fontWeight = FontWeight.Light, letterSpacing = (-0.025).em)

    /** Tracked-caps section header — `.section-label` (12px / 600 / +0.08em, uppercased). */
    val sectionLabel: TextStyle
        @Composable get() = TextStyle(fontFamily = family).copy(fontSize = 12.sp, fontWeight = FontWeight.SemiBold, letterSpacing = 0.08.em)

    /** List-row primary line, read state — sender/title (15px / 300 / -0.01em). */
    val rowTitle: TextStyle
        @Composable get() = TextStyle(fontFamily = family).copy(fontSize = 15.sp, fontWeight = FontWeight.Normal, letterSpacing = (-0.01).em)

    /** List-row primary line, unread/emphasis — (15px / 600 / -0.015em). */
    val rowTitleStrong: TextStyle
        @Composable get() = TextStyle(fontFamily = family).copy(fontSize = 15.sp, fontWeight = FontWeight.SemiBold, letterSpacing = (-0.015).em)

    /** List-row secondary line — `.email-row-subject` (14px / 300). */
    val rowSubtitle: TextStyle
        @Composable get() = TextStyle(fontFamily = family).copy(fontSize = 14.sp, fontWeight = FontWeight.Light, letterSpacing = (-0.005).em)

    /** Row snippet/preview — `.email-row-snippet` (12px / +0.005em). */
    val snippet: TextStyle
        @Composable get() = TextStyle(fontFamily = family).copy(fontSize = 12.sp, fontWeight = FontWeight.Light, letterSpacing = 0.005.em)

    /** Timestamp / meta caption — `.email-row-time` (12px / +0.02em, tabular). */
    val meta: TextStyle
        @Composable get() = TextStyle(fontFamily = family).copy(fontSize = 12.sp, fontWeight = FontWeight.Normal, letterSpacing = 0.02.em)

    /** Body prose — `.analysis-card-body` (14px / 300 / 1.7 / -0.003em). */
    val body: TextStyle
        @Composable get() = TextStyle(fontFamily = family).copy(fontSize = 15.sp, lineHeight = 25.sp, fontWeight = FontWeight.Light, letterSpacing = (-0.003).em)

    /** Primary button label — `button.primary` (15px / 600). */
    val button: TextStyle
        @Composable get() = TextStyle(fontFamily = family).copy(fontSize = 15.sp, fontWeight = FontWeight.SemiBold)

    /** Hint / muted label — `.tg-hint` (13px / 500 / +0.04em). */
    val hint: TextStyle
        @Composable get() = TextStyle(fontFamily = family).copy(fontSize = 13.sp, fontWeight = FontWeight.Medium, letterSpacing = 0.04.em)
}
