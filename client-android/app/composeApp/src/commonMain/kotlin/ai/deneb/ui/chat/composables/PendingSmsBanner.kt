package ai.deneb.ui.chat.composables

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.key
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.deneb.data.SmsDraft
import ai.deneb.data.SmsDraftStatus
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebBannerEnter
import ai.deneb.ui.denebBannerExit
import ai.deneb.ui.handCursor
import ai.deneb.ui.denebAdaptiveCardBorder
import ai.deneb.ui.denebAdaptiveCardColors
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.ic_close
import deneb.composeapp.generated.resources.sms_draft_banner_discard
import deneb.composeapp.generated.resources.sms_draft_banner_dismiss
import deneb.composeapp.generated.resources.sms_draft_banner_failed
import deneb.composeapp.generated.resources.sms_draft_banner_send
import deneb.composeapp.generated.resources.sms_draft_banner_sending
import deneb.composeapp.generated.resources.sms_draft_banner_sent
import deneb.composeapp.generated.resources.sms_draft_banner_to
import kotlinx.collections.immutable.ImmutableList
import org.jetbrains.compose.resources.stringResource
import org.jetbrains.compose.resources.vectorResource

/**
 * Stack of cards, one per pending/sending/sent/failed SMS draft. Explicit
 * confirmation gate — the AI stages drafts via `send_sms` / `reply_sms` but
 * nothing leaves the device until the user taps Send here.
 */
@Composable
internal fun PendingSmsBanners(
    drafts: ImmutableList<SmsDraft>,
    onSend: (String) -> Unit,
    onDiscard: (String) -> Unit,
) {
    AnimatedVisibility(
        visible = drafts.isNotEmpty(),
        enter = denebBannerEnter,
        exit = denebBannerExit,
    ) {
        Column(
            modifier = Modifier.fillMaxWidth(),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            for (draft in drafts) {
                // Stable key so a status change on one draft doesn't recompose siblings.
                key(draft.id) {
                    PendingSmsBanner(
                        draft = draft,
                        onSend = { onSend(draft.id) },
                        onDiscard = { onDiscard(draft.id) },
                    )
                }
            }
        }
    }
}

@Composable
private fun PendingSmsBanner(
    draft: SmsDraft,
    onSend: () -> Unit,
    onDiscard: () -> Unit,
) {
    val haptics = rememberHaptics()
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 12.dp, vertical = 2.dp),
        colors = denebAdaptiveCardColors(),
        border = denebAdaptiveCardBorder(),
    ) {
        Column(
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 8.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = stringResource(Res.string.sms_draft_banner_to, draft.address),
                    style = MaterialTheme.typography.labelMedium,
                    fontWeight = FontWeight.SemiBold,
                    color = MaterialTheme.colorScheme.onBackground,
                    modifier = Modifier.weight(1f),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                val dismissLabel = stringResource(
                    if (draft.status == SmsDraftStatus.SENT) {
                        Res.string.sms_draft_banner_dismiss
                    } else {
                        Res.string.sms_draft_banner_discard
                    },
                )
                IconButton(
                    modifier = Modifier.size(24.dp).handCursor(),
                    onClick = onDiscard,
                ) {
                    Icon(
                        imageVector = vectorResource(Res.drawable.ic_close),
                        contentDescription = dismissLabel,
                        tint = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.size(16.dp),
                    )
                }
            }
            Spacer(Modifier.height(4.dp))
            Text(
                text = draft.body,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onBackground,
                maxLines = 3,
                overflow = TextOverflow.Ellipsis,
            )
            Spacer(Modifier.height(4.dp))
            when (draft.status) {
                SmsDraftStatus.PENDING -> {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.End,
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        TextButton(onClick = { haptics.confirm(); onSend() }) {
                            Text(stringResource(Res.string.sms_draft_banner_send))
                        }
                    }
                }

                SmsDraftStatus.SENDING -> {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        CircularProgressIndicator(
                            modifier = Modifier.size(14.dp),
                            strokeWidth = 2.dp,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        Spacer(Modifier.size(8.dp))
                        Text(
                            text = stringResource(Res.string.sms_draft_banner_sending),
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }

                SmsDraftStatus.SENT -> Text(
                    text = stringResource(Res.string.sms_draft_banner_sent),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.primary,
                )

                SmsDraftStatus.FAILED -> Text(
                    text = stringResource(Res.string.sms_draft_banner_failed, draft.lastError ?: "unknown error"),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.error,
                )
            }
        }
    }
}
