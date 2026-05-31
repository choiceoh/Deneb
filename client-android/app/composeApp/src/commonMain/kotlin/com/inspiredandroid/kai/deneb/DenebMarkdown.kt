package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp

/**
 * Lightweight markdown renderer for wiki / topic-doc / analysis bodies — enough
 * structure (headings, bullets, inline bold + code) to read well without
 * pulling in Kai's chat-coupled renderer. Not a full parser.
 */
@Composable
fun DenebMarkdown(text: String, modifier: Modifier = Modifier) {
    val onSurface = MaterialTheme.colorScheme.onSurface
    Column(modifier) {
        var inFence = false
        text.split("\n").forEach { raw ->
            val line = raw.trimEnd()
            when {
                line.startsWith("```") -> inFence = !inFence
                inFence -> Text(
                    line,
                    style = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
                    color = onSurface,
                )
                line.startsWith("### ") -> Heading(line.removePrefix("### "), MaterialTheme.typography.titleSmall.fontSize)
                line.startsWith("## ") -> Heading(line.removePrefix("## "), MaterialTheme.typography.titleMedium.fontSize)
                line.startsWith("# ") -> Heading(line.removePrefix("# "), MaterialTheme.typography.titleLarge.fontSize)
                line.startsWith("- ") || line.startsWith("* ") -> Row2("•  ", line.drop(2), onSurface)
                line.isBlank() -> Spacer(Modifier.height(8.dp))
                else -> Text(inline(line), style = MaterialTheme.typography.bodyMedium, color = onSurface)
            }
        }
    }
}

@Composable
private fun Heading(text: String, size: androidx.compose.ui.unit.TextUnit) {
    Spacer(Modifier.height(8.dp))
    Text(
        text,
        style = MaterialTheme.typography.titleMedium.copy(fontSize = size, fontWeight = FontWeight.SemiBold),
        color = MaterialTheme.colorScheme.onSurface,
    )
    Spacer(Modifier.height(2.dp))
}

@Composable
private fun Row2(prefix: String, body: String, color: androidx.compose.ui.graphics.Color) {
    Text(
        buildAnnotatedString {
            append(prefix)
            append(inline(body))
        },
        style = MaterialTheme.typography.bodyMedium,
        color = color,
    )
}

/** Parse inline **bold** and `code` into an AnnotatedString. */
private fun inline(text: String): AnnotatedString = buildAnnotatedString {
    var i = 0
    while (i < text.length) {
        when {
            text.startsWith("**", i) -> {
                val end = text.indexOf("**", i + 2)
                if (end > 0) {
                    withStyle(SpanStyle(fontWeight = FontWeight.Bold)) { append(text.substring(i + 2, end)) }
                    i = end + 2
                } else {
                    append(text.substring(i)); i = text.length
                }
            }
            text[i] == '`' -> {
                val end = text.indexOf('`', i + 1)
                if (end > 0) {
                    withStyle(SpanStyle(fontFamily = FontFamily.Monospace)) { append(text.substring(i + 1, end)) }
                    i = end + 1
                } else {
                    append(text.substring(i)); i = text.length
                }
            }
            else -> { append(text[i]); i++ }
        }
    }
}
