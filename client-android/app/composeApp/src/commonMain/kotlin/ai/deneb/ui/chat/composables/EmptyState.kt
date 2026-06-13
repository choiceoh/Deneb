package ai.deneb.ui.chat.composables

import ai.deneb.ui.DenebType
import ai.deneb.ui.components.LogoAnimation
import ai.deneb.ui.denebHint
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import kotlinx.datetime.TimeZone
import kotlinx.datetime.toLocalDateTime
import kotlin.time.Clock

@Composable
internal fun EmptyState(
    modifier: Modifier,
) {
    Column(
        modifier = modifier,
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        val greeting = remember {
            val hour = Clock.System.now().toLocalDateTime(TimeZone.currentSystemDefault()).hour
            when (hour) {
                in 5..10 -> "좋은 아침이에요"
                in 11..16 -> "좋은 오후예요"
                in 17..21 -> "좋은 저녁이에요"
                else -> "늦은 시간까지 고생 많으세요"
            }
        }
        LogoAnimation()
        Spacer(Modifier.height(16.dp))
        Text(
            text = greeting,
            style = DenebType.subject,
            color = MaterialTheme.colorScheme.onBackground,
        )
        Spacer(Modifier.height(8.dp))
        Text(
            text = "분석부터 일정까지 — 무엇이든 물어보세요",
            style = DenebType.body,
            color = denebHint(),
            textAlign = TextAlign.Center,
            modifier = Modifier.padding(horizontal = 24.dp),
        )
    }
}
