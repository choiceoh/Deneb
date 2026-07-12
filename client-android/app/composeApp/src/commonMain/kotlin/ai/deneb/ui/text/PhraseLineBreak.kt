package ai.deneb.ui.text

import androidx.compose.ui.text.style.LineBreak

/**
 * Phrase-aware line breaking for short Korean UI text (table cells and other
 * narrow slots): prefer breaking at spaces (어절) over mid-word, so
 * "카운트다운" never splits into "카/운트다운". The granular LineBreak
 * constructor (WordBreak.Phrase) is Android-only API — other targets fall
 * back to the high-quality paragraph preset, which is the closest common one.
 */
expect fun denebPhraseLineBreak(): LineBreak
