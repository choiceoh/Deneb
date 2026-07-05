package ai.deneb

import ai.deneb.data.DataRepository
import ai.deneb.deneb.DenebGatewayClient
import ai.deneb.ui.chat.WorkFeedItem
import androidx.car.app.CarContext
import androidx.car.app.Screen
import androidx.car.app.constraints.ConstraintManager
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
 * one refresh on entry, then live StateFlow updates (the phone's SSE/native-sync
 * keeps it current) re-render the list. Re-rendering the same template with the
 * same title counts as a refresh for the host, not a new task step.
 */
class DenebCarFeedScreen(carContext: CarContext) : Screen(carContext) {
    private val client: DenebGatewayClient? =
        runCatching { GlobalContext.getOrNull()?.get<DataRepository>() as? DenebGatewayClient }.getOrNull()
    private var loading = true
    private var refreshFailed = false

    init {
        val c = client
        if (c == null) {
            loading = false
        } else {
            lifecycleScope.launch {
                refreshFailed = !runCatching { c.refreshWorkFeed() }.getOrDefault(false)
                loading = false
                invalidate()
            }
            lifecycleScope.launch {
                c.denebWorkFeed.collect { if (!loading) invalidate() }
            }
        }
    }

    override fun onGetTemplate(): Template {
        val c = client
            ?: return MessageTemplate.Builder("데네브에 연결할 수 없습니다. 폰에서 앱을 먼저 열어주세요.")
                .setTitle("데네브")
                .setHeaderAction(Action.APP_ICON)
                .build()

        val items = c.denebWorkFeed.value.take(rowLimit())
        val list = ItemList.Builder().apply {
            if (items.isEmpty() && !loading) {
                setNoItemsMessage(
                    if (refreshFailed) {
                        "게이트웨이에 연결하지 못했습니다. 폰에서 연결을 확인해주세요."
                    } else {
                        "새 소식이 없습니다."
                    },
                )
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
            .setOnClickListener {
                // Opening a card reads it — same durable, cross-device read state the
                // phone sets (miniapp.workfeed.read). Fire-and-forget: only readAtMs on
                // the gateway changes, so the car list doesn't reshuffle mid-drive.
                client?.let { c -> lifecycleScope.launch { runCatching { c.markWorkFeedRead(item.id) } } }
                screenManager.push(DenebCarCardScreen(carContext, item))
            }
            .build()
    }

    /**
     * Host-reported list row cap where available (ConstraintManager is Car API
     * 2+; we declare minCarApiLevel=1), clamped to [MAX_ROWS] so a generous
     * host still gets a glanceable list rather than a scroll marathon.
     */
    private fun rowLimit(): Int {
        val hostLimit = runCatching {
            if (carContext.carAppApiLevel >= 2) {
                carContext.getCarService(ConstraintManager::class.java)
                    .getContentLimit(ConstraintManager.CONTENT_LIMIT_TYPE_LIST)
            } else {
                MAX_ROWS
            }
        }.getOrDefault(MAX_ROWS)
        return hostLimit.coerceIn(1, MAX_ROWS)
    }

    private companion object {
        // Fallback/upper bound when the host doesn't report a list constraint.
        const val MAX_ROWS = 6
    }
}
