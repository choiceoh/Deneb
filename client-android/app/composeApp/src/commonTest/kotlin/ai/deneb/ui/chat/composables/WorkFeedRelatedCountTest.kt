package ai.deneb.ui.chat.composables

import ai.deneb.ui.chat.WorkFeedItem
import kotlinx.serialization.json.Json
import kotlin.test.Test
import kotlin.test.assertEquals

class WorkFeedRelatedCountTest {
    @Test
    fun countsDistinctNonBlankRelatedCardsOtherThanSelf() {
        val item = WorkFeedItem(
            id = "self",
            relatedIds = listOf("related-a", " related-a ", "", "self", "related-b"),
        )

        assertEquals(2, item.relatedWorkFeedCount())
    }

    @Test
    fun decodesGatewaySemanticGroupingFields() {
        val item = Json.decodeFromString<WorkFeedItem>(
            """{"id":"one","clusterId":"wfc-risk","relatedIds":["two","three"]}""",
        )

        assertEquals("wfc-risk", item.clusterId)
        assertEquals(listOf("two", "three"), item.relatedIds)
    }
}
