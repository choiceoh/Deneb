package ai.deneb.deneb

import ai.deneb.data.AppSettings
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebListRow
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

/**
 * Settings hub "더보기 표시 항목" tab: which 더보기 list entries appear. Each hideable
 * tile ([hideableMoreEntries] — every [MoreEntry] that isn't [MoreEntry.alwaysShown]) gets a
 * row with a Material [Switch]: ON = 표시, OFF = 숨김. The user's hidden set persists in
 * [AppSettings.getHiddenMoreTiles] keyed by the tile's stable [MoreEntry.key], and
 * [DenebMoreScreen] filters the grid against it (composing with the 챗봇-mode 업무 gate).
 *
 * 채팅·설정 are intentionally NOT listed: 설정 is [MoreEntry.alwaysShown] (hiding it would lock
 * out this very control) and 채팅 is a bottom-bar tab (never in the grid). Hosted by
 * [DenebConfigScreen]'s detail shell.
 *
 * Stateful shell ([AppTilesTab]) holds the live hidden set + persistence; the stateless body
 * ([AppTilesContent]) is pure (hidden set + onToggle) so it renders under the preview harness.
 */
@Composable
internal fun AppTilesTab(appSettings: AppSettings) {
    var hidden by remember { mutableStateOf(appSettings.getHiddenMoreTiles()) }
    AppTilesContent(
        hidden = hidden,
        onToggle = { key, hide ->
            appSettings.setMoreTileHidden(key, hide)
            // Re-read so the source of truth stays the persisted set (not a local mutation).
            hidden = appSettings.getHiddenMoreTiles()
        },
    )
}

/** Stateless body: lists [hideableMoreEntries], each with a 표시/숨김 [Switch]. [onToggle] is
 *  called with the tile key and the new HIDDEN state (true = now hidden). */
@Composable
internal fun AppTilesContent(hidden: Set<String>, onToggle: (key: String, hidden: Boolean) -> Unit) {
    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(top = 4.dp, bottom = 24.dp),
    ) {
        Text(
            "더보기 화면에 표시할 항목을 고릅니다. 끄면 그 항목이 목록에서 숨겨집니다. " +
                "채팅·설정은 항상 표시됩니다.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
        )
        DenebGroup(label = "더보기 표시 항목") {
            hideableMoreEntries.forEachIndexed { i, entry ->
                val shown = entry.key !in hidden
                DenebListRow(
                    title = entry.label,
                    onClick = { onToggle(entry.key, shown) },
                    icon = entry.icon,
                    selected = shown,
                    divider = i < hideableMoreEntries.lastIndex,
                    chevron = false,
                    trailing = {
                        Switch(
                            checked = shown,
                            // checked = 표시; flipping it OFF hides the tile (onToggle hidden=true).
                            onCheckedChange = { isShown -> onToggle(entry.key, !isShown) },
                        )
                    },
                )
            }
        }
        Spacer(Modifier.height(20.dp))
    }
}
