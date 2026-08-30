package ai.deneb.ui.chat.composables

import ai.deneb.ui.icons.outlined.AutoAwesome
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

/**
 * The empty chat: a muted monochrome sparkle and nothing else.
 *
 * There used to be a time-of-day greeting here ("좋은 아침이에요" / "좋은 오후예요" / …).
 * It is gone, for two reasons that point the same way. It was scaffolding on the
 * most-seen screen — a line that performs warmth without carrying information, which
 * is exactly what ADR 0007's first principle removes. And it read the wall clock, so
 * this fixture's golden changed four times a day; it drifted between two consecutive
 * renders the day the gate was built.
 */
@Composable
internal fun EmptyState(modifier: Modifier) {
    Column(
        modifier = modifier,
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        // A calm "assistant" anchor that stays on-palette. (Replaced the purple logo
        // orb, which was the one element breaking the monochrome idiom here.)
        Icon(
            Icons.Outlined.AutoAwesome,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.onBackground.copy(alpha = 0.5f),
            modifier = Modifier.size(44.dp),
        )
    }
}
