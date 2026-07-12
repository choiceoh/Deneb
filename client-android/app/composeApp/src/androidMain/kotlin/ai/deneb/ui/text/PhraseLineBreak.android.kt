package ai.deneb.ui.text

import androidx.compose.ui.text.style.LineBreak

actual fun denebPhraseLineBreak(): LineBreak = LineBreak(
    strategy = LineBreak.Strategy.HighQuality,
    strictness = LineBreak.Strictness.Normal,
    wordBreak = LineBreak.WordBreak.Phrase,
)
