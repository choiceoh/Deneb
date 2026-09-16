package ai.deneb.ui.chat.composables

import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.fallback_reason_budget
import deneb.composeapp.generated.resources.fallback_reason_circuit_open
import deneb.composeapp.generated.resources.fallback_reason_engine_down
import deneb.composeapp.generated.resources.fallback_reason_error
import deneb.composeapp.generated.resources.fallback_reason_stall
import org.jetbrains.compose.resources.StringResource

/**
 * The gateway's fallback reason (done frame `fallbackReason`) as the label the
 * answer's meta line shows. The strings are the gateway's stable vocabulary
 * (`chat.FallbackReason*`); anything else — an older gateway, a blank — gets
 * no label and the line falls back to the plain "answered by" form.
 */
internal fun fallbackReasonLabel(reason: String?): StringResource? = when (reason) {
    "engine_down" -> Res.string.fallback_reason_engine_down
    "circuit_open" -> Res.string.fallback_reason_circuit_open
    "stall" -> Res.string.fallback_reason_stall
    "budget" -> Res.string.fallback_reason_budget
    "error" -> Res.string.fallback_reason_error
    else -> null
}
