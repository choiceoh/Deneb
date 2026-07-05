package ai.deneb

import ai.deneb.data.DataRepository
import ai.deneb.deneb.DenebGatewayClient
import ai.deneb.ui.chat.WorkFeedItem
import androidx.car.app.CarContext
import androidx.car.app.Screen
import androidx.car.app.model.Action
import androidx.car.app.model.ItemList
import androidx.car.app.model.ListTemplate
import androidx.car.app.model.MessageTemplate
import androidx.car.app.model.Row
import androidx.car.app.model.Template
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.launch
import org.koin.core.context.GlobalContext

/**
 * Root car screen: the latest work-feed cards as a driving-safe list. Rows show
 * title + one summary line; tapping pushes [DenebCarCardScreen] with the body.
 * Data comes from the same [DenebGatewayClient] feed state the phone UI uses —
 * one refresh on entry, then whatever is cached.
 */
class DenebCarFeedScreen(carContext: CarContext) : Screen(carContext) {
    private val client: DenebGatewayClient? =
        runCatching { GlobalContext.getOrNull()?.get<DataRepository>() as? DenebGatewayClient }.getOrNull()
    private var loading = true

    init {
        val c = client
        if (c == null) {
            loading = false
        } else {
            lifecycleScope.launch {
                runCatching { c.refreshWorkFeed() }
                loading = false
                invalidate()
            }
        }
    }

    override fun onGetTemplate(): Template {
        val c = client
            ?: return MessageTemplate.Builder("데네브에 연결할 수 없습니다. 폰에서 앱을 먼저 열어주세요.")
                .setTitle("데네브")
                .setHeaderAction(Action.APP_ICON)
                .build()

        val items = c.denebWorkFeed.value.take(MAX_ROWS)
        val list = ItemList.Builder().apply {
            if (items.isEmpty() && !loading) {
                setNoItemsMessage("새 소식이 없습니다.")
            }
            items.forEach { item -> addItem(feedRow(item)) }
        }.build()

        return ListTemplate.Builder()
            .setTitle("데네브 업무 피드")
            .setHeaderAction(Action.APP_ICON)
            .setLoading(loading)
            .apply { if (!loading) setSingleList(list) }
            .build()
    }

    private fun feedRow(item: WorkFeedItem): Row {
        val title = item.title.ifBlank { item.summary.ifBlank { "업무 카드" } }
        return Row.Builder()
            .setTitle(if (item.question) "❓ $title" else title)
            .apply { item.summary.takeIf { it.isNotBlank() && it != title }?.let { addText(it) } }
            .setOnClickListener { screenManager.push(DenebCarCardScreen(carContext, item)) }
            .build()
    }

    private companion object {
        // Auto hosts cap list rows (ConstraintManager: typically 6) — stay under it.
        const val MAX_ROWS = 6
    }
}
