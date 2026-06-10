package ai.deneb.ui

import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.em
import androidx.compose.ui.unit.sp

/**
 * The Mini App's type scale, ported from frontend/src/styles.css so native screens
 * speak the same typographic language. This is the single source for Deneb screen
 * typography — reach for these named, content-addressed roles (a "row subtitle", a
 * "snippet") rather than Material's size-addressed roles (`headlineSmall`), which are
 * tuned for a different (icon + card) idiom.
 *
 * The system, stated as four laws (reverse-extracted from the values, then completed
 * by filling the one register the values were missing — see [cardTitle]):
 *
 *  1. Registers, named by content function. Display (menuItem 46 / viewTitle 32 /
 *     subject 26) -> heading (cardTitle 18) -> reading (rowTitle / rowTitleStrong /
 *     body 15, rowSubtitle 14) -> caption (hint 13, sectionLabel / snippet / meta 12).
 *     A role is chosen by meaning, not size, so the scale mirrors the screen's IA.
 *
 *  2. Tracking optically compensates size. Strong negative at display (-0.035), easing
 *     through ~0 at reading, to positive at caption (+0.005..+0.08), with a surcharge
 *     for the uppercased sectionLabel. Big text tightens into a block; small text
 *     opens up to stay legible.
 *
 *  3. Weight encodes function, not size. The giants are the lightest (ExtraLight 200);
 *     weight rises to SemiBold (600) only where an element acts or structures —
 *     emphasis (rowTitleStrong), headings (cardTitle), labels (sectionLabel), buttons.
 *
 *  4. Leading is density-first. Explicit lineHeight is set only on the display giants
 *     (tight, so a title stacks as one block) and on body (a generous 1.67 — the one
 *     place sustained reading earns air). Every other role inherits the font's compact
 *     default on purpose: list / caption density is the default, comfort the exception.
 *
 * Known residuals (do NOT fix blind — these need an on-device / renderPreviews check):
 * screen headers elsewhere reach for a heavier 28px, hinting the ExtraLight [viewTitle]
 * may read thin in Hangul; and ~22px content-title fallbacks (wiki / mail / person)
 * belong on [subject], not a new role.
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

    /**
     * Section / card heading — the rung that fills the gap between [subject] and the
     * reading tier (18sp / 600 / -0.015em). Titles a grouped block: a settings card, a
     * content section, a markdown `##`. Derived from the four laws rather than picked:
     * its size is the geometric center of the 26->15 gap nudged toward real demand, its
     * SemiBold weight follows law 3 (a heading structures, so it takes weight), and its
     * tracking sits on the law-2 optical curve between subject (-0.025) and rowTitle
     * (-0.01). No lineHeight — density stays tight (law 4).
     */
    val cardTitle: TextStyle
        @Composable get() = TextStyle(fontFamily = family).copy(fontSize = 18.sp, fontWeight = FontWeight.SemiBold, letterSpacing = (-0.015).em)

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
