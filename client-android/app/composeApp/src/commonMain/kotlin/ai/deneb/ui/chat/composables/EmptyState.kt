package ai.deneb.ui.chat.composables

import ai.deneb.ui.DenebType
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material3.Icon
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
    // recall on = 업무 workspace, recall off = 챗봇 workspace (the top-bar pill).
    // 업무 greets the operator with a personalized time-of-day line; 챗봇 opens a
    // light general chat. A single greeting line — no subtitle in either mode.
    recallEnabled: Boolean,
    modifier: Modifier,
) {
    Column(
        modifier = modifier,
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        val hour = remember {
            Clock.System.now().toLocalDateTime(TimeZone.currentSystemDefault()).hour
        }
        val greeting = if (recallEnabled) {
            when (hour) {
                in 5..10 -> "선택님, 좋은 아침이에요"
                in 11..16 -> "선택님, 좋은 오후예요"
                in 17..21 -> "선택님, 좋은 저녁이에요"
                else -> "선택님, 늦은 시간까지 고생 많으세요"
            }
        } else {
            "안녕하세요? 무슨 대화를 할까요?"
        }
        // A muted monochrome sparkle — a calm "assistant" anchor that stays on-palette.
        // (Replaced the purple logo orb, which was the one element breaking the
        // monochrome + cool/warm accent idiom on the most-seen screen.)
        Icon(
            Icons.Outlined.AutoAwesome,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.onBackground.copy(alpha = 0.5f),
            modifier = Modifier.size(44.dp),
        )
        Spacer(Modifier.height(16.dp))
        Text(
            text = greeting,
            style = DenebType.subject,
            color = MaterialTheme.colorScheme.onBackground,
            textAlign = TextAlign.Center,
            modifier = Modifier.padding(horizontal = 24.dp),
        )
    }
}
