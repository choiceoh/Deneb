package ai.deneb.deneb

import ai.deneb.deneb.generated.EngineModelRow
import ai.deneb.deneb.generated.EngineStatusResult
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import ai.deneb.ui.handCursor
import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp

/**
 * Which model's statistics the page shows. The engine serves one model at a
 * time but not the same one all day — a switch, a campaign's window — and its
 * counters carry no model name, so until the gateway keyed its history by
 * model, a day of GLM-5.3 with three Qwen3.8 windows read as one Qwen3.8 day.
 *
 * Plain text tabs: the model names on the page background, the selected one
 * in ink with an accent underline (operator, 2026-09-19: boxed chips read
 * heavier than the statistics they filter). A single model is a plain label.
 * Selecting only re-reads the statistics; what the engine serves is not touched here.
 */
@Composable
internal fun EngineModelTabs(status: EngineStatusResult, onSelect: (String) -> Unit) {
    Column(Modifier.fillMaxWidth()) {
        Text(
            "통계 모델",
            style = DenebType.sectionLabel,
            color = denebHint(),
            modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 12.dp),
        )
        if (status.models.size < 2) {
            Text(status.selectedModel.ifBlank { status.model }.ifBlank { "모델 미상" }, style = DenebType.rowTitle, modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp))
            return@Column
        }
        Row(
            Modifier
                .fillMaxWidth()
                .horizontalScroll(rememberScrollState())
                .padding(start = 16.dp, end = 16.dp, top = 6.dp),
            verticalAlignment = Alignment.Bottom,
        ) {
            status.models.forEachIndexed { index, row ->
                if (index > 0) Spacer(Modifier.width(20.dp))
                EngineModelTab(row, status, onSelect)
            }
        }
        engineModelNote(status)?.let {
            Text(
                text = it,
                style = DenebType.meta,
                color = denebHint(),
                modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 6.dp),
            )
        }
    }
}

@Composable
private fun EngineModelTab(row: EngineModelRow, status: EngineStatusResult, onSelect: (String) -> Unit) {
    val selected = row.model == status.selectedModel
    val haptics = rememberHaptics()
    val marker = engineModelMarker(row, status)
    // The underline takes the label's own width: IntrinsicSize.Max sizes the
    // column to its widest child, the label row, and the bar fills that. No
    // ripple or hover box — no background at all; the moving underline and the
    // tap haptic are the feedback.
    Column(
        Modifier
            .width(IntrinsicSize.Max)
            .selectable(
                selected = selected,
                interactionSource = remember { MutableInteractionSource() },
                indication = null,
                role = Role.Tab,
            ) {
                if (!selected) {
                    haptics.tap()
                    onSelect(row.model)
                }
            }
            .handCursor()
            .heightIn(min = 48.dp)
            .padding(top = 10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = row.model,
                style = if (selected) DenebType.rowTitleStrong else DenebType.rowTitle,
                color = if (selected) MaterialTheme.colorScheme.onBackground else denebHint(),
                maxLines = 1,
            )
            if (marker != null) {
                Spacer(Modifier.width(6.dp))
                Text(
                    text = marker,
                    style = DenebType.meta,
                    color = if (marker == SERVING_NOW) MaterialTheme.colorScheme.primary else denebHint(),
                    maxLines = 1,
                )
            }
        }
        Spacer(Modifier.height(8.dp))
        Box(
            Modifier
                .fillMaxWidth()
                .height(2.dp)
                .background(if (selected) MaterialTheme.colorScheme.primary else Color.Transparent, RoundedCornerShape(1.dp)),
        )
    }
}

private const val SERVING_NOW = "서빙 중"

/**
 * The small word beside the current model's name: whether the engine answers
 * as it right now, or it only was the last one seen — an engine that is down
 * serves nothing, whatever it served last. Null for every other model.
 */
internal fun engineModelMarker(row: EngineModelRow, status: EngineStatusResult): String? = when {
    !row.current -> null
    status.reachable && status.model == row.model -> SERVING_NOW
    else -> "마지막"
}

/**
 * One line under the tabs while a past model is selected: what the numbers
 * below cover, and that the engine-wide cards (availability, share) did not
 * narrow with them. Null for the current model — the page reads as before.
 */
internal fun engineModelNote(status: EngineStatusResult): String? {
    val row = status.models.firstOrNull { it.model == status.selectedModel } ?: return null
    if (row.current) return null
    val last = if (row.lastDay.isNotBlank()) " · 마지막 ${formatDayShort(row.lastDay)}" else ""
    return "지난 모델의 통계 · 최근 7일 중 ${row.days}일$last. 가용성·점유율은 엔진 전체 기준입니다."
}

/**
 * True while the page shows the model the engine serves now (or served last,
 * when it cannot be asked). Only then do the live process's internals and the
 * "current run" wording belong to the numbers on screen.
 */
internal fun EngineStatusResult.viewingCurrentModel(): Boolean {
    if (selectedModel.isBlank()) return true
    val current = models.firstOrNull { it.current }?.model ?: return true
    return current == selectedModel
}
