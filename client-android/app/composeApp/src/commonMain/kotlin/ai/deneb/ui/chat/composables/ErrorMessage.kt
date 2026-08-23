package ai.deneb.ui.chat.composables

import ai.deneb.network.UiError
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.handCursor
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import org.jetbrains.compose.resources.stringResource

@Composable
internal fun uiErrorText(error: UiError): String = when (error) {
    is UiError.Resource -> stringResource(error.resource)
    is UiError.Text -> error.message
    is UiError.ResourceWithDetail -> "${stringResource(error.resource)}: ${error.detail}"
}

@Composable
internal fun ErrorMessage(
    error: UiError,
    onDismiss: () -> Unit,
) {
    val text = uiErrorText(error)
    val haptics = rememberHaptics()
    Surface(
        modifier = Modifier.fillMaxWidth().padding(16.dp),
        shape = RoundedCornerShape(16.dp),
        color = MaterialTheme.colorScheme.errorContainer,
    ) {
        Column(
            modifier = Modifier.padding(16.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(
                text = "⚠️ 문제가 생겼어요",
                style = DenebType.rowTitleStrong,
                color = MaterialTheme.colorScheme.onErrorContainer,
            )
            Spacer(Modifier.height(6.dp))
            Text(
                text = text,
                style = DenebType.body,
                color = MaterialTheme.colorScheme.onErrorContainer,
                textAlign = TextAlign.Center,
            )
            Spacer(Modifier.height(10.dp))
            TextButton(
                modifier = Modifier.handCursor(),
                onClick = {
                    haptics.tap()
                    onDismiss()
                },
            ) {
                // "확인", not "다시 시도": the failed text is already restored in the
                // composer, so the retry the user wants is pressing send — a retry
                // button here would send it a second time.
                Text("확인", color = MaterialTheme.colorScheme.onErrorContainer)
            }
        }
    }
}
