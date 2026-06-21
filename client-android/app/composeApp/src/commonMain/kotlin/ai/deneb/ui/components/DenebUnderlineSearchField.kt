package ai.deneb.ui.components

import ai.deneb.ui.DenebType
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.handCursor
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp

/**
 * Deneb's flat search/text input idiom: no box, no pill, no fill — just the text
 * over a single hairline that goes cool-primary (and a touch thicker) on focus.
 * This is the house style for search inputs (search screen, fleet HF search,
 * contacts/mail/files filters); prefer it over the boxy Material `OutlinedTextField`.
 *
 * - [onSearch] non-null switches the IME action to Search and fires on submit
 *   (for screens that run an explicit query); null = live / as-you-type filtering.
 * - [clearable] shows a built-in clear (✕) when the query is non-empty; it calls
 *   `onQueryChange("")`, so each caller's own blank-handling runs on clear.
 * - [trailing] is an optional right-aligned slot (a progress spinner) — it takes
 *   precedence over [clearable] when a screen needs custom trailing content.
 * - [textStyle] defaults to the airy [DenebType.subject]; pass a smaller role
 *   (e.g. [DenebType.body]) on dense list/filter bars.
 */
@Composable
fun DenebUnderlineSearchField(
    query: String,
    onQueryChange: (String) -> Unit,
    placeholder: String,
    modifier: Modifier = Modifier,
    textStyle: TextStyle = DenebType.subject,
    onSearch: (() -> Unit)? = null,
    clearable: Boolean = false,
    trailing: (@Composable () -> Unit)? = null,
) {
    var focused by remember { mutableStateOf(false) }
    val line = if (focused) MaterialTheme.colorScheme.primary else denebHairline()
    val effectiveTrailing: (@Composable () -> Unit)? = when {
        trailing != null -> trailing

        clearable && query.isNotEmpty() -> {
            { ClearAffordance(onClear = { onQueryChange("") }) }
        }

        else -> null
    }
    Column(modifier.fillMaxWidth()) {
        BasicTextField(
            value = query,
            onValueChange = onQueryChange,
            singleLine = true,
            textStyle = textStyle.copy(color = MaterialTheme.colorScheme.onBackground),
            cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
            keyboardOptions = if (onSearch != null) {
                KeyboardOptions(imeAction = ImeAction.Search)
            } else {
                KeyboardOptions.Default
            },
            keyboardActions = KeyboardActions(onSearch = { onSearch?.invoke() }),
            modifier = Modifier
                .fillMaxWidth()
                .onFocusChanged { focused = it.isFocused }
                .padding(vertical = 10.dp),
            decorationBox = { inner ->
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Box(Modifier.weight(1f)) {
                        if (query.isEmpty()) {
                            Text(placeholder, style = textStyle, color = denebHint())
                        }
                        inner()
                    }
                    if (effectiveTrailing != null) {
                        Spacer(Modifier.width(8.dp))
                        effectiveTrailing()
                    }
                }
            },
        )
        Box(
            Modifier
                .fillMaxWidth()
                .height(if (focused) 1.5.dp else 1.dp)
                .background(line),
        )
    }
}

@Composable
private fun ClearAffordance(onClear: () -> Unit) {
    Box(
        Modifier
            .size(32.dp)
            .clip(CircleShape)
            .clickable { onClear() }
            .handCursor(),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            Icons.Filled.Close,
            contentDescription = "지우기",
            modifier = Modifier.size(18.dp),
            tint = denebHint(),
        )
    }
}
