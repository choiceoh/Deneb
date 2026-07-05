package ai.deneb

import ai.deneb.ui.chat.WorkFeedItem
import androidx.car.app.CarContext
import androidx.car.app.Screen
import androidx.car.app.model.Action
import androidx.car.app.model.MessageTemplate
import androidx.car.app.model.Template

/**
 * One feed card as a MessageTemplate. The host truncates long bodies while
 * driving (driver-distraction rules) — full reading stays a parked/phone
 * affordance. Question cards point at the phase-1 voice-reply path instead of
 * offering in-template input (templates have no free-text field).
 */
class DenebCarCardScreen(
    carContext: CarContext,
    private val item: WorkFeedItem,
) : Screen(carContext) {
    override fun onGetTemplate(): Template {
        val body = item.body.ifBlank { item.summary }.ifBlank { "내용이 없습니다." }
        val message = if (item.question) "$body\n\n답장은 데네브 알림에서 음성으로 할 수 있어요." else body
        return MessageTemplate.Builder(message)
            .setTitle(item.title.ifBlank { "업무 카드" })
            .setHeaderAction(Action.BACK)
            .build()
    }
}
