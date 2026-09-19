package ai.deneb.deneb

import ai.deneb.deneb.generated.EngineServing
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebDialog
import ai.deneb.ui.components.DenebTextButton
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import ai.deneb.ui.handCursor
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp

// Which model the engine's production serves, and switching it.
//
// The fleet decides, not the gateway: stkernel's st_production.py on the
// engine's head holds the choice and its supervisor makes the switch (the old
// model down, the new one up — minutes, during which local turns go to the
// fallback chain). The gateway asks the head (`miniapp.engine.serving`), writes
// a choice (`miniapp.engine.select`), and moves the roles that ran on the
// engine once production answers as the new model.
//
// The serving model has an explicit action, separate from the statistics filter.

/**
 * What the engine page's shell hands the verdict line: the fleet's last answer
 * (null until one arrives, and for good on a gateway without the RPC), whether a
 * choice is being written, whether the last one failed, and the action that
 * writes one.
 */
internal class ServingState(
    val serving: EngineServing?,
    val requesting: Boolean = false,
    val requestFailed: Boolean = false,
    val onSwitch: (String) -> Unit = {},
)

/** The serving model and its explicit change action. The dialog is the decision. */
@Composable
internal fun EngineModelSwitcher(text: String, state: ServingState?, fallback: @Composable () -> Unit = {}) {
    val serving = state?.serving
    val switchable = serving != null && servingSwitchable(serving) && !state.requesting
    var choosing by remember { mutableStateOf(false) }
    if (text.isBlank()) {
        fallback()
        return
    }
    Column(Modifier.fillMaxWidth()) {
        Text(
            text = if (state?.requesting == true) "$text · 전환 요청 중" else text,
            style = DenebType.rowSubtitle,
            color = denebHint(),
        )
        if (switchable) {
            DenebTextButton(onClick = { choosing = true }, modifier = Modifier.heightIn(min = 48.dp)) { Text("모델 변경") }
        }
    }
    if (choosing && serving != null) {
        EngineServingDialog(
            serving = serving,
            onSwitch = {
                choosing = false
                state.onSwitch(it)
            },
            onDismiss = { choosing = false },
        )
    }
}

/**
 * The switch as a decision: the fleet's models, and what switching costs,
 * before anything is written. Opens on the current choice; 전환 stays off until
 * a different model is picked.
 */
@Composable
private fun EngineServingDialog(serving: EngineServing, onSwitch: (String) -> Unit, onDismiss: () -> Unit) {
    var picked by remember(serving.selected) { mutableStateOf(serving.selected) }
    val haptics = rememberHaptics()
    DenebDialog(
        onDismissRequest = onDismiss,
        title = { Text("서빙 모델") },
        text = { EngineServingChoices(serving, picked, onPick = { picked = it }) },
        confirmButton = {
            DenebTextButton(
                onClick = {
                    haptics.confirm()
                    onSwitch(picked)
                },
                enabled = picked.isNotBlank() && picked != serving.selected,
            ) { Text("전환") }
        },
        dismissButton = { DenebTextButton(onClick = onDismiss) { Text("취소") } },
    )
}

/**
 * The dialog's body: a radio row per model the fleet can serve, the serving
 * one marked, then the cost of a switch in one short paragraph. Its own
 * composable so the preview can draw the dialog's face without a window.
 */
@Composable
internal fun EngineServingChoices(serving: EngineServing, picked: String, onPick: (String) -> Unit) {
    val haptics = rememberHaptics()
    Column {
        Column(Modifier.selectableGroup()) {
            serving.profiles.forEach { p ->
                val isPicked = p.profile == picked
                Row(
                    Modifier
                        .fillMaxWidth()
                        .selectable(selected = isPicked, role = Role.RadioButton) {
                            if (!isPicked) {
                                haptics.tap()
                                onPick(p.profile)
                            }
                        }
                        .handCursor()
                        .padding(vertical = 2.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    RadioButton(selected = isPicked, onClick = null)
                    Spacer(Modifier.width(8.dp))
                    Text(
                        text = p.model.ifBlank { p.profile },
                        style = DenebType.rowTitle,
                        color = MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.weight(1f),
                    )
                    if (p.profile == serving.serving) {
                        Text(text = "서빙 중", style = DenebType.meta, color = denebHint())
                    }
                }
            }
        }
        Spacer(Modifier.height(14.dp))
        Text(SERVING_SWITCH_NOTE)
    }
}

internal const val SERVING_SWITCH_NOTE =
    "엔진을 내렸다가 이 모델로 다시 띄웁니다. 2~5분 동안 로컬 요청은 클라우드 폴백으로 가고, " +
        "엔진을 쓰던 역할은 새 모델로 옮겨집니다. 부팅에 실패하면 기본 모델로 돌아갑니다."

/** True when a switch can be made here: the fleet answered, a supervisor that
 *  reads the choice is running, and there is another model to choose. */
internal fun servingSwitchable(s: EngineServing): Boolean = s.available && s.supervised && s.profiles.size >= 2

/**
 * The switch in motion, for the verdict line — "glm-5.3-flash →
 * qwen3.8-flash-next · 띄우는 중" — or null at rest. A choice written but not
 * yet taken (the supervisor looks every 30 s) reads as pending, never as
 * served. A boot given up or reverted is not motion: [servingNotices] says it.
 */
internal fun servingProgress(s: EngineServing): String? {
    if (!s.available || !s.supervised) return null
    val from = s.servingModel.ifBlank { s.serving }
    val to = s.selectedModel.ifBlank { s.selected }
    val step = when {
        s.phase == "switching" -> "이전 모델을 내리는 중"
        s.phase == "launching" -> "띄우는 중"
        s.phase == "waiting" -> "플릿이 비길 기다리는 중"
        s.phase == "serving" && s.selected.isNotBlank() && s.selected != s.serving -> "곧 시작"
        else -> return null
    }
    val models = if (from.isBlank() || from == to) to else "$from → $to"
    return "$models · $step"
}

/** One line under the verdict; [failure] only when a person has to act. */
internal data class ServingNotice(val text: String, val failure: Boolean = false)

/**
 * The exceptions, one line each — nothing when production serves what was
 * chosen and every role went with it. A gateway with the control switched off
 * (no head to ask) has nothing to report either: the feature is absent, not
 * broken.
 */
internal fun servingNotices(state: ServingState?): List<ServingNotice> {
    val s = state?.serving ?: return emptyList()
    return buildList {
        if (state.requestFailed) add(ServingNotice("전환을 요청하지 못했습니다", failure = true))
        when {
            !s.available && s.head.isNotBlank() -> add(ServingNotice("모델 전환 불가 — ${s.reason}"))
            s.available && !s.supervised -> add(ServingNotice("모델 전환 불가 — 슈퍼바이저가 모델 선택을 아직 읽지 않습니다"))
        }
        if (s.phase == "held") add(ServingNotice("부팅 재시도를 멈췄습니다 — 확인이 필요합니다", failure = true))
        // Held until the next choice: production is not serving what the operator picked.
        if (s.chosenBy == "supervisor") add(ServingNotice("고른 모델이 부팅에 실패해 기본 모델로 돌아왔습니다"))
        s.held.forEach { add(ServingNotice("${engineRoleLabel(it.role)}은 그대로 — ${it.reason}")) }
    }
}

/** The model picker's names for the roles (ConfigModelTab's ModelRole). */
internal fun engineRoleLabel(role: String): String = when (role) {
    "main" -> "메인"
    "coding" -> "코딩"
    "vision" -> "비전"
    "tiny" -> "초경량"
    "lightweight" -> "경량"
    "fallback" -> "폴백"
    "submain" -> "자율"
    else -> role
}
