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
 * Organizing principle — two registers, two jobs. Deneb is a Korean-first daily-driver
 * tool, not an editorial surface, so where expression and legibility conflict the
 * editorial character is confined to the sparse display "voice" and the working tiers
 * stay functional:
 *
 *   - Display (menuItem 46 / viewTitle 28 / subject 22): editorial — large, airy, tightly
 *     tracked — but Hangul-adapted. The giants are Light, not ExtraLight (200 reads as thin
 *     hairlines on Korean syllable blocks), and the tight Latin tracking is eased so the
 *     blocks do not crowd. Seen rarely and read once, so the expressive cost is cheap.
 *   - Heading + reading + caption (cardTitle 18 / rowTitle·rowTitleStrong·body 15,
 *     rowSubtitle 14 / hint 13, sectionLabel·snippet·meta 12): functional — legible
 *     weights, no hairlines, density-first leading. Scanned repeatedly, so friction is the
 *     cost that matters.
 *
 * Four laws hold across both:
 *
 *  1. Roles are named by content function, not size, so the scale mirrors the screen's IA.
 *     Sizes follow a ~1.22 ladder in the working range (15 -> 18 -> 22 -> 28), with
 *     menuItem the deliberate home-hero outlier.
 *  2. Tracking optically compensates size: negative at display (-0.02), through ~0 at
 *     reading, to positive at caption (+0.005..+0.08), with a surcharge for the uppercased
 *     sectionLabel.
 *  3. Weight encodes function, not size: it rises to SemiBold (600) only where an element
 *     acts or structures (emphasis, headings, labels, buttons). The display tier sits at
 *     Light — expressive yet legible — rather than the old hairline ExtraLight.
 *  4. Leading is density-first: explicit lineHeight only on the display giants (tight, so a
 *     title stacks as one block) and on body (a generous 1.67 for sustained reading); every
 *     other role inherits the font's compact default on purpose.
 *
 * Residual (needs an on-device / renderPreviews check before chasing further): body is
 * still Light (300) — whether that reads heavy enough for sustained Korean paragraphs, and
 * how the new display weights/sizes look in situ, is unverified in this pass.
 *
 * Every style is Pretendard (the bundled UI face); only the size/weight/tracking differ.
 * Access is @Composable because the bundled font is a compose resource.
 */
object DenebType {
    private val family: FontFamily
        @Composable get() = PretendardFontFamily()

    /**
     * Home / landing giants — `.type-item`. The product's loudest editorial voice, so it
     * keeps the airy, tightly-tracked display character — but Hangul-adapted: Light, not
     * the old ExtraLight, since 200 reads as thin hairlines on Korean. (46sp / 300 / -0.02em.)
     */
    val menuItem: TextStyle
        @Composable get() = TextStyle(fontFamily = family).copy(fontSize = 46.sp, lineHeight = 50.sp, fontWeight = FontWeight.Light, letterSpacing = (-0.02).em)

    /**
     * Hero page title — `.view-title`, the [ai.deneb.ui.DenebScreenScaffold] header. Light,
     * not the old ExtraLight: screens were hand-rolling a heavier ~28px header to dodge the
     * thin giant, so this folds that back into one legible token. (28sp / 300 / -0.018em.)
     */
    val viewTitle: TextStyle
        @Composable get() = TextStyle(fontFamily = family).copy(fontSize = 28.sp, lineHeight = 32.sp, fontWeight = FontWeight.Light, letterSpacing = (-0.018).em)

    /**
     * Content subject — `.email-subject` / `.wiki-title`: the title of the one thing on a
     * screen (a mail, a wiki page, a person). 22sp matches the size screens already reach
     * for via the titleLarge fallback, so those should migrate here. (22sp / 300 / -0.015em.)
     */
    val subject: TextStyle
        @Composable get() = TextStyle(fontFamily = family).copy(fontSize = 22.sp, lineHeight = 28.sp, fontWeight = FontWeight.Light, letterSpacing = (-0.015).em)

    /**
     * Section / card heading — the rung that fills the gap between [subject] and the
     * reading tier (18sp / 600 / -0.015em). Titles a grouped block: a settings card, a
     * content section, a markdown `##`. Derived from the four laws rather than picked:
     * its size is the geometric center of the subject->reading gap (sqrt(22*15) ~= 18),
     * its SemiBold weight follows law 3 (a heading structures, so it takes weight), and
     * its tracking sits on the law-2 optical curve between subject (-0.015) and rowTitle
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
