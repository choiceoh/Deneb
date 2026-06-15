package ai.deneb.deneb

import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import ai.deneb.ui.denebInsightContainer
import ai.deneb.ui.markdown.MarkdownContent
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/**
 * Person dossier (`miniapp.gmail.sender_context` + `list_recent from:`): recent
 * volume, the wiki pages that mention them (tap -> page), and their recent
 * messages (tap -> mail detail). Framed by [DenebScreenScaffold].
 */
@Composable
fun DenebPersonScreen(
    client: DenebGatewayClient,
    sender: String,
    onBack: () -> Unit,
    onOpenMail: (String) -> Unit = {},
    onOpenWiki: (String) -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var ctx by remember(sender) { mutableStateOf<SenderContext?>(null) }
    var loadFailed by remember(sender) { mutableStateOf(false) }
    var recent by remember(sender) { mutableStateOf<List<MailMessage>?>(null) }
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()

    suspend fun load() {
        loadFailed = false
        ctx = null
        val c = client.fetchSenderContext(sender)
        ctx = c
        loadFailed = c == null
        val email = c?.email?.ifBlank { sender } ?: sender
        recent = client.fetchRecentFromSender(email)
    }
    LaunchedEffect(sender) { load() }

    DenebScreenScaffold(title = "사람", onBack = onBack, tabBar = navigationTabBar) {
        Column(
            Modifier
                .fillMaxWidth()
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp),
        ) {
            Spacer(Modifier.height(8.dp))

            val c = ctx
            if (c == null) {
                if (loadFailed) {
                    DenebError("정보를 불러오지 못했습니다.", onRetry = { scope.launch { load() } })
                } else {
                    DenebLoading()
                }
                return@Column
            }

            Text(
                c.displayName,
                style = DenebType.subject,
                color = MaterialTheme.colorScheme.onBackground,
            )
            if (c.email.isNotBlank()) {
                Text(c.email, style = DenebType.rowSubtitle, color = denebHint())
            }
            if (c.recentCount > 0) {
                Spacer(Modifier.height(8.dp))
                Text(
                    "최근 ${c.windowDays}일 · ${c.recentCount}통 수신",
                    style = DenebType.meta,
                    color = denebHint(),
                )
            }
            if (c.wikiFacts.isNotBlank()) {
                // AI-insight callout: wikiFacts is the graphify-CLI synthesized
                // dossier of what the wiki graph knows about this person. It is the
                // screen's one AI-analysis block, so it gets the warm-apricot insight
                // surface (soft fill + AutoAwesome mark + apricot title), matching the
                // mail-detail idiom. See native-design-system.md (2-accent doctrine).
                Spacer(Modifier.height(16.dp))
                Column(
                    Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(16.dp))
                        .background(denebInsightContainer())
                        .padding(16.dp),
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Icon(
                            imageVector = Icons.Outlined.AutoAwesome,
                            contentDescription = null,
                            tint = denebInsight(),
                            modifier = Modifier.size(20.dp),
                        )
                        Spacer(Modifier.width(10.dp))
                        Text("AI 분석", style = DenebType.rowTitleStrong, color = denebInsight())
                    }
                    Spacer(Modifier.height(8.dp))
                    MarkdownContent(c.wikiFacts, baseStyle = MaterialTheme.typography.bodyMedium)
                }
            }

            if (c.wikiHits.isNotEmpty()) {
                DenebSectionLabel("관련 위키")
                c.wikiHits.forEach { hit ->
                    Column(
                        Modifier
                            .fillMaxWidth()
                            .then(
                                if (hit.path.isNotBlank()) {
                                    Modifier.clickable {
                                        haptics.tap()
                                        onOpenWiki(hit.path)
                                    }
                                } else {
                                    Modifier
                                },
                            )
                            .padding(vertical = 8.dp),
                    ) {
                        Text(hit.title, style = DenebType.rowTitle, color = MaterialTheme.colorScheme.onBackground)
                        if (hit.summary.isNotBlank()) {
                            Text(hit.summary, style = DenebType.rowSubtitle, color = denebHint())
                        }
                    }
                }
            }

            val mail = recent
            if (!mail.isNullOrEmpty()) {
                DenebSectionLabel("최근 메일 ${mail.size}")
                mail.forEach { m ->
                    MailRow(
                        message = m,
                        selecting = false,
                        isSelected = false,
                        onTap = {
                            haptics.tap()
                            onOpenMail(m.id)
                        },
                        onLongPress = {},
                    )
                }
            }

            if (c.recentCount == 0 && c.wikiHits.isEmpty() && c.wikiFacts.isBlank() && mail.isNullOrEmpty()) {
                Spacer(Modifier.height(12.dp))
                Text(
                    "알려진 컨텍스트가 없습니다.",
                    style = DenebType.body,
                    color = denebHint(),
                )
            }
            Spacer(Modifier.height(24.dp))
        }
    }
}
